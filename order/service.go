package order

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const OperationCheckout = "checkout"

type Service struct {
	transactor Transactor
	reader     Reader
	clock      Clock
	numbers    NumberGenerator
}

func NewService(transactor Transactor, reader Reader, clock Clock, numbers NumberGenerator) (*Service, error) {
	if transactor == nil {
		return nil, errors.New("new order service: transactor is required")
	}
	if reader == nil {
		return nil, errors.New("new order service: reader is required")
	}
	if clock == nil {
		return nil, errors.New("new order service: clock is required")
	}
	if numbers == nil {
		return nil, errors.New("new order service: number generator is required")
	}
	return &Service{transactor: transactor, reader: reader, clock: clock, numbers: numbers}, nil
}

func (s *Service) Create(ctx context.Context, userID uint64, input CreateInput) (*Order, error) {
	now := s.clock.Now()
	items, err := normalizeRequestedItems(input.Items)
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, fmt.Errorf("create order: %w", &ValidationError{Field: "idempotency_key", Message: "must not be empty", Err: ErrInvalidOrder})
	}

	requestHash, err := canonicalRequestHash(items)
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}

	var created *Order
	err = s.transactor.WithinTransaction(ctx, func(tx Tx) error {
		claim, err := tx.ClaimIdempotency(ctx, IdempotencyClaim{
			UserID:      userID,
			Operation:   OperationCheckout,
			Key:         strings.TrimSpace(input.IdempotencyKey),
			RequestHash: requestHash,
			CreatedAt:   now,
		})
		if err != nil {
			return fmt.Errorf("claim idempotency: %w", err)
		}

		switch claim.Kind {
		case "", IdempotencyClaimed:
		case IdempotencyReplay:
			order, err := s.reader.ByID(ctx, claim.OrderID)
			if err != nil {
				return fmt.Errorf("load replay order %d: %w", claim.OrderID, err)
			}
			if order == nil {
				return fmt.Errorf("load replay order %d: %w", claim.OrderID, ErrNotFound)
			}
			created = cloneOrder(order)
			return nil
		case IdempotencyConflict:
			return ErrConflict
		default:
			return fmt.Errorf("claim idempotency: unknown result %q", claim.Kind)
		}

		order, err := s.buildOrder(ctx, tx, userID, items, now)
		if err != nil {
			return err
		}
		if err := tx.CreateOrder(ctx, order); err != nil {
			return fmt.Errorf("persist order: %w", err)
		}
		if err := tx.CompleteIdempotency(ctx, IdempotencyCompletion{
			UserID:    userID,
			Operation: OperationCheckout,
			Key:       strings.TrimSpace(input.IdempotencyKey),
			OrderID:   order.ID,
			UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("complete idempotency: %w", err)
		}
		created = cloneOrder(order)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	return created, nil
}

func (s *Service) Confirm(ctx context.Context, orderID uint64, expectedVersion uint64) (*Order, error) {
	return s.advance(ctx, orderID, expectedVersion, "confirm", 0, func(order *Order, now time.Time) error {
		return order.Confirm(now)
	}, false)
}

func (s *Service) Ship(ctx context.Context, orderID uint64, expectedVersion uint64) (*Order, error) {
	return s.advance(ctx, orderID, expectedVersion, "ship", 0, func(order *Order, now time.Time) error {
		return order.Ship(now)
	}, false)
}

func (s *Service) Deliver(ctx context.Context, orderID uint64, expectedVersion uint64) (*Order, error) {
	return s.advance(ctx, orderID, expectedVersion, "deliver", 0, func(order *Order, now time.Time) error {
		return order.Deliver(now)
	}, false)
}

func (s *Service) Cancel(ctx context.Context, actor Actor, orderID uint64, expectedVersion uint64) (*Order, error) {
	if !actor.Admin {
		if actor.UserID == 0 {
			return nil, fmt.Errorf("cancel order %d: %w", orderID, ErrForbidden)
		}
		order, err := s.reader.ByID(ctx, orderID)
		if err != nil {
			return nil, fmt.Errorf("cancel order %d: preflight: %w", orderID, err)
		}
		if order == nil {
			return nil, fmt.Errorf("cancel order %d: preflight: %w", orderID, ErrNotFound)
		}
		// Authorization is checked before entering the transaction so a caller cannot
		// use persistence timing to probe orders they do not own.
		if order.UserID != actor.UserID {
			return nil, fmt.Errorf("cancel order %d: %w", orderID, ErrForbidden)
		}
	}

	return s.advance(ctx, orderID, expectedVersion, "cancel", actor.UserID, func(order *Order, now time.Time) error {
		if err := order.Cancel(now); err != nil {
			return err
		}
		return nil
	}, true)
}

func (s *Service) buildOrder(ctx context.Context, tx Tx, userID uint64, items []RequestedItem, now time.Time) (*Order, error) {
	productIDs := make([]uint64, 0, len(items))
	for _, item := range items {
		productIDs = append(productIDs, item.ProductID)
	}
	products, err := tx.LockProducts(ctx, productIDs)
	if err != nil {
		return nil, fmt.Errorf("lock products: %w", err)
	}

	productsByID := make(map[uint64]SellableProduct, len(products))
	for _, product := range products {
		productsByID[product.ID] = product
	}

	number, err := s.numbers.New(now)
	if err != nil {
		return nil, fmt.Errorf("generate number: %w", err)
	}

	order := &Order{
		UserID:    userID,
		Number:    number,
		Status:    StatusPending,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}

	for _, requested := range items {
		product, ok := productsByID[requested.ProductID]
		if !ok {
			return nil, fmt.Errorf("product %d: %w", requested.ProductID, ErrNotFound)
		}
		if product.Stock < requested.Quantity {
			return nil, fmt.Errorf("product %d: %w", requested.ProductID, ErrInsufficientStock)
		}

		subtotal, err := checkedMultiply(product.UnitPrice, requested.Quantity)
		if err != nil {
			return nil, fmt.Errorf("product %d: %w", requested.ProductID, err)
		}
		total, err := checkedAdd(order.Total, subtotal)
		if err != nil {
			return nil, fmt.Errorf("product %d: %w", requested.ProductID, err)
		}

		order.Items = append(order.Items, Item{
			ProductID: product.ID,
			SKU:       product.SKU,
			Name:      product.Name,
			UnitPrice: product.UnitPrice,
			Subtotal:  subtotal,
			Quantity:  requested.Quantity,
		})
		order.Total = total
	}

	// Inventory is decremented from the locked server-side snapshot so clients can
	// never smuggle stale prices or stock assumptions into the persisted order.
	for _, item := range order.Items {
		if err := tx.DecreaseStock(ctx, StockChange{
			ProductID:   item.ProductID,
			Delta:       -int64(item.Quantity),
			ActorUserID: userID,
		}); err != nil {
			return nil, fmt.Errorf("decrease stock for product %d: %w", item.ProductID, err)
		}
	}

	return order, nil
}

func (s *Service) advance(
	ctx context.Context,
	orderID uint64,
	expectedVersion uint64,
	action string,
	actorUserID uint64,
	apply func(*Order, time.Time) error,
	restoreInventory bool,
) (*Order, error) {
	var updated *Order
	err := s.transactor.WithinTransaction(ctx, func(tx Tx) error {
		order, err := tx.LockOrder(ctx, orderID)
		if err != nil {
			return fmt.Errorf("lock order %d: %w", orderID, err)
		}
		if order == nil {
			return fmt.Errorf("lock order %d: %w", orderID, ErrNotFound)
		}
		if err := apply(order, s.clock.Now()); err != nil {
			return err
		}
		if restoreInventory {
			for _, item := range order.Items {
				if err := tx.IncreaseStock(ctx, StockChange{
					ProductID:   item.ProductID,
					Delta:       int64(item.Quantity),
					ActorUserID: actorUserID,
				}); err != nil {
					return fmt.Errorf("restore stock for product %d: %w", item.ProductID, err)
				}
			}
		}
		if err := tx.UpdateStatus(ctx, order, expectedVersion); err != nil {
			return fmt.Errorf("persist order %d %s: %w", orderID, action, err)
		}
		updated = cloneOrder(order)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s order %d: %w", action, orderID, err)
	}
	return updated, nil
}

func (o *Order) Confirm(now time.Time) error {
	return o.transition(StatusPending, StatusConfirmed, now)
}

func (o *Order) Ship(now time.Time) error {
	return o.transition(StatusConfirmed, StatusShipped, now)
}

func (o *Order) Deliver(now time.Time) error {
	return o.transition(StatusShipped, StatusDelivered, now)
}

func (o *Order) Cancel(now time.Time) error {
	switch o.Status {
	case StatusPending, StatusConfirmed:
		o.Status = StatusCancelled
		o.Version++
		o.UpdatedAt = now
		return nil
	default:
		return fmt.Errorf("status %q: %w", o.Status, ErrInvalidTransition)
	}
}

func (o *Order) transition(from Status, to Status, now time.Time) error {
	if o.Status != from {
		return fmt.Errorf("status %q: %w", o.Status, ErrInvalidTransition)
	}
	o.Status = to
	o.Version++
	o.UpdatedAt = now
	return nil
}

func normalizeRequestedItems(items []RequestedItem) ([]RequestedItem, error) {
	if len(items) == 0 {
		return nil, &ValidationError{Field: "items", Message: "must not be empty", Err: ErrInvalidOrder}
	}

	merged := make(map[uint64]uint64, len(items))
	for _, item := range items {
		if item.ProductID == 0 {
			return nil, &ValidationError{Field: "items", Message: "product_id must be greater than 0", Err: ErrInvalidOrder}
		}
		if item.Quantity == 0 {
			return nil, &ValidationError{Field: "items", Message: "quantity must be greater than 0", Err: ErrInvalidOrder}
		}
		merged[item.ProductID] += uint64(item.Quantity)
		if merged[item.ProductID] > math.MaxUint32 {
			return nil, &ValidationError{Field: "items", Message: "combined quantity exceeds 4294967295", Err: ErrInvalidOrder}
		}
	}

	normalized := make([]RequestedItem, 0, len(merged))
	for productID, quantity := range merged {
		normalized = append(normalized, RequestedItem{ProductID: productID, Quantity: uint32(quantity)})
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].ProductID < normalized[j].ProductID
	})
	return normalized, nil
}

func canonicalRequestHash(items []RequestedItem) ([32]byte, error) {
	normalized, err := normalizeRequestedItems(items)
	if err != nil {
		return [32]byte{}, err
	}

	// The fixed-width binary form prevents ambiguous text encodings and guarantees
	// the same logical basket hashes identically regardless of original item order.
	buf := make([]byte, 0, len(normalized)*(8+4))
	for _, item := range normalized {
		var chunk [12]byte
		binary.BigEndian.PutUint64(chunk[:8], item.ProductID)
		binary.BigEndian.PutUint32(chunk[8:], item.Quantity)
		buf = append(buf, chunk[:]...)
	}
	return sha256.Sum256(buf), nil
}

func checkedMultiply(money Money, quantity uint32) (Money, error) {
	if money.Currency == "" {
		return Money{}, ErrInvalidCurrency
	}
	if money.Amount <= 0 {
		return Money{}, ErrInvalidMoney
	}
	if quantity == 0 {
		return Money{}, ErrInvalidMoney
	}
	if money.Amount > math.MaxInt64/int64(quantity) {
		return Money{}, ErrInvalidMoney
	}
	return Money{Amount: money.Amount * int64(quantity), Currency: money.Currency}, nil
}

func checkedAdd(left Money, right Money) (Money, error) {
	if left == (Money{}) {
		return right, nil
	}
	if left.Currency != right.Currency {
		return Money{}, ErrInvalidCurrency
	}
	if right.Amount > math.MaxInt64-left.Amount {
		return Money{}, ErrInvalidMoney
	}
	return Money{Amount: left.Amount + right.Amount, Currency: left.Currency}, nil
}

package product

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

type Service struct {
	store Store
	clock Clock
}

func NewService(store Store, clock Clock) (*Service, error) {
	if store == nil {
		return nil, errors.New("new product service: store is required")
	}
	if clock == nil {
		return nil, errors.New("new product service: clock is required")
	}
	return &Service{store: store, clock: clock}, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*Product, error) {
	sku, err := normalizeSKU(input.SKU)
	if err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}
	name, err := normalizeName(input.Name)
	if err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}
	price, err := normalizeMoney(input.Price)
	if err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}
	stock, err := normalizeStock(input.InitialStock)
	if err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}

	now := s.clock.Now()
	product := &Product{
		SKU:         sku,
		Name:        name,
		Description: input.Description,
		Price:       price,
		Stock:       stock,
		Status:      StatusDraft,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.store.Create(ctx, product); err != nil {
		return nil, fmt.Errorf("create product: persist: %w", err)
	}
	return cloneProduct(product), nil
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (*Product, error) {
	product, err := s.AdminByID(ctx, input.ID)
	if err != nil {
		return nil, fmt.Errorf("update product %d: %w", input.ID, err)
	}

	sku, err := normalizeSKU(input.SKU)
	if err != nil {
		return nil, fmt.Errorf("update product %d: %w", input.ID, err)
	}
	name, err := normalizeName(input.Name)
	if err != nil {
		return nil, fmt.Errorf("update product %d: %w", input.ID, err)
	}
	price, err := normalizeMoney(input.Price)
	if err != nil {
		return nil, fmt.Errorf("update product %d: %w", input.ID, err)
	}

	product.SKU = sku
	product.Name = name
	product.Description = input.Description
	product.Price = price
	// Keep the persisted stock untouched so inventory can only move through explicit stock adjustments.
	product.Version++
	product.UpdatedAt = s.clock.Now()

	if err := s.store.Update(ctx, product, input.ExpectedVersion); err != nil {
		return nil, fmt.Errorf("update product %d: %w", input.ID, err)
	}
	return cloneProduct(product), nil
}

func (s *Service) Publish(ctx context.Context, id uint64, expectedVersion uint64) (*Product, error) {
	return s.setStatus(ctx, id, expectedVersion, StatusActive)
}

func (s *Service) Unpublish(ctx context.Context, id uint64, expectedVersion uint64) (*Product, error) {
	return s.setStatus(ctx, id, expectedVersion, StatusInactive)
}

func (s *Service) setStatus(ctx context.Context, id uint64, expectedVersion uint64, status Status) (*Product, error) {
	product, err := s.AdminByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("set product %d status %q: %w", id, status, err)
	}
	product.Status = status
	product.Version++
	product.UpdatedAt = s.clock.Now()
	if err := s.store.Update(ctx, product, expectedVersion); err != nil {
		return nil, fmt.Errorf("set product %d status %q: %w", id, status, err)
	}
	return cloneProduct(product), nil
}

func (s *Service) PublicByID(ctx context.Context, id uint64) (*Product, error) {
	product, err := s.store.ByID(ctx, id, true)
	if err != nil {
		return nil, fmt.Errorf("get public product %d: %w", id, err)
	}
	if product == nil {
		return nil, fmt.Errorf("get public product %d: %w", id, ErrNotFound)
	}
	return cloneProduct(product), nil
}

func (s *Service) AdminByID(ctx context.Context, id uint64) (*Product, error) {
	product, err := s.store.ByID(ctx, id, false)
	if err != nil {
		return nil, fmt.Errorf("get product %d: %w", id, err)
	}
	if product == nil {
		return nil, fmt.Errorf("get product %d: %w", id, ErrNotFound)
	}
	return cloneProduct(product), nil
}

func (s *Service) ListPublic(ctx context.Context, filter ListFilter) (Page, error) {
	filter.Status = StatusActive
	if err := filter.Validate(); err != nil {
		return Page{}, fmt.Errorf("list products: %w", err)
	}
	// Public list endpoints must never expose inactive catalog items, regardless of caller input.
	page, err := s.store.List(ctx, filter, true)
	if err != nil {
		return Page{}, fmt.Errorf("list products: %w", err)
	}
	return clonePage(page), nil
}

func (s *Service) ListAdmin(ctx context.Context, filter ListFilter) (Page, error) {
	if err := filter.Validate(); err != nil {
		return Page{}, fmt.Errorf("list products: %w", err)
	}
	page, err := s.store.List(ctx, filter, false)
	if err != nil {
		return Page{}, fmt.Errorf("list products: %w", err)
	}
	return clonePage(page), nil
}

func (s *Service) AdjustStock(ctx context.Context, actor Actor, input AdjustStockInput) (*Product, error) {
	if actor.UserID == 0 {
		return nil, fmt.Errorf("adjust stock for product %d: %w", input.ProductID, ErrForbidden)
	}
	if input.Delta == 0 {
		return nil, fmt.Errorf("adjust stock for product %d: %w", input.ProductID, ErrInvalidStockAdjustment)
	}
	reason := strings.TrimSpace(input.Reason)
	if !utf8.ValidString(reason) || reason == "" {
		return nil, fmt.Errorf("adjust stock for product %d: %w", input.ProductID, ErrInvalidStockAdjustment)
	}

	product, err := s.AdminByID(ctx, input.ProductID)
	if err != nil {
		return nil, fmt.Errorf("adjust stock for product %d: %w", input.ProductID, err)
	}

	// Compute the post-adjustment stock in memory so we can reject insufficient-stock
	// attempts before mutating persistence.
	newStock := int64(product.Stock) + input.Delta
	if newStock < 0 {
		return nil, fmt.Errorf("adjust stock for product %d: %w", input.ProductID, ErrInsufficientStock)
	}
	if newStock > int64(math.MaxUint32) {
		return nil, fmt.Errorf("adjust stock for product %d: %w", input.ProductID, ErrInvalidStock)
	}

	adjustment := AdjustmentInput{
		ProductID:  input.ProductID,
		Delta:      input.Delta,
		StockAfter: uint32(newStock),
		Reason:     reason,
		Version:    input.Version,
		Actor:      actor,
		UpdatedAt:  s.clock.Now(),
	}
	product, err = s.store.AdjustStock(ctx, adjustment)
	if err != nil {
		return nil, fmt.Errorf("adjust stock for product %d: %w", input.ProductID, err)
	}
	return cloneProduct(product), nil
}

func normalizeSKU(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || !utf8.ValidString(normalized) {
		return "", &ValidationError{Field: "sku", Message: "must be valid UTF-8 and not empty", Err: ErrInvalidSKU}
	}
	return strings.ToUpper(normalized), nil
}

func normalizeName(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || !utf8.ValidString(normalized) {
		return "", &ValidationError{Field: "name", Message: "must be valid UTF-8 and not empty", Err: ErrInvalidName}
	}
	return normalized, nil
}

func normalizeMoney(money Money) (Money, error) {
	currency := strings.TrimSpace(money.Currency)
	if currency == "" || !utf8.ValidString(currency) {
		return Money{}, &ValidationError{Field: "currency", Message: "must be valid and supported", Err: ErrInvalidCurrency}
	}
	currency = strings.ToUpper(currency)
	if _, ok := supportedCurrencies[currency]; !ok {
		return Money{}, &ValidationError{Field: "currency", Message: fmt.Sprintf("%q is not supported", currency), Err: ErrInvalidCurrency}
	}
	if money.Amount <= 0 {
		return Money{}, &ValidationError{Field: "price", Message: "must be greater than 0", Err: ErrInvalidPrice}
	}

	return Money{Amount: money.Amount, Currency: currency}, nil
}

func normalizeStock(stock int64) (uint32, error) {
	if stock < 0 || stock > int64(math.MaxUint32) {
		return 0, &ValidationError{Field: "initial_stock", Message: "must be between 0 and 4294967295", Err: ErrInvalidStock}
	}
	return uint32(stock), nil
}

func cloneProduct(product *Product) *Product {
	if product == nil {
		return nil
	}
	cloned := *product
	return &cloned
}

func clonePage(page Page) Page {
	cloned := page
	cloned.Items = append([]Product(nil), page.Items...)
	return cloned
}

var supportedCurrencies = map[string]struct{}{
	"CNY": {},
	"USD": {},
	"EUR": {},
	"JPY": {},
	"GBP": {},
}

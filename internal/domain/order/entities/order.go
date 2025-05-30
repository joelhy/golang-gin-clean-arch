package entities

import (
	"time"

	sharedEntities "clean-arch-gin/internal/domain/shared/entities"
)

// OrderStatus represents the status of an order
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusConfirmed OrderStatus = "confirmed"
	OrderStatusShipped   OrderStatus = "shipped"
	OrderStatusDelivered OrderStatus = "delivered"
	OrderStatusCancelled OrderStatus = "cancelled"
)

// Order represents the order aggregate root
type Order struct {
	ID          uint
	UserID      uint
	Status      OrderStatus
	TotalAmount float64
	Items       []*OrderItem
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

// OrderItem represents an item within an order
type OrderItem struct {
	ID        uint
	OrderID   uint
	ProductID uint
	Quantity  int
	Price     float64
	CreatedAt time.Time
}

// NewOrder creates a new order with validation
func NewOrder(userID uint, items []*OrderItem) (*Order, error) {
	if userID == 0 {
		return nil, ErrInvalidUserID
	}
	if len(items) == 0 {
		return nil, ErrEmptyOrder
	}

	order := &Order{
		UserID:    userID,
		Status:    OrderStatusPending,
		Items:     items,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Calculate total amount
	order.calculateTotal()

	return order, nil
}

// AddItem adds an item to the order
func (o *Order) AddItem(productID uint, quantity int, price float64) error {
	if o.Status != OrderStatusPending {
		return ErrOrderNotModifiable
	}

	item := &OrderItem{
		ProductID: productID,
		Quantity:  quantity,
		Price:     price,
		CreatedAt: time.Now(),
	}

	o.Items = append(o.Items, item)
	o.calculateTotal()
	o.UpdatedAt = time.Now()

	return nil
}

// RemoveItem removes an item from the order
func (o *Order) RemoveItem(itemID uint) error {
	if o.Status != OrderStatusPending {
		return ErrOrderNotModifiable
	}

	for i, item := range o.Items {
		if item.ID == itemID {
			o.Items = append(o.Items[:i], o.Items[i+1:]...)
			o.calculateTotal()
			o.UpdatedAt = time.Now()
			return nil
		}
	}

	return ErrOrderItemNotFound
}

// Confirm changes order status to confirmed
func (o *Order) Confirm() error {
	if o.Status != OrderStatusPending {
		return ErrInvalidOrderStatusTransition
	}

	o.Status = OrderStatusConfirmed
	o.UpdatedAt = time.Now()
	return nil
}

// Ship changes order status to shipped
func (o *Order) Ship() error {
	if o.Status != OrderStatusConfirmed {
		return ErrInvalidOrderStatusTransition
	}

	o.Status = OrderStatusShipped
	o.UpdatedAt = time.Now()
	return nil
}

// Deliver changes order status to delivered
func (o *Order) Deliver() error {
	if o.Status != OrderStatusShipped {
		return ErrInvalidOrderStatusTransition
	}

	o.Status = OrderStatusDelivered
	o.UpdatedAt = time.Now()
	return nil
}

// Cancel cancels the order
func (o *Order) Cancel() error {
	if o.Status == OrderStatusDelivered {
		return ErrCannotCancelDeliveredOrder
	}

	o.Status = OrderStatusCancelled
	o.UpdatedAt = time.Now()
	return nil
}

// IsDeleted checks if the order is soft deleted
func (o *Order) IsDeleted() bool {
	return o.DeletedAt != nil
}

// MarkAsDeleted soft deletes the order
func (o *Order) MarkAsDeleted() {
	now := time.Now()
	o.DeletedAt = &now
	o.UpdatedAt = now
}

// calculateTotal calculates the total amount of the order
func (o *Order) calculateTotal() {
	total := 0.0
	for _, item := range o.Items {
		total += item.Price * float64(item.Quantity)
	}
	o.TotalAmount = total
}

// Domain errors for order
var (
	ErrInvalidUserID                = sharedEntities.DomainError{Message: "invalid user ID"}
	ErrEmptyOrder                   = sharedEntities.DomainError{Message: "order must contain at least one item"}
	ErrOrderNotModifiable           = sharedEntities.DomainError{Message: "order cannot be modified in current status"}
	ErrOrderItemNotFound            = sharedEntities.DomainError{Message: "order item not found"}
	ErrInvalidOrderStatusTransition = sharedEntities.DomainError{Message: "invalid order status transition"}
	ErrCannotCancelDeliveredOrder   = sharedEntities.DomainError{Message: "cannot cancel delivered order"}
	ErrOrderNotFound                = sharedEntities.DomainError{Message: "order not found"}
)

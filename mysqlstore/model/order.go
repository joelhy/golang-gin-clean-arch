package model

import "time"

// Order is the persistence representation of a customer order.
type Order struct {
	ID          uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	Number      string    `gorm:"column:number;type:varchar(40);not null"`
	UserID      uint64    `gorm:"column:user_id;type:bigint unsigned;not null"`
	Status      string    `gorm:"column:status;type:varchar(16);not null"`
	TotalAmount int64     `gorm:"column:total_amount;type:bigint;not null"`
	Currency    string    `gorm:"column:currency;type:char(3);not null"`
	Version     uint64    `gorm:"column:version;type:bigint unsigned;not null"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(6);not null"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime(6);not null"`
}

func (Order) TableName() string { return "orders" }

// OrderItem stores immutable product identity and price snapshots so later
// catalog changes cannot alter the historical meaning of an accepted order.
type OrderItem struct {
	ID              uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	OrderID         uint64    `gorm:"column:order_id;type:bigint unsigned;not null"`
	ProductID       uint64    `gorm:"column:product_id;type:bigint unsigned;not null"`
	ProductSKU      string    `gorm:"column:product_sku;type:varchar(64);not null"`
	ProductName     string    `gorm:"column:product_name;type:varchar(200);not null"`
	UnitPriceAmount int64     `gorm:"column:unit_price_amount;type:bigint;not null"`
	Currency        string    `gorm:"column:currency;type:char(3);not null"`
	Quantity        uint32    `gorm:"column:quantity;type:int unsigned;not null"`
	SubtotalAmount  int64     `gorm:"column:subtotal_amount;type:bigint;not null"`
	CreatedAt       time.Time `gorm:"column:created_at;type:datetime(6);not null"`
}

func (OrderItem) TableName() string { return "order_items" }

// IdempotencyKey binds a canonical request digest to the eventual order result.
type IdempotencyKey struct {
	ID             uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	UserID         uint64    `gorm:"column:user_id;type:bigint unsigned;not null"`
	Operation      string    `gorm:"column:operation;type:varchar(64);not null"`
	IdempotencyKey string    `gorm:"column:idempotency_key;type:varchar(128);not null"`
	RequestHash    []byte    `gorm:"column:request_hash;type:binary(32);size:32;not null"`
	OrderID        *uint64   `gorm:"column:order_id;type:bigint unsigned"`
	CreatedAt      time.Time `gorm:"column:created_at;type:datetime(6);not null"`
	UpdatedAt      time.Time `gorm:"column:updated_at;type:datetime(6);not null"`
}

func (IdempotencyKey) TableName() string { return "idempotency_keys" }

package model

import "time"

// Product is the persistence representation of a catalog item and its stock.
type Product struct {
	ID          uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	SKU         string    `gorm:"column:sku;type:varchar(64);not null"`
	Name        string    `gorm:"column:name;type:varchar(200);not null"`
	Description *string   `gorm:"column:description;type:text"`
	PriceAmount int64     `gorm:"column:price_amount;type:bigint;not null"`
	Currency    string    `gorm:"column:currency;type:char(3);not null"`
	Stock       uint32    `gorm:"column:stock;type:int unsigned;not null"`
	Status      string    `gorm:"column:status;type:varchar(16);not null"`
	Version     uint64    `gorm:"column:version;type:bigint unsigned;not null"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(6);not null"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime(6);not null"`
}

func (Product) TableName() string { return "products" }

// StockAdjustment is an append-only audit record of a stock change.
type StockAdjustment struct {
	ID          uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	ProductID   uint64    `gorm:"column:product_id;type:bigint unsigned;not null"`
	Delta       int32     `gorm:"column:delta;type:int;not null"`
	StockAfter  uint32    `gorm:"column:stock_after;type:int unsigned;not null"`
	Reason      string    `gorm:"column:reason;type:varchar(64);not null"`
	ActorUserID uint64    `gorm:"column:actor_user_id;type:bigint unsigned;not null"`
	OrderID     *uint64   `gorm:"column:order_id;type:bigint unsigned"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(6);not null"`
}

func (StockAdjustment) TableName() string { return "stock_adjustments" }

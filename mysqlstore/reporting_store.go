package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"clean-arch-gin/reporting"
	"gorm.io/gorm"
)

type ReportingStore struct {
	db *gorm.DB
}

func NewReportingStore(db *gorm.DB) (*ReportingStore, error) {
	if db == nil {
		return nil, errors.New("new MySQL reporting store: database is required")
	}
	return &ReportingStore{db: db}, nil
}

func (s *ReportingStore) Snapshot(ctx context.Context, window reporting.Range) (reporting.Snapshot, error) {
	users, err := s.loadUsers(ctx, window)
	if err != nil {
		return reporting.Snapshot{}, err
	}
	inventory, err := s.loadInventory(ctx)
	if err != nil {
		return reporting.Snapshot{}, err
	}
	orders, err := s.loadOrders(ctx, window)
	if err != nil {
		return reporting.Snapshot{}, err
	}
	return reporting.Snapshot{Users: users, Inventory: inventory, Orders: orders}, nil
}

func (s *ReportingStore) loadUsers(ctx context.Context, window reporting.Range) (reporting.Users, error) {
	var row struct {
		Total    uint64
		Active   uint64
		NewUsers uint64 `gorm:"column:new_users"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0) AS active,
			COALESCE(SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END), 0) AS new_users
		FROM users
	`, window.From, window.To).Scan(&row).Error; err != nil {
		return reporting.Users{}, fmt.Errorf("reporting users snapshot: %w", err)
	}
	return reporting.Users{Total: row.Total, Active: row.Active, New: row.NewUsers}, nil
}

func (s *ReportingStore) loadInventory(ctx context.Context) (reporting.Inventory, error) {
	var row struct {
		ActiveProducts uint64 `gorm:"column:active_products"`
		UnitsInStock   uint64 `gorm:"column:units_in_stock"`
		StockValue     int64  `gorm:"column:stock_value"`
		Currency       string `gorm:"column:currency"`
		CurrencyCount  uint64 `gorm:"column:currency_count"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) AS active_products,
			COALESCE(SUM(stock), 0) AS units_in_stock,
			COALESCE(SUM(price_amount * stock), 0) AS stock_value,
			COALESCE(MIN(currency), '') AS currency,
			COUNT(DISTINCT currency) AS currency_count
		FROM products
		WHERE status = 'active'
	`).Scan(&row).Error; err != nil {
		return reporting.Inventory{}, fmt.Errorf("reporting inventory snapshot: %w", err)
	}
	if row.CurrencyCount > 1 {
		return reporting.Inventory{}, reporting.ErrMixedCurrency
	}
	return reporting.Inventory{
		ActiveProducts: row.ActiveProducts,
		UnitsInStock:   row.UnitsInStock,
		StockValue:     row.StockValue,
		Currency:       strings.ToUpper(strings.TrimSpace(row.Currency)),
	}, nil
}

func (s *ReportingStore) loadOrders(ctx context.Context, window reporting.Range) (reporting.Orders, error) {
	var row struct {
		Total         uint64
		Pending       uint64
		Confirmed     uint64
		Shipped       uint64
		Delivered     uint64
		Cancelled     uint64
		GrossAmount   int64  `gorm:"column:gross_amount"`
		Currency      string `gorm:"column:currency"`
		CurrencyCount uint64 `gorm:"column:currency_count"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0) AS pending,
			COALESCE(SUM(CASE WHEN status = 'confirmed' THEN 1 ELSE 0 END), 0) AS confirmed,
			COALESCE(SUM(CASE WHEN status = 'shipped' THEN 1 ELSE 0 END), 0) AS shipped,
			COALESCE(SUM(CASE WHEN status = 'delivered' THEN 1 ELSE 0 END), 0) AS delivered,
			COALESCE(SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END), 0) AS cancelled,
			COALESCE(SUM(total_amount), 0) AS gross_amount,
			COALESCE(MIN(currency), '') AS currency,
			COUNT(DISTINCT currency) AS currency_count
		FROM orders
		WHERE created_at >= ? AND created_at < ?
	`, window.From, window.To).Scan(&row).Error; err != nil {
		return reporting.Orders{}, fmt.Errorf("reporting orders snapshot: %w", err)
	}
	if row.CurrencyCount > 1 {
		return reporting.Orders{}, reporting.ErrMixedCurrency
	}
	return reporting.Orders{
		Total:       row.Total,
		Pending:     row.Pending,
		Confirmed:   row.Confirmed,
		Shipped:     row.Shipped,
		Delivered:   row.Delivered,
		Cancelled:   row.Cancelled,
		GrossAmount: row.GrossAmount,
		Currency:    strings.ToUpper(strings.TrimSpace(row.Currency)),
	}, nil
}

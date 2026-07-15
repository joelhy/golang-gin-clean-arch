package order

import (
	"context"
	"fmt"
	"sort"
)

func (s *Service) ByID(ctx context.Context, actor Actor, orderID uint64) (*Order, error) {
	order, err := s.reader.ByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("get order %d: %w", orderID, err)
	}
	if order == nil {
		return nil, fmt.Errorf("get order %d: %w", orderID, ErrNotFound)
	}
	if !actor.Admin && order.UserID != actor.UserID {
		return nil, fmt.Errorf("get order %d: %w", orderID, ErrForbidden)
	}
	return cloneOrder(order), nil
}

func (s *Service) List(ctx context.Context, actor Actor, filter ListFilter) (Page, error) {
	if !actor.Admin {
		if actor.UserID == 0 {
			return Page{}, fmt.Errorf("list orders: %w", ErrForbidden)
		}
		filter.UserID = actor.UserID
	}
	if err := filter.Validate(); err != nil {
		return Page{}, fmt.Errorf("list orders: %w", err)
	}
	page, err := s.reader.List(ctx, filter)
	if err != nil {
		return Page{}, fmt.Errorf("list orders: %w", err)
	}
	return clonePage(page), nil
}

func sortOrders(items []Order, filter ListFilter) {
	field := filter.Sort
	if field == "" {
		field = "created_at"
	}

	sort.SliceStable(items, func(i, j int) bool {
		var less bool
		switch field {
		case "id":
			less = items[i].ID < items[j].ID
		case "number":
			less = items[i].Number < items[j].Number
		case "status":
			less = items[i].Status < items[j].Status
		case "total":
			less = items[i].Total.Amount < items[j].Total.Amount
		case "updated_at":
			less = items[i].UpdatedAt.Before(items[j].UpdatedAt)
		case "created_at":
			fallthrough
		default:
			less = items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		if filter.Descending {
			return !less && !equalOrderField(items[i], items[j], field)
		}
		return less
	})
}

func equalOrderField(left Order, right Order, field string) bool {
	switch field {
	case "id":
		return left.ID == right.ID
	case "number":
		return left.Number == right.Number
	case "status":
		return left.Status == right.Status
	case "total":
		return left.Total.Amount == right.Total.Amount
	case "updated_at":
		return left.UpdatedAt.Equal(right.UpdatedAt)
	default:
		return left.CreatedAt.Equal(right.CreatedAt)
	}
}

func cloneOrder(order *Order) *Order {
	if order == nil {
		return nil
	}
	cloned := *order
	cloned.Items = append([]Item(nil), order.Items...)
	return &cloned
}

func clonePage(page Page) Page {
	cloned := page
	cloned.Items = append([]Order(nil), page.Items...)
	for i := range cloned.Items {
		cloned.Items[i].Items = append([]Item(nil), page.Items[i].Items...)
	}
	return cloned
}

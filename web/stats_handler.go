package web

import (
	"context"
	"net/http"
	"net/url"

	"clean-arch-gin/reporting"
	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type statsService interface {
	Snapshot(context.Context, reporting.Range) (reporting.Snapshot, error)
}

type statsHandler struct {
	service statsService
}

type statsSnapshotDTO struct {
	Users     statsUsersDTO     `json:"users"`
	Inventory statsInventoryDTO `json:"inventory"`
	Orders    statsOrdersDTO    `json:"orders"`
}

type statsUsersDTO struct {
	Total  uint64 `json:"total"`
	Active uint64 `json:"active"`
	New    uint64 `json:"new"`
}

type statsInventoryDTO struct {
	ActiveProducts uint64   `json:"active_products"`
	UnitsInStock   uint64   `json:"units_in_stock"`
	StockValue     moneyDTO `json:"stock_value"`
	Currency       string   `json:"currency"`
}

type statsOrdersDTO struct {
	Total       uint64   `json:"total"`
	Pending     uint64   `json:"pending"`
	Confirmed   uint64   `json:"confirmed"`
	Shipped     uint64   `json:"shipped"`
	Delivered   uint64   `json:"delivered"`
	Cancelled   uint64   `json:"cancelled"`
	GrossAmount moneyDTO `json:"gross_amount"`
	Currency    string   `json:"currency"`
}

func NewStatsHandler(service statsService) *statsHandler {
	return &statsHandler{service: service}
}

func (h *statsHandler) Get(c *gin.Context) {
	if _, _, ok := requireIdentityAndPermission(c, user.PermissionStatsRead); !ok {
		return
	}

	window, problem := decodeStatsRange(c.Request.URL.Query())
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	snapshot, err := h.service.Snapshot(c.Request.Context(), window)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, snapshotToDTO(snapshot))
}

func decodeStatsRange(values url.Values) (reporting.Range, *Problem) {
	from, ok, problem := singletonUTCTime(values, "from")
	if problem != nil {
		return reporting.Range{}, problem
	}
	if !ok {
		problem := validationProblem("from", "required")
		return reporting.Range{}, &problem
	}

	to, ok, problem := singletonUTCTime(values, "to")
	if problem != nil {
		return reporting.Range{}, problem
	}
	if !ok {
		problem := validationProblem("to", "required")
		return reporting.Range{}, &problem
	}
	return reporting.Range{From: from, To: to}, nil
}

func snapshotToDTO(snapshot reporting.Snapshot) statsSnapshotDTO {
	return statsSnapshotDTO{
		Users: statsUsersDTO{
			Total:  snapshot.Users.Total,
			Active: snapshot.Users.Active,
			New:    snapshot.Users.New,
		},
		Inventory: statsInventoryDTO{
			ActiveProducts: snapshot.Inventory.ActiveProducts,
			UnitsInStock:   snapshot.Inventory.UnitsInStock,
			StockValue:     moneyDTO{Amount: snapshot.Inventory.StockValue, Currency: snapshot.Inventory.Currency},
			Currency:       snapshot.Inventory.Currency,
		},
		Orders: statsOrdersDTO{
			Total:       snapshot.Orders.Total,
			Pending:     snapshot.Orders.Pending,
			Confirmed:   snapshot.Orders.Confirmed,
			Shipped:     snapshot.Orders.Shipped,
			Delivered:   snapshot.Orders.Delivered,
			Cancelled:   snapshot.Orders.Cancelled,
			GrossAmount: moneyDTO{Amount: snapshot.Orders.GrossAmount, Currency: snapshot.Orders.Currency},
			Currency:    snapshot.Orders.Currency,
		},
	}
}

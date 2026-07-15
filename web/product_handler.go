package web

import (
	"context"
	"net/http"
	"time"

	"clean-arch-gin/product"
	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type productService interface {
	Create(context.Context, product.CreateInput) (*product.Product, error)
	Update(context.Context, product.UpdateInput) (*product.Product, error)
	Publish(context.Context, uint64, uint64) (*product.Product, error)
	Unpublish(context.Context, uint64, uint64) (*product.Product, error)
	PublicByID(context.Context, uint64) (*product.Product, error)
	AdminByID(context.Context, uint64) (*product.Product, error)
	ListPublic(context.Context, product.ListFilter) (product.Page, error)
	ListAdmin(context.Context, product.ListFilter) (product.Page, error)
	AdjustStock(context.Context, product.Actor, product.AdjustStockInput) (*product.Product, error)
}

type productHandler struct {
	service productService
}

type moneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type productDTO struct {
	ID          uint64   `json:"id"`
	SKU         string   `json:"sku"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Price       moneyDTO `json:"price"`
	Stock       uint32   `json:"stock"`
	Status      string   `json:"status"`
	Version     uint64   `json:"version"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

type createProductRequest struct {
	SKU          string   `json:"sku"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Price        moneyDTO `json:"price"`
	InitialStock int64    `json:"initial_stock"`
}

type updateProductRequest struct {
	SKU         string   `json:"sku"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Price       moneyDTO `json:"price"`
	Version     uint64   `json:"version"`
}

type versionRequest struct {
	Version uint64 `json:"version"`
}

type adjustStockRequest struct {
	Delta   int64  `json:"delta"`
	Reason  string `json:"reason"`
	Version uint64 `json:"version"`
}

func NewProductHandler(service productService) *productHandler {
	return &productHandler{service: service}
}

func (h *productHandler) ListPublic(c *gin.Context) {
	query, problem := DecodePaginationQuery(c.Request.URL.Query(), QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"id", "sku", "name", "status", "price", "created_at", "updated_at"},
	})
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	// Public catalog access always goes through the domain's active-only path. The
	// handler ignores status flags here so callers cannot opt into draft data.
	page, err := h.service.ListPublic(c.Request.Context(), product.ListFilter{
		Sort:       defaultSort(query.Sort),
		Descending: query.Direction == "desc",
		Limit:      query.Pagination.Limit,
		Offset:     query.Pagination.Offset,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, productPageDTO(page))
}

func (h *productHandler) GetPublic(c *gin.Context) {
	id, problem := parseUintParam(c, "id")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}
	item, err := h.service.PublicByID(c.Request.Context(), id)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, productToDTO(item))
}

func (h *productHandler) Create(c *gin.Context) {
	if _, _, ok := requireIdentityAndPermission(c, user.PermissionProductsWrite); !ok {
		return
	}

	var req createProductRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	created, err := h.service.Create(c.Request.Context(), product.CreateInput{
		SKU:         req.SKU,
		Name:        req.Name,
		Description: req.Description,
		Price: product.Money{
			Amount:   req.Price.Amount,
			Currency: req.Price.Currency,
		},
		InitialStock: req.InitialStock,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusCreated, productToDTO(created))
}

func (h *productHandler) GetAdmin(c *gin.Context) {
	if _, _, ok := requireIdentityAndPermission(c, user.PermissionProductsWrite); !ok {
		return
	}
	id, problem := parseUintParam(c, "id")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}
	item, err := h.service.AdminByID(c.Request.Context(), id)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, productToDTO(item))
}

func (h *productHandler) Update(c *gin.Context) {
	if _, _, ok := requireIdentityAndPermission(c, user.PermissionProductsWrite); !ok {
		return
	}
	id, problem := parseUintParam(c, "id")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	var req updateProductRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	updated, err := h.service.Update(c.Request.Context(), product.UpdateInput{
		ID:          id,
		SKU:         req.SKU,
		Name:        req.Name,
		Description: req.Description,
		Price: product.Money{
			Amount:   req.Price.Amount,
			Currency: req.Price.Currency,
		},
		ExpectedVersion: req.Version,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, productToDTO(updated))
}

func (h *productHandler) ListAdmin(c *gin.Context) {
	if _, _, ok := requireIdentityAndPermission(c, user.PermissionProductsWrite); !ok {
		return
	}
	query, problem := DecodePaginationQuery(c.Request.URL.Query(), QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"id", "sku", "name", "status", "price", "created_at", "updated_at"},
	})
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}
	status, problem := optionalSingleton(c, "status")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	page, err := h.service.ListAdmin(c.Request.Context(), product.ListFilter{
		Status:     product.Status(status),
		Sort:       defaultSort(query.Sort),
		Descending: query.Direction == "desc",
		Limit:      query.Pagination.Limit,
		Offset:     query.Pagination.Offset,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, productPageDTO(page))
}

func (h *productHandler) Publish(c *gin.Context) {
	h.setProductStatus(c, true)
}

func (h *productHandler) Unpublish(c *gin.Context) {
	h.setProductStatus(c, false)
}

func (h *productHandler) AdjustStock(c *gin.Context) {
	identity, _, ok := requireIdentityAndPermission(c, user.PermissionProductsStock)
	if !ok {
		return
	}
	id, problem := parseUintParam(c, "id")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	var req adjustStockRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	updated, err := h.service.AdjustStock(c.Request.Context(), product.Actor{UserID: identity.UserID}, product.AdjustStockInput{
		ProductID: id,
		Delta:     req.Delta,
		Reason:    req.Reason,
		Version:   req.Version,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, productToDTO(updated))
}

func (h *productHandler) setProductStatus(c *gin.Context, publish bool) {
	if _, _, ok := requireIdentityAndPermission(c, user.PermissionProductsWrite); !ok {
		return
	}
	id, problem := parseUintParam(c, "id")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	var req versionRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	var (
		item *product.Product
		err  error
	)
	if publish {
		item, err = h.service.Publish(c.Request.Context(), id, req.Version)
	} else {
		item, err = h.service.Unpublish(c.Request.Context(), id, req.Version)
	}
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, productToDTO(item))
}

func productPageDTO(page product.Page) PageData {
	items := make([]productDTO, 0, len(page.Items))
	for i := range page.Items {
		items = append(items, productToDTO(&page.Items[i]))
	}
	return NewPageData(items, Pagination{Limit: page.Limit, Offset: page.Offset})
}

func productToDTO(value *product.Product) productDTO {
	if value == nil {
		return productDTO{}
	}
	return productDTO{
		ID:          value.ID,
		SKU:         value.SKU,
		Name:        value.Name,
		Description: value.Description,
		Price: moneyDTO{
			Amount:   value.Price.Amount,
			Currency: value.Price.Currency,
		},
		Stock:     value.Stock,
		Status:    string(value.Status),
		Version:   value.Version,
		CreatedAt: formatProductTime(value.CreatedAt),
		UpdatedAt: formatProductTime(value.UpdatedAt),
	}
}

func formatProductTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

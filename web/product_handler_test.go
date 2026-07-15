package web

import (
	"context"
	"net/http"
	"testing"
	"time"

	"clean-arch-gin/product"
	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type fakeProductHandlerService struct {
	createFn      func(context.Context, product.CreateInput) (*product.Product, error)
	updateFn      func(context.Context, product.UpdateInput) (*product.Product, error)
	publishFn     func(context.Context, uint64, uint64) (*product.Product, error)
	unpublishFn   func(context.Context, uint64, uint64) (*product.Product, error)
	publicByIDFn  func(context.Context, uint64) (*product.Product, error)
	adminByIDFn   func(context.Context, uint64) (*product.Product, error)
	listPublicFn  func(context.Context, product.ListFilter) (product.Page, error)
	listAdminFn   func(context.Context, product.ListFilter) (product.Page, error)
	adjustStockFn func(context.Context, product.Actor, product.AdjustStockInput) (*product.Product, error)
}

func (f fakeProductHandlerService) Create(ctx context.Context, input product.CreateInput) (*product.Product, error) {
	return f.createFn(ctx, input)
}

func (f fakeProductHandlerService) Update(ctx context.Context, input product.UpdateInput) (*product.Product, error) {
	return f.updateFn(ctx, input)
}

func (f fakeProductHandlerService) Publish(ctx context.Context, id uint64, version uint64) (*product.Product, error) {
	return f.publishFn(ctx, id, version)
}

func (f fakeProductHandlerService) Unpublish(ctx context.Context, id uint64, version uint64) (*product.Product, error) {
	return f.unpublishFn(ctx, id, version)
}

func (f fakeProductHandlerService) PublicByID(ctx context.Context, id uint64) (*product.Product, error) {
	return f.publicByIDFn(ctx, id)
}

func (f fakeProductHandlerService) AdminByID(ctx context.Context, id uint64) (*product.Product, error) {
	return f.adminByIDFn(ctx, id)
}

func (f fakeProductHandlerService) ListPublic(ctx context.Context, filter product.ListFilter) (product.Page, error) {
	return f.listPublicFn(ctx, filter)
}

func (f fakeProductHandlerService) ListAdmin(ctx context.Context, filter product.ListFilter) (product.Page, error) {
	return f.listAdminFn(ctx, filter)
}

func (f fakeProductHandlerService) AdjustStock(ctx context.Context, actor product.Actor, input product.AdjustStockInput) (*product.Product, error) {
	return f.adjustStockFn(ctx, actor, input)
}

func TestProductHandlerPublicListUsesPublicServiceOnly(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	publicCalls := 0
	handler := NewProductHandler(fakeProductHandlerService{
		listPublicFn: func(_ context.Context, filter product.ListFilter) (product.Page, error) {
			publicCalls++
			if filter.Status != "" || filter.Sort != "created_at" || !filter.Descending || filter.Limit != 5 || filter.Offset != 10 {
				t.Fatalf("filter = %+v", filter)
			}
			return product.Page{
				Items: []product.Product{{
					ID:     2,
					SKU:    "SKU-2",
					Name:   "Widget",
					Price:  product.Money{Amount: 1099, Currency: "USD"},
					Stock:  7,
					Status: product.StatusActive,
				}},
				Total:  1,
				Limit:  5,
				Offset: 10,
			}, nil
		},
		listAdminFn: func(_ context.Context, _ product.ListFilter) (product.Page, error) {
			t.Fatal("ListAdmin must not run for public list")
			return product.Page{}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/products", handler.ListPublic)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/products?status=draft&sort=created_at&direction=desc&limit=5&offset=10", nil, nil)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.items.0.price.amount", float64(1099))
	assertJSONPath(t, rec.Body.Bytes(), "data.items.0.price.currency", "USD")
	assertJSONPath(t, rec.Body.Bytes(), "data.pagination.offset", float64(10))
	if publicCalls != 1 {
		t.Fatalf("ListPublic calls = %d, want 1", publicCalls)
	}
}

func TestProductHandlerPublicByIDUsesPublicServiceOnly(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewProductHandler(fakeProductHandlerService{
		publicByIDFn: func(_ context.Context, id uint64) (*product.Product, error) {
			if id != 2 {
				t.Fatalf("id = %d", id)
			}
			return &product.Product{ID: 2, SKU: "SKU-2", Name: "Widget", Price: product.Money{Amount: 1099, Currency: "USD"}, Status: product.StatusActive}, nil
		},
		adminByIDFn: func(_ context.Context, _ uint64) (*product.Product, error) {
			t.Fatal("AdminByID must not run for public get")
			return nil, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/products/:id", handler.GetPublic)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/products/2", nil, nil)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.id", float64(2))
}

func TestProductHandlerAdminCreate(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	now := time.Date(2026, 7, 15, 12, 0, 0, 45, time.FixedZone("CST", 8*3600))
	handler := NewProductHandler(fakeProductHandlerService{
		createFn: func(_ context.Context, input product.CreateInput) (*product.Product, error) {
			if input.SKU != "sku-1" || input.Name != "Widget" || input.Description != "A widget" {
				t.Fatalf("input = %+v", input)
			}
			if input.Price.Amount != 1099 || input.Price.Currency != "USD" || input.InitialStock != 9 {
				t.Fatalf("input = %+v", input)
			}
			return &product.Product{
				ID:          3,
				SKU:         "SKU-1",
				Name:        "Widget",
				Description: "A widget",
				Price:       product.Money{Amount: 1099, Currency: "USD"},
				Stock:       9,
				Status:      product.StatusDraft,
				Version:     1,
				CreatedAt:   now,
				UpdatedAt:   now,
			}, nil
		},
	})

	router := gin.New()
	router.POST("/api/v1/admin/products", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionProductsWrite), handler.Create)

	rec := performJSON(t, router, http.MethodPost, "/api/v1/admin/products", `{"sku":"sku-1","name":"Widget","description":"A widget","price":{"amount":1099,"currency":"USD"},"initial_stock":9}`)

	assertStatusCode(t, rec, http.StatusCreated)
	assertJSONPath(t, rec.Body.Bytes(), "data.id", float64(3))
	assertJSONPath(t, rec.Body.Bytes(), "data.created_at", now.UTC().Format(time.RFC3339Nano))
}

func TestProductHandlerAdminUpdate(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewProductHandler(fakeProductHandlerService{
		updateFn: func(_ context.Context, input product.UpdateInput) (*product.Product, error) {
			if input.ID != 3 || input.ExpectedVersion != 4 {
				t.Fatalf("input = %+v", input)
			}
			return &product.Product{ID: 3, SKU: "SKU-1", Name: "Widget 2", Price: product.Money{Amount: 2099, Currency: "USD"}, Version: 5}, nil
		},
	})

	router := gin.New()
	router.PUT("/api/v1/admin/products/:id", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionProductsWrite), handler.Update)

	rec := performJSON(t, router, http.MethodPut, "/api/v1/admin/products/3", `{"sku":"sku-1","name":"Widget 2","description":"Second","price":{"amount":2099,"currency":"USD"},"version":4}`)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.version", float64(5))
}

func TestProductHandlerAdminByID(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewProductHandler(fakeProductHandlerService{
		adminByIDFn: func(_ context.Context, id uint64) (*product.Product, error) {
			if id != 3 {
				t.Fatalf("id = %d", id)
			}
			return &product.Product{ID: 3, SKU: "SKU-3", Name: "Widget 3", Price: product.Money{Amount: 2099, Currency: "USD"}, Status: product.StatusDraft}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/products/:id", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionProductsWrite), handler.GetAdmin)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/products/3", nil, nil)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.id", float64(3))
	assertJSONPath(t, rec.Body.Bytes(), "data.status", "draft")
}

func TestProductHandlerAdminList(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewProductHandler(fakeProductHandlerService{
		listAdminFn: func(_ context.Context, filter product.ListFilter) (product.Page, error) {
			if filter.Status != product.StatusDraft || filter.Sort != "price" || filter.Limit != 2 || filter.Offset != 4 {
				t.Fatalf("filter = %+v", filter)
			}
			if filter.Descending {
				t.Fatalf("filter.Descending = true, want false")
			}
			return product.Page{
				Items:  []product.Product{{ID: 4, SKU: "SKU-4", Name: "Draft", Price: product.Money{Amount: 999, Currency: "USD"}, Status: product.StatusDraft}},
				Total:  1,
				Limit:  2,
				Offset: 4,
			}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/products", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionProductsWrite), handler.ListAdmin)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/products?status=draft&sort=price&direction=asc&limit=2&offset=4", nil, nil)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.items.0.status", "draft")
	assertJSONPath(t, rec.Body.Bytes(), "data.pagination.limit", float64(2))
}

func TestProductHandlerAdminPublishAndUnpublish(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewProductHandler(fakeProductHandlerService{
		publishFn: func(_ context.Context, id uint64, version uint64) (*product.Product, error) {
			if id != 4 || version != 3 {
				t.Fatalf("publish id=%d version=%d", id, version)
			}
			return &product.Product{ID: 4, Status: product.StatusActive}, nil
		},
		unpublishFn: func(_ context.Context, id uint64, version uint64) (*product.Product, error) {
			if id != 4 || version != 4 {
				t.Fatalf("unpublish id=%d version=%d", id, version)
			}
			return &product.Product{ID: 4, Status: product.StatusInactive}, nil
		},
	})

	router := gin.New()
	router.POST("/api/v1/admin/products/:id/publish", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionProductsWrite), handler.Publish)
	router.POST("/api/v1/admin/products/:id/unpublish", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionProductsWrite), handler.Unpublish)

	publishRec := performJSON(t, router, http.MethodPost, "/api/v1/admin/products/4/publish", `{"version":3}`)
	unpublishRec := performJSON(t, router, http.MethodPost, "/api/v1/admin/products/4/unpublish", `{"version":4}`)

	assertStatusCode(t, publishRec, http.StatusOK)
	assertStatusCode(t, unpublishRec, http.StatusOK)
	assertJSONPath(t, publishRec.Body.Bytes(), "data.status", "active")
	assertJSONPath(t, unpublishRec.Body.Bytes(), "data.status", "inactive")
}

func TestProductHandlerAdjustStock(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewProductHandler(fakeProductHandlerService{
		adjustStockFn: func(_ context.Context, actor product.Actor, input product.AdjustStockInput) (*product.Product, error) {
			if actor.UserID != 77 || input.ProductID != 4 || input.Delta != -2 || input.Reason != "manual recount" || input.Version != 5 {
				t.Fatalf("actor=%+v input=%+v", actor, input)
			}
			return &product.Product{ID: 4, Stock: 7, Version: 6, Status: product.StatusActive}, nil
		},
	})

	router := gin.New()
	router.POST("/api/v1/admin/products/:id/stock", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionProductsStock), handler.AdjustStock)

	rec := performJSON(t, router, http.MethodPost, "/api/v1/admin/products/4/stock", `{"delta":-2,"reason":"manual recount","version":5}`)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.stock", float64(7))
}

package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"clean-arch-gin/order"
	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type orderService interface {
	Create(context.Context, uint64, order.CreateInput) (*order.Order, error)
	ByID(context.Context, order.Actor, uint64) (*order.Order, error)
	List(context.Context, order.Actor, order.ListFilter) (order.Page, error)
	Cancel(context.Context, order.Actor, uint64, uint64) (*order.Order, error)
	Confirm(context.Context, uint64, uint64) (*order.Order, error)
	Ship(context.Context, uint64, uint64) (*order.Order, error)
	Deliver(context.Context, uint64, uint64) (*order.Order, error)
}

type orderHandler struct {
	service orderService
}

type createOrderRequest struct {
	Items []createOrderItemRequest `json:"items"`
}

type createOrderItemRequest struct {
	ProductID uint64 `json:"product_id"`
	Quantity  uint32 `json:"quantity"`
}

type orderDTO struct {
	ID        uint64         `json:"id"`
	UserID    uint64         `json:"user_id"`
	Number    string         `json:"number"`
	Status    string         `json:"status"`
	Total     moneyDTO       `json:"total"`
	Items     []orderItemDTO `json:"items,omitempty"`
	Version   uint64         `json:"version"`
	CreatedAt string         `json:"created_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
}

type orderItemDTO struct {
	ID        uint64   `json:"id"`
	ProductID uint64   `json:"product_id"`
	SKU       string   `json:"sku"`
	Name      string   `json:"name"`
	UnitPrice moneyDTO `json:"unit_price"`
	Subtotal  moneyDTO `json:"subtotal"`
	Quantity  uint32   `json:"quantity"`
}

func NewOrderHandler(service orderService) *orderHandler {
	return &orderHandler{service: service}
}

func (h *orderHandler) Create(c *gin.Context) {
	identity, ok := identityFromContext(c)
	if !ok {
		writeFailure(c, http.StatusUnauthorized, Problem{Code: CodeAuthentication, Message: "authentication failed"})
		return
	}

	idempotencyKey, problem := parseIdempotencyKey(c)
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	var req createOrderRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	items := make([]order.RequestedItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, order.RequestedItem{ProductID: item.ProductID, Quantity: item.Quantity})
	}

	created, err := h.service.Create(c.Request.Context(), identity.UserID, order.CreateInput{
		IdempotencyKey: idempotencyKey,
		Items:          items,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusCreated, orderToDTO(created))
}

func (h *orderHandler) ListCustomer(c *gin.Context) {
	identity, ok := identityFromContext(c)
	if !ok {
		writeFailure(c, http.StatusUnauthorized, Problem{Code: CodeAuthentication, Message: "authentication failed"})
		return
	}

	filter, problem := decodeOrderFilter(c.Request.URL.Query())
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}
	filter.UserID = identity.UserID

	page, err := h.service.List(c.Request.Context(), order.Actor{UserID: identity.UserID}, filter)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, orderPageDTO(page))
}

func (h *orderHandler) GetCustomer(c *gin.Context) {
	identity, ok := identityFromContext(c)
	if !ok {
		writeFailure(c, http.StatusUnauthorized, Problem{Code: CodeAuthentication, Message: "authentication failed"})
		return
	}
	id, problem := parseUintParam(c, "id")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	item, err := h.service.ByID(c.Request.Context(), order.Actor{UserID: identity.UserID}, id)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, orderToDTO(item))
}

func (h *orderHandler) CancelCustomer(c *gin.Context) {
	identity, ok := identityFromContext(c)
	if !ok {
		writeFailure(c, http.StatusUnauthorized, Problem{Code: CodeAuthentication, Message: "authentication failed"})
		return
	}
	h.cancel(c, order.Actor{UserID: identity.UserID})
}

func (h *orderHandler) ListAdmin(c *gin.Context) {
	identity, _, ok := requireIdentityAndPermission(c, user.PermissionOrdersReadAll)
	if !ok {
		return
	}

	filter, problem := decodeOrderFilter(c.Request.URL.Query())
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	page, err := h.service.List(c.Request.Context(), order.Actor{UserID: identity.UserID, Admin: true}, filter)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, orderPageDTO(page))
}

func (h *orderHandler) Confirm(c *gin.Context) {
	h.advance(c, func(orderID uint64, version uint64) (*order.Order, error) {
		return h.service.Confirm(c.Request.Context(), orderID, version)
	})
}

func (h *orderHandler) Ship(c *gin.Context) {
	h.advance(c, func(orderID uint64, version uint64) (*order.Order, error) {
		return h.service.Ship(c.Request.Context(), orderID, version)
	})
}

func (h *orderHandler) Deliver(c *gin.Context) {
	h.advance(c, func(orderID uint64, version uint64) (*order.Order, error) {
		return h.service.Deliver(c.Request.Context(), orderID, version)
	})
}

func (h *orderHandler) CancelAdmin(c *gin.Context) {
	identity, _, ok := requireIdentityAndPermission(c, user.PermissionOrdersManage)
	if !ok {
		return
	}
	h.cancel(c, order.Actor{UserID: identity.UserID, Admin: true})
}

func (h *orderHandler) cancel(c *gin.Context, actor order.Actor) {
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

	updated, err := h.service.Cancel(c.Request.Context(), actor, id, req.Version)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, orderToDTO(updated))
}

func (h *orderHandler) advance(c *gin.Context, apply func(uint64, uint64) (*order.Order, error)) {
	if _, _, ok := requireIdentityAndPermission(c, user.PermissionOrdersManage); !ok {
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

	updated, err := apply(id, req.Version)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, orderToDTO(updated))
}

func parseIdempotencyKey(c *gin.Context) (string, *Problem) {
	values := c.Request.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		problem := validationProblem("idempotency_key", "required")
		return "", &problem
	}
	key := strings.TrimSpace(values[0])
	if len(key) < 1 || len(key) > 128 {
		problem := validationProblem("idempotency_key", "invalid_length")
		return "", &problem
	}
	for _, r := range key {
		if r < 0x21 || r > 0x7e {
			problem := validationProblem("idempotency_key", "invalid_character")
			return "", &problem
		}
	}
	return key, nil
}

func decodeOrderFilter(values url.Values) (order.ListFilter, *Problem) {
	query, problem := DecodePaginationQuery(values, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"id", "number", "status", "total", "created_at", "updated_at"},
		StartKey:     "created_from",
		EndKey:       "created_to",
	})
	if problem != nil {
		return order.ListFilter{}, problem
	}

	filter := order.ListFilter{
		Sort:        defaultSort(query.Sort),
		Descending:  query.Direction == "desc",
		Limit:       query.Pagination.Limit,
		Offset:      query.Pagination.Offset,
		CreatedFrom: query.StartAt,
		CreatedTo:   query.EndAt,
	}

	for _, raw := range values["status"] {
		filter.Statuses = append(filter.Statuses, order.Status(strings.TrimSpace(raw)))
	}

	min, ok, problem := singletonInt64(values, "min_total_amount")
	if problem != nil {
		return order.ListFilter{}, problem
	}
	if ok {
		filter.MinTotalAmount = &min
	}

	max, ok, problem := singletonInt64(values, "max_total_amount")
	if problem != nil {
		return order.ListFilter{}, problem
	}
	if ok {
		filter.MaxTotalAmount = &max
	}

	return filter, nil
}

func singletonInt64(values url.Values, key string) (int64, bool, *Problem) {
	raw, ok, problem := singletonString(values, key)
	if !ok || problem != nil {
		return 0, ok, problem
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		problem := validationProblem(key, "invalid_integer")
		return 0, false, &problem
	}
	return value, true, nil
}

func orderToDTO(value *order.Order) orderDTO {
	if value == nil {
		return orderDTO{}
	}
	items := make([]orderItemDTO, 0, len(value.Items))
	for _, item := range value.Items {
		items = append(items, orderItemDTO{
			ID:        item.ID,
			ProductID: item.ProductID,
			SKU:       item.SKU,
			Name:      item.Name,
			UnitPrice: moneyDTO{Amount: item.UnitPrice.Amount, Currency: item.UnitPrice.Currency},
			Subtotal:  moneyDTO{Amount: item.Subtotal.Amount, Currency: item.Subtotal.Currency},
			Quantity:  item.Quantity,
		})
	}
	return orderDTO{
		ID:        value.ID,
		UserID:    value.UserID,
		Number:    value.Number,
		Status:    string(value.Status),
		Total:     moneyDTO{Amount: value.Total.Amount, Currency: value.Total.Currency},
		Items:     items,
		Version:   value.Version,
		CreatedAt: formatUTCTime(value.CreatedAt),
		UpdatedAt: formatUTCTime(value.UpdatedAt),
	}
}

func orderPageDTO(page order.Page) PageData {
	items := make([]orderDTO, 0, len(page.Items))
	for i := range page.Items {
		items = append(items, orderToDTO(&page.Items[i]))
	}
	return NewPageData(items, Pagination{Limit: page.Limit, Offset: page.Offset})
}

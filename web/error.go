package web

import (
	"context"
	"errors"
	"net/http"

	"clean-arch-gin/order"
	"clean-arch-gin/product"
	"clean-arch-gin/user"
)

func problemFromError(err error) (int, Problem) {
	if err == nil {
		return http.StatusOK, Problem{Code: CodeOK}
	}

	var (
		userValidation    *user.ValidationError
		productValidation *product.ValidationError
		orderValidation   *order.ValidationError
	)
	switch {
	case errors.As(err, &userValidation):
		return http.StatusBadRequest, validationProblem(userValidation.Field, normalizeValidationReason(err, "invalid"))
	case errors.As(err, &productValidation):
		return http.StatusBadRequest, validationProblem(productValidation.Field, normalizeValidationReason(err, "invalid"))
	case errors.As(err, &orderValidation):
		return http.StatusBadRequest, validationProblem(orderValidation.Field, normalizeValidationReason(err, "invalid"))
	case isValidationSentinel(err):
		return http.StatusBadRequest, Problem{
			Code:    CodeValidation,
			Message: "request validation failed",
		}
	case errors.Is(err, user.ErrEmailExists):
		return http.StatusConflict, Problem{Code: CodeEmailExists, Message: "email already exists"}
	case errors.Is(err, user.ErrInvalidCredentials),
		errors.Is(err, user.ErrInvalidAccessToken),
		errors.Is(err, user.ErrInvalidRefresh),
		errors.Is(err, user.ErrDisabled):
		return http.StatusUnauthorized, Problem{Code: CodeAuthentication, Message: "authentication failed"}
	case errors.Is(err, user.ErrSessionExpired),
		errors.Is(err, user.ErrSessionRevoked),
		errors.Is(err, user.ErrRefreshReuse):
		return http.StatusUnauthorized, Problem{Code: CodeSessionExpired, Message: "session expired"}
	case errors.Is(err, user.ErrForbidden),
		errors.Is(err, user.ErrLastAdmin),
		errors.Is(err, product.ErrForbidden),
		errors.Is(err, order.ErrForbidden):
		return http.StatusForbidden, Problem{Code: CodePermission, Message: "permission denied"}
	case errors.Is(err, user.ErrNotFound),
		errors.Is(err, user.ErrRoleNotFound):
		return http.StatusNotFound, Problem{Code: CodeUserNotFound, Message: "user not found"}
	case errors.Is(err, product.ErrNotFound):
		return http.StatusNotFound, Problem{Code: CodeProductNotFound, Message: "product not found"}
	case errors.Is(err, order.ErrNotFound):
		return http.StatusNotFound, Problem{Code: CodeOrderNotFound, Message: "order not found"}
	case errors.Is(err, order.ErrInvalidTransition):
		return http.StatusConflict, Problem{Code: CodeInvalidTransition, Message: "invalid order transition"}
	case errors.Is(err, product.ErrInsufficientStock),
		errors.Is(err, order.ErrInsufficientStock):
		return http.StatusConflict, Problem{Code: CodeInsufficientStock, Message: "insufficient stock"}
	case errors.Is(err, user.ErrConflict),
		errors.Is(err, product.ErrConflict),
		errors.Is(err, order.ErrConflict):
		return http.StatusConflict, Problem{Code: CodeIdempotencyConflict, Message: "conflict"}
	case errors.Is(err, context.Canceled):
		return 499, Problem{Code: CodeInternal, Message: "request canceled"}
	default:
		return http.StatusInternalServerError, Problem{Code: CodeInternal, Message: "internal server error"}
	}
}

func validationProblem(field string, reason string) Problem {
	return Problem{
		Code:    CodeValidation,
		Message: "request validation failed",
		Data: FieldErrors{
			{Field: field, Reason: reason},
		},
	}
}

func normalizeValidationReason(err error, fallback string) string {
	switch {
	case errors.Is(err, user.ErrInvalidEmail):
		return "invalid_email"
	case errors.Is(err, user.ErrInvalidName), errors.Is(err, product.ErrInvalidName):
		return "invalid_name"
	case errors.Is(err, user.ErrWeakPassword):
		return "weak_password"
	case errors.Is(err, product.ErrInvalidSKU):
		return "invalid_sku"
	case errors.Is(err, product.ErrInvalidPrice):
		return "invalid_price"
	case errors.Is(err, product.ErrInvalidCurrency), errors.Is(err, order.ErrInvalidCurrency):
		return "invalid_currency"
	case errors.Is(err, product.ErrInvalidStock):
		return "invalid_stock"
	case errors.Is(err, product.ErrInvalidStatus), errors.Is(err, user.ErrInvalidStatus), errors.Is(err, order.ErrInvalidStatus):
		return "invalid_status"
	case errors.Is(err, product.ErrInvalidStockAdjustment):
		return "invalid_stock_adjustment"
	case errors.Is(err, order.ErrInvalidOrder):
		return "invalid_order"
	case errors.Is(err, order.ErrInvalidMoney):
		return "invalid_money"
	case errors.Is(err, user.ErrInvalidFilter), errors.Is(err, product.ErrInvalidFilter), errors.Is(err, order.ErrInvalidFilter):
		return "invalid_filter"
	default:
		return fallback
	}
}

func isValidationSentinel(err error) bool {
	return errors.Is(err, user.ErrInvalidEmail) ||
		errors.Is(err, user.ErrInvalidName) ||
		errors.Is(err, user.ErrWeakPassword) ||
		errors.Is(err, user.ErrInvalidStatus) ||
		errors.Is(err, user.ErrInvalidFilter) ||
		errors.Is(err, product.ErrInvalidSKU) ||
		errors.Is(err, product.ErrInvalidName) ||
		errors.Is(err, product.ErrInvalidPrice) ||
		errors.Is(err, product.ErrInvalidCurrency) ||
		errors.Is(err, product.ErrInvalidStock) ||
		errors.Is(err, product.ErrInvalidStatus) ||
		errors.Is(err, product.ErrInvalidFilter) ||
		errors.Is(err, product.ErrInvalidStockAdjustment) ||
		errors.Is(err, order.ErrInvalidOrder) ||
		errors.Is(err, order.ErrInvalidMoney) ||
		errors.Is(err, order.ErrInvalidCurrency) ||
		errors.Is(err, order.ErrInvalidStatus) ||
		errors.Is(err, order.ErrInvalidFilter)
}

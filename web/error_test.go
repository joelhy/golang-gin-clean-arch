package web

import (
	"errors"
	"net/http"
	"testing"

	"clean-arch-gin/order"
	"clean-arch-gin/product"
	"clean-arch-gin/user"
)

func TestWriteProblemFromErrorMapsSentinels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   int
	}{
		{
			name:       "user validation",
			err:        &user.ValidationError{Field: "email", Message: "is invalid", Err: user.ErrInvalidEmail},
			wantStatus: http.StatusBadRequest,
			wantCode:   CodeValidation,
		},
		{
			name:       "email exists",
			err:        user.ErrEmailExists,
			wantStatus: http.StatusConflict,
			wantCode:   CodeEmailExists,
		},
		{
			name:       "user not found",
			err:        user.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   CodeUserNotFound,
		},
		{
			name:       "product stock",
			err:        product.ErrInsufficientStock,
			wantStatus: http.StatusConflict,
			wantCode:   CodeInsufficientStock,
		},
		{
			name:       "order transition",
			err:        order.ErrInvalidTransition,
			wantStatus: http.StatusConflict,
			wantCode:   CodeInvalidTransition,
		},
		{
			name:       "auth",
			err:        user.ErrInvalidCredentials,
			wantStatus: http.StatusUnauthorized,
			wantCode:   CodeAuthentication,
		},
		{
			name:       "session expired",
			err:        user.ErrSessionExpired,
			wantStatus: http.StatusUnauthorized,
			wantCode:   CodeSessionExpired,
		},
		{
			name:       "permission",
			err:        user.ErrForbidden,
			wantStatus: http.StatusForbidden,
			wantCode:   CodePermission,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, problem := problemFromError(tc.err)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d", status, tc.wantStatus)
			}
			if problem.Code != tc.wantCode {
				t.Fatalf("problem code = %d, want %d", problem.Code, tc.wantCode)
			}
			if problem.Message == "" {
				t.Fatalf("problem message must not be empty: %+v", problem)
			}
		})
	}
}

func TestWriteProblemFromErrorSanitizesUnknownErrors(t *testing.T) {
	status, problem := problemFromError(errors.New("database password leaked"))

	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
	}
	if problem.Code != CodeInternal {
		t.Fatalf("problem code = %d, want %d", problem.Code, CodeInternal)
	}
	if problem.Message == "" {
		t.Fatal("problem message must not be empty")
	}
	if problem.Message == "database password leaked" {
		t.Fatalf("message leaked internals: %q", problem.Message)
	}
}

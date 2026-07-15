package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	CodeOK                  = 0
	CodeMalformedJSON       = 10000
	CodeValidation          = 10001
	CodeAuthentication      = 20000
	CodePermission          = 20001
	CodeSessionExpired      = 20002
	CodeUserNotFound        = 30000
	CodeEmailExists         = 30001
	CodeProductNotFound     = 40000
	CodeInsufficientStock   = 40001
	CodeOrderNotFound       = 50000
	CodeInvalidTransition   = 50001
	CodeIdempotencyConflict = 50002
	CodeInternal            = 90000
)

type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// Problem is the transport-safe failure category rendered in the uniform HTTP envelope.
type Problem struct {
	Code    int
	Message string
	Data    any
}

// FieldError points callers at the exact request field that failed validation.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type FieldErrors []FieldError

func writeSuccess(c *gin.Context, status int, data any) {
	body := envelope{Code: CodeOK}
	if data != nil {
		body.Data = data
	}
	c.JSON(status, body)
}

func writeFailure(c *gin.Context, status int, problem Problem) {
	body := envelope{
		Code:    problem.Code,
		Message: problem.Message,
	}
	if problem.Data != nil {
		body.Data = problem.Data
	}
	c.AbortWithStatusJSON(status, body)
}

func writeInternal(c *gin.Context) {
	writeFailure(c, http.StatusInternalServerError, Problem{
		Code:    CodeInternal,
		Message: "internal server error",
	})
}

package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func decodeJSON(c *gin.Context, dst any, maxBodyBytes int64) *Problem {
	if maxBodyBytes > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	}

	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return decodeProblem(err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return &Problem{
			Code:    CodeMalformedJSON,
			Message: "request body must contain exactly one JSON value",
		}
	}

	return nil
}

func decodeProblem(err error) *Problem {
	var (
		syntaxErr     *json.SyntaxError
		typeErr       *json.UnmarshalTypeError
		maxBytesError *http.MaxBytesError
	)

	switch {
	case errors.Is(err, io.EOF):
		return &Problem{Code: CodeMalformedJSON, Message: "request body must not be empty"}
	case errors.As(err, &syntaxErr):
		return &Problem{Code: CodeMalformedJSON, Message: "request body must be valid JSON"}
	case errors.As(err, &maxBytesError):
		return &Problem{Code: CodeMalformedJSON, Message: fmt.Sprintf("request body must not exceed %d bytes", maxBytesError.Limit)}
	case errors.As(err, &typeErr):
		field := typeErr.Field
		if field == "" {
			field = "body"
		}
		return &Problem{
			Code:    CodeValidation,
			Message: "request validation failed",
			Data: FieldErrors{
				{Field: field, Reason: "invalid_type"},
			},
		}
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.TrimPrefix(err.Error(), "json: unknown field ")
		field = strings.Trim(field, `"`)
		return &Problem{
			Code:    CodeValidation,
			Message: "request validation failed",
			Data: FieldErrors{
				{Field: field, Reason: "unknown_field"},
			},
		}
	default:
		return &Problem{Code: CodeMalformedJSON, Message: "request body must be valid JSON"}
	}
}

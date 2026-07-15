package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type decodePayload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestDecodeJSONSuccess(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"alice","age":30}`))

	var got decodePayload
	if problem := decodeJSON(c, &got, 1024); problem != nil {
		t.Fatalf("decodeJSON() problem = %+v, want nil", *problem)
	}
	if got.Name != "alice" || got.Age != 30 {
		t.Fatalf("decoded payload = %+v", got)
	}
}

func TestDecodeJSONRejectsUnknownField(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"alice","unknown":true}`))

	var got decodePayload
	problem := decodeJSON(c, &got, 1024)
	if problem == nil {
		t.Fatal("decodeJSON() problem = nil, want error")
	}
	if problem.Code != CodeValidation {
		t.Fatalf("problem code = %d, want %d", problem.Code, CodeValidation)
	}
	fields, ok := problem.Data.(FieldErrors)
	if !ok || len(fields) != 1 || fields[0].Field != "unknown" || fields[0].Reason != "unknown_field" {
		t.Fatalf("problem data = %#v", problem.Data)
	}
}

func TestDecodeJSONRejectsTypeMismatch(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"alice","age":"old"}`))

	var got decodePayload
	problem := decodeJSON(c, &got, 1024)
	if problem == nil {
		t.Fatal("decodeJSON() problem = nil, want error")
	}
	if problem.Code != CodeValidation {
		t.Fatalf("problem code = %d, want %d", problem.Code, CodeValidation)
	}
	fields, ok := problem.Data.(FieldErrors)
	if !ok || len(fields) != 1 || fields[0].Field != "age" || fields[0].Reason != "invalid_type" {
		t.Fatalf("problem data = %#v", problem.Data)
	}
}

func TestDecodeJSONRejectsMultipleValues(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"alice"}{"age":30}`))

	var got decodePayload
	problem := decodeJSON(c, &got, 1024)
	if problem == nil {
		t.Fatal("decodeJSON() problem = nil, want error")
	}
	if problem.Code != CodeMalformedJSON {
		t.Fatalf("problem code = %d, want %d", problem.Code, CodeMalformedJSON)
	}
}

func TestDecodeJSONRejectsTooLargeBody(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"alice","age":30}`))

	var got decodePayload
	problem := decodeJSON(c, &got, 8)
	if problem == nil {
		t.Fatal("decodeJSON() problem = nil, want error")
	}
	if problem.Code != CodeMalformedJSON {
		t.Fatalf("problem code = %d, want %d", problem.Code, CodeMalformedJSON)
	}
}

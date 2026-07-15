package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWriteSuccessContract(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	writeSuccess(c, http.StatusOK, map[string]any{"name": "alice"})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if got["code"] != float64(CodeOK) {
		t.Fatalf("code = %#v, want %d", got["code"], CodeOK)
	}
	if _, ok := got["message"]; ok {
		t.Fatalf("success body must not include message: %#v", got)
	}
	if _, ok := got["data"]; !ok {
		t.Fatalf("success body must include data when provided: %#v", got)
	}
	for _, forbidden := range []string{"success", "meta", "details", "request_id"} {
		if _, ok := got[forbidden]; ok {
			t.Fatalf("unexpected key %q in %#v", forbidden, got)
		}
	}
}

func TestWriteSuccessOmitsNilData(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	writeSuccess(c, http.StatusOK, nil)

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := got["data"]; ok {
		t.Fatalf("success body must omit nil data: %#v", got)
	}
}

func TestWriteFailureContract(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	writeFailure(c, http.StatusBadRequest, Problem{
		Code:    CodeValidation,
		Message: "request validation failed",
		Data: FieldErrors{
			{Field: "email", Reason: "invalid_email"},
		},
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["code"] != float64(CodeValidation) || got["message"] == "" {
		t.Fatalf("body = %#v", got)
	}
	for _, forbidden := range []string{"success", "meta", "details", "request_id"} {
		if _, ok := got[forbidden]; ok {
			t.Errorf("unexpected key %q", forbidden)
		}
	}
}

func TestWriteSuccessPaginationNestedUnderData(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	writeSuccess(c, http.StatusOK, NewPageData([]string{"a"}, Pagination{
		Limit:  1,
		Offset: 0,
	}))

	var got struct {
		Code int `json:"code"`
		Data struct {
			Items      []string   `json:"items"`
			Pagination Pagination `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got.Code != CodeOK {
		t.Fatalf("code = %d, want %d", got.Code, CodeOK)
	}
	if got.Data.Pagination.Limit != 1 {
		t.Fatalf("pagination = %+v, want nested under data", got.Data.Pagination)
	}
}

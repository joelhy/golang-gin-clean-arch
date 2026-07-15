package web

import (
	"net/url"
	"testing"
	"time"
)

func TestDecodePaginationQuerySuccess(t *testing.T) {
	params, problem := DecodePaginationQuery(url.Values{
		"limit":     {"10"},
		"offset":    {"5"},
		"sort":      {"created_at"},
		"direction": {"desc"},
		"start_at":  {"2026-07-15T00:00:00Z"},
		"end_at":    {"2026-07-16T00:00:00Z"},
	}, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"created_at", "email"},
		StartKey:     "start_at",
		EndKey:       "end_at",
	})
	if problem != nil {
		t.Fatalf("DecodePaginationQuery() problem = %+v, want nil", *problem)
	}
	if params.Pagination.Limit != 10 || params.Pagination.Offset != 5 {
		t.Fatalf("pagination = %+v", params.Pagination)
	}
	if params.Sort != "created_at" || params.Direction != "desc" {
		t.Fatalf("params = %+v", params)
	}
	if params.StartAt == nil || params.EndAt == nil {
		t.Fatalf("range = %+v", params)
	}
	if !params.StartAt.Equal(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("start = %v", params.StartAt)
	}
}

func TestDecodePaginationRejectsRepeatedSingletonParameter(t *testing.T) {
	_, problem := DecodePaginationQuery(url.Values{"limit": {"10", "11"}}, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"created_at"},
	})
	if problem == nil {
		t.Fatal("DecodePaginationQuery() problem = nil, want error")
	}
	assertProblemFieldReason(t, problem, "limit", "repeated_parameter")
}

func TestDecodePaginationRejectsInvalidInteger(t *testing.T) {
	_, problem := DecodePaginationQuery(url.Values{"limit": {"ten"}}, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"created_at"},
	})
	if problem == nil {
		t.Fatal("DecodePaginationQuery() problem = nil, want error")
	}
	assertProblemFieldReason(t, problem, "limit", "invalid_integer")
}

func TestDecodePaginationRejectsLimitOutsideRange(t *testing.T) {
	_, problem := DecodePaginationQuery(url.Values{"limit": {"101"}}, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"created_at"},
	})
	if problem == nil {
		t.Fatal("DecodePaginationQuery() problem = nil, want error")
	}
	assertProblemFieldReason(t, problem, "limit", "out_of_range")
}

func TestDecodePaginationRejectsNegativeOffset(t *testing.T) {
	_, problem := DecodePaginationQuery(url.Values{"offset": {"-1"}}, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"created_at"},
	})
	if problem == nil {
		t.Fatal("DecodePaginationQuery() problem = nil, want error")
	}
	assertProblemFieldReason(t, problem, "offset", "negative_offset")
}

func TestDecodePaginationRejectsUnknownSort(t *testing.T) {
	_, problem := DecodePaginationQuery(url.Values{"sort": {"status"}}, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"created_at"},
	})
	if problem == nil {
		t.Fatal("DecodePaginationQuery() problem = nil, want error")
	}
	assertProblemFieldReason(t, problem, "sort", "unknown_sort")
}

func TestDecodePaginationRejectsUnknownDirection(t *testing.T) {
	_, problem := DecodePaginationQuery(url.Values{"direction": {"sideways"}}, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"created_at"},
	})
	if problem == nil {
		t.Fatal("DecodePaginationQuery() problem = nil, want error")
	}
	assertProblemFieldReason(t, problem, "direction", "unknown_direction")
}

func TestDecodePaginationRejectsMalformedUTCTimestamp(t *testing.T) {
	_, problem := DecodePaginationQuery(url.Values{"start_at": {"2026-07-15T00:00:00+08:00"}}, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"created_at"},
		StartKey:     "start_at",
	})
	if problem == nil {
		t.Fatal("DecodePaginationQuery() problem = nil, want error")
	}
	assertProblemFieldReason(t, problem, "start_at", "invalid_timestamp")
}

func TestDecodePaginationRejectsInvertedRange(t *testing.T) {
	_, problem := DecodePaginationQuery(url.Values{
		"start_at": {"2026-07-16T00:00:00Z"},
		"end_at":   {"2026-07-15T00:00:00Z"},
	}, QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"created_at"},
		StartKey:     "start_at",
		EndKey:       "end_at",
	})
	if problem == nil {
		t.Fatal("DecodePaginationQuery() problem = nil, want error")
	}
	assertProblemFieldReason(t, problem, "start_at", "inverted_range")
}

func assertProblemFieldReason(t *testing.T, problem *Problem, wantField string, wantReason string) {
	t.Helper()
	if problem.Code != CodeValidation {
		t.Fatalf("problem code = %d, want %d", problem.Code, CodeValidation)
	}
	fields, ok := problem.Data.(FieldErrors)
	if !ok || len(fields) != 1 {
		t.Fatalf("problem data = %#v", problem.Data)
	}
	if fields[0].Field != wantField || fields[0].Reason != wantReason {
		t.Fatalf("field error = %+v, want field=%q reason=%q", fields[0], wantField, wantReason)
	}
}

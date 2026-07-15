package web

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Pagination struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type PageData struct {
	Items      any        `json:"items,omitempty"`
	Pagination Pagination `json:"pagination"`
}

type QueryOptions struct {
	DefaultLimit int
	AllowedSorts []string
	StartKey     string
	EndKey       string
}

type QueryParams struct {
	Pagination Pagination
	Sort       string
	Direction  string
	StartAt    *time.Time
	EndAt      *time.Time
}

func NewPageData(items any, pagination Pagination) PageData {
	return PageData{Items: items, Pagination: pagination}
}

// DecodePaginationQuery rejects repeated singleton parameters so clients cannot
// smuggle ambiguous values through different proxy or framework coercion rules.
func DecodePaginationQuery(values url.Values, opts QueryOptions) (QueryParams, *Problem) {
	params := QueryParams{
		Pagination: Pagination{
			Limit: opts.DefaultLimit,
		},
		Direction: "asc",
	}
	if params.Pagination.Limit == 0 {
		params.Pagination.Limit = 20
	}

	limit, ok, problem := singletonInt(values, "limit")
	if problem != nil {
		return QueryParams{}, problem
	}
	if ok {
		if limit < 1 || limit > 100 {
			problem := validationProblem("limit", "out_of_range")
			return QueryParams{}, &problem
		}
		params.Pagination.Limit = limit
	}

	offset, ok, problem := singletonInt(values, "offset")
	if problem != nil {
		return QueryParams{}, problem
	}
	if ok {
		if offset < 0 {
			problem := validationProblem("offset", "negative_offset")
			return QueryParams{}, &problem
		}
		params.Pagination.Offset = offset
	}

	sortField, ok, problem := singletonString(values, "sort")
	if problem != nil {
		return QueryParams{}, problem
	}
	if ok {
		if !contains(opts.AllowedSorts, sortField) {
			problem := validationProblem("sort", "unknown_sort")
			return QueryParams{}, &problem
		}
		params.Sort = sortField
	}

	direction, ok, problem := singletonString(values, "direction")
	if problem != nil {
		return QueryParams{}, problem
	}
	if ok {
		if direction != "asc" && direction != "desc" {
			problem := validationProblem("direction", "unknown_direction")
			return QueryParams{}, &problem
		}
		params.Direction = direction
	}

	if opts.StartKey != "" {
		start, ok, problem := singletonUTCTime(values, opts.StartKey)
		if problem != nil {
			return QueryParams{}, problem
		}
		if ok {
			params.StartAt = &start
		}
	}
	if opts.EndKey != "" {
		end, ok, problem := singletonUTCTime(values, opts.EndKey)
		if problem != nil {
			return QueryParams{}, problem
		}
		if ok {
			params.EndAt = &end
		}
	}
	if params.StartAt != nil && params.EndAt != nil && params.StartAt.After(*params.EndAt) {
		problem := validationProblem(opts.StartKey, "inverted_range")
		return QueryParams{}, &problem
	}

	return params, nil
}

func singletonString(values url.Values, key string) (string, bool, *Problem) {
	items := values[key]
	switch len(items) {
	case 0:
		return "", false, nil
	case 1:
		return items[0], true, nil
	default:
		problem := validationProblem(key, "repeated_parameter")
		return "", false, &problem
	}
}

func singletonInt(values url.Values, key string) (int, bool, *Problem) {
	raw, ok, problem := singletonString(values, key)
	if !ok || problem != nil {
		return 0, ok, problem
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		problem := validationProblem(key, "invalid_integer")
		return 0, false, &problem
	}
	return value, true, nil
}

func singletonUTCTime(values url.Values, key string) (time.Time, bool, *Problem) {
	raw, ok, problem := singletonString(values, key)
	if !ok || problem != nil {
		return time.Time{}, ok, problem
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil || !strings.HasSuffix(raw, "Z") {
		problem := validationProblem(key, "invalid_timestamp")
		return time.Time{}, false, &problem
	}
	return parsed.UTC(), true, nil
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

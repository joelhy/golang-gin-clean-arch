package user

import (
	"context"
	"fmt"
	"time"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

const (
	RoleAdmin    = "admin"
	RoleCustomer = "customer"
)

const (
	PermissionUsersRead     = "users:read"
	PermissionUsersWrite    = "users:write"
	PermissionUsersRoles    = "users:roles"
	PermissionProductsWrite = "products:write"
	PermissionProductsStock = "products:stock"
	PermissionOrdersReadAll = "orders:read_all"
	PermissionOrdersManage  = "orders:manage"
	PermissionStatsRead     = "stats:read"
)

type User struct {
	ID           uint64
	Email        string
	Name         string
	PasswordHash string
	Status       Status
	Version      uint64
	Roles        []Role
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Role struct {
	ID          uint64
	Name        string
	Permissions []Permission
}

type Permission struct {
	ID   uint64
	Name string
}

type Identity struct {
	UserID    uint64
	SessionID string
	TokenID   string
}

type Actor struct {
	UserID      uint64
	Permissions []string
}

type Page struct {
	Items  []User
	Total  uint64
	Limit  int
	Offset int
}

type ListFilter struct {
	Email      string
	Name       string
	Status     Status
	Role       string
	Sort       string
	Descending bool
	Limit      int
	Offset     int
}

func (f ListFilter) Validate() error {
	if f.Status != "" && !validStatus(f.Status) {
		return &ValidationError{Field: "status", Message: fmt.Sprintf("%q is not supported", f.Status), Err: ErrInvalidStatus}
	}
	// SQL identifiers cannot be bound as query parameters, so the domain exposes
	// only fields that the adapter can map to fixed ORDER BY expressions.
	switch f.Sort {
	case "id", "email", "name", "status", "created_at", "updated_at":
	default:
		return &ValidationError{Field: "sort", Message: fmt.Sprintf("%q is not supported", f.Sort), Err: ErrInvalidFilter}
	}
	if f.Limit < 1 || f.Limit > 100 {
		return &ValidationError{Field: "limit", Message: "must be between 1 and 100", Err: ErrInvalidFilter}
	}
	if f.Offset < 0 {
		return &ValidationError{Field: "offset", Message: "must not be negative", Err: ErrInvalidFilter}
	}
	return nil
}

type RegisterInput struct {
	Email    string
	Name     string
	Password string
}

type UpdateMeInput struct {
	Name            string
	ExpectedVersion uint64
}

type SetStatusInput struct {
	UserID          uint64
	Status          Status
	ExpectedVersion uint64
}

type ReplaceRolesInput struct {
	UserID          uint64
	Roles           []string
	ExpectedVersion uint64
}

// Store is the persistence contract consumed by the account domain.
type Store interface {
	// CreateWithRole must persist both the account and its initial role atomically;
	// otherwise bootstrap failure could leave a credentialed user without authority.
	// Implementations also map the email unique constraint to ErrEmailExists so the
	// service never needs a race-prone duplicate preflight.
	CreateWithRole(context.Context, *User, string) error
	ByID(context.Context, uint64) (*User, error)
	ByEmail(context.Context, string) (*User, error)
	UpdateProfile(context.Context, *User, uint64) error
	List(context.Context, ListFilter) (Page, error)
	// SetStatus and ReplaceRoles must enforce the last-active-admin invariant again
	// in the same transaction as the write. The service preflight improves error
	// reporting but cannot prevent two concurrent requests from both passing it.
	SetStatus(context.Context, uint64, Status, uint64) error
	ReplaceRoles(context.Context, uint64, []string, uint64) error
	RoleNamesExist(context.Context, []string) (bool, error)
	IsLastActiveAdmin(context.Context, uint64) (bool, error)
	Permissions(context.Context, uint64) ([]string, error)
}

type Passwords interface {
	Hash(string) (string, error)
	Verify(encoded, password string) (bool, error)
	NeedsRehash(string) bool
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

func validStatus(status Status) bool {
	return status == StatusActive || status == StatusDisabled
}

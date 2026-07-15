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

// Session represents one refresh-token family. Rotation is monotonic and lets
// persistence reject stale concurrent refreshes without trusting bearer input.
type Session struct {
	ID              string
	UserID          uint64
	Rotation        uint64
	ExpiresAt       time.Time
	RevokedAt       *time.Time
	ReuseDetectedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// RefreshToken carries only a one-way digest at the persistence boundary. Raw
// bearer material exists only in Tokens returned to the caller.
type RefreshToken struct {
	ID           string
	SessionID    string
	Digest       string
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
	RevokedAt    *time.Time
	ReplacedByID string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Tokens struct {
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

type LoginInput struct {
	Email    string
	Password string
}

type RotateSessionInput struct {
	Digest   string
	Now      time.Time
	NewToken *RefreshToken
}

type RotateSessionResult struct {
	Session      *Session
	RefreshToken *RefreshToken
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
	UpdateProfile(context.Context, *User, uint64) (*User, error)
	List(context.Context, ListFilter) (Page, error)
	// SetStatus must acquire the same shared serialization guard as ReplaceRoles
	// before recounting active admins and writing. Locking the stable admin-role row,
	// an advisory lock, or a serializable equivalent prevents two transactions that
	// each saw two admins from concurrently disabling different accounts.
	SetStatus(context.Context, uint64, Status, uint64) error
	// ReplaceRoles uses the same guard and lock order as SetStatus, then recounts and
	// writes in that transaction. A plain transaction without this shared guard still
	// permits last-admin write-skew.
	ReplaceRoles(context.Context, uint64, []string, uint64) error
	// RoleNamesExist reports true only when every normalized role name exists.
	RoleNamesExist(context.Context, []string) (bool, error)
	// IsLastActiveAdmin is a service preflight for quick business feedback only; its
	// result cannot provide concurrency safety after this call returns. Task 5 must
	// integration-test concurrent two-admin disable and admin-role-removal attempts,
	// proving the shared write guard lets at most one revocation commit.
	IsLastActiveAdmin(context.Context, uint64) (bool, error)
	Permissions(context.Context, uint64) ([]string, error)
}

type Passwords interface {
	Hash(string) (string, error)
	Verify(encoded, password string) (bool, error)
	NeedsRehash(string) bool
}

// AuthUserStore is intentionally narrower than Store because authentication
// must not gain account-administration writes through its persistence port.
type AuthUserStore interface {
	ByEmail(context.Context, string) (*User, error)
	ByID(context.Context, uint64) (*User, error)
	UpdatePasswordHash(context.Context, uint64, string, uint64) (*User, error)
	Permissions(context.Context, uint64) ([]string, error)
}

type SessionStore interface {
	Create(context.Context, *Session, *RefreshToken) error
	Active(context.Context, string, uint64, time.Time) (*Session, error)
	Rotate(context.Context, RotateSessionInput) (RotateSessionResult, error)
	Revoke(context.Context, string, uint64, time.Time) error
}

type AccessTokens interface {
	Issue(Identity, time.Time) (string, time.Time, error)
	Parse(string, time.Time) (Identity, error)
}

type RefreshTokens interface {
	Generate() (raw, digest string, err error)
	Digest(string) string
}

type IDGenerator interface {
	NewID() (string, error)
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

func validStatus(status Status) bool {
	return status == StatusActive || status == StatusDisabled
}

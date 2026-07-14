package user

import "errors"

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrInvalidName        = errors.New("invalid name")
	ErrWeakPassword       = errors.New("weak password")
	ErrEmailExists        = errors.New("email already exists")
	ErrNotFound           = errors.New("not found")
	ErrDisabled           = errors.New("account disabled")
	ErrForbidden          = errors.New("forbidden")
	ErrRoleNotFound       = errors.New("role not found")
	ErrLastAdmin          = errors.New("cannot modify the last active admin")
	ErrConflict           = errors.New("conflict")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidRefresh     = errors.New("invalid refresh token")
	ErrRefreshReuse       = errors.New("refresh token reuse detected")
	ErrSessionExpired     = errors.New("session expired")
	ErrSessionRevoked     = errors.New("session revoked")

	ErrInvalidStatus = errors.New("invalid user status")
	ErrInvalidFilter = errors.New("invalid user list filter")
)

// ValidationError identifies the invalid input field while preserving a stable
// sentinel for callers that need to translate domain failures to transport errors.
type ValidationError struct {
	Field   string
	Message string
	Err     error
}

func (e *ValidationError) Error() string {
	if e.Message == "" {
		return e.Field + " is invalid"
	}
	return e.Field + " " + e.Message
}

func (e *ValidationError) Unwrap() error { return e.Err }

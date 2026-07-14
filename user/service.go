package user

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	maxEmailBytes    = 320
	maxNameRunes     = 100
	minPasswordBytes = 12
	maxPasswordBytes = 128
)

type Service struct {
	store     Store
	passwords Passwords
	clock     Clock
}

func NewService(store Store, passwords Passwords, clock Clock) (*Service, error) {
	if store == nil {
		return nil, errors.New("new user service: store is required")
	}
	if passwords == nil {
		return nil, errors.New("new user service: passwords is required")
	}
	if clock == nil {
		return nil, errors.New("new user service: clock is required")
	}
	return &Service{store: store, passwords: passwords, clock: clock}, nil
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (*User, error) {
	return s.createWithRole(ctx, input, RoleCustomer)
}

func (s *Service) BootstrapAdmin(ctx context.Context, input RegisterInput) (*User, error) {
	return s.createWithRole(ctx, input, RoleAdmin)
}

func (s *Service) createWithRole(ctx context.Context, input RegisterInput, role string) (*User, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return nil, fmt.Errorf("create %s account: %w", role, err)
	}
	name, err := normalizeName(input.Name)
	if err != nil {
		return nil, fmt.Errorf("create %s account: %w", role, err)
	}
	if err := validatePassword(input.Password); err != nil {
		return nil, fmt.Errorf("create %s account: %w", role, err)
	}

	passwordHash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return nil, fmt.Errorf("create %s account: hash password: %w", role, err)
	}
	now := s.clock.Now()
	user := &User{
		Email:        email,
		Name:         name,
		PasswordHash: passwordHash,
		Status:       StatusActive,
		Version:      1,
		Roles:        []Role{{Name: role}},
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// A read-before-write duplicate check is intentionally avoided: only the
	// database unique constraint can decide correctly under concurrent registration.
	if err := s.store.CreateWithRole(ctx, user, role); err != nil {
		return nil, fmt.Errorf("create %s account: persist: %w", role, err)
	}
	return cloneUser(user), nil
}

func (s *Service) Me(ctx context.Context, userID uint64) (*User, error) {
	user, err := s.store.ByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get current user %d: %w", userID, err)
	}
	if user == nil {
		return nil, fmt.Errorf("get current user %d: %w", userID, ErrNotFound)
	}
	// Unknown persisted states fail closed so corrupt data cannot reactivate access.
	if user.Status != StatusActive {
		return nil, fmt.Errorf("get current user %d: %w", userID, ErrDisabled)
	}
	return cloneUser(user), nil
}

func (s *Service) UpdateMe(ctx context.Context, userID uint64, input UpdateMeInput) (*User, error) {
	name, err := normalizeName(input.Name)
	if err != nil {
		return nil, fmt.Errorf("update current user %d: %w", userID, err)
	}
	user, err := s.Me(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("update current user %d: %w", userID, err)
	}
	user.Name = name
	persisted, err := s.store.UpdateProfile(ctx, user, input.ExpectedVersion)
	if err != nil {
		return nil, fmt.Errorf("update current user %d: %w", userID, err)
	}
	return cloneUser(persisted), nil
}

func normalizeEmail(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", &ValidationError{Field: "email", Message: "must be valid UTF-8", Err: ErrInvalidEmail}
	}
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || len(normalized) > maxEmailBytes {
		return "", &ValidationError{Field: "email", Message: "must be between 1 and 320 bytes", Err: ErrInvalidEmail}
	}
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Name != "" || address.Address != normalized {
		return "", &ValidationError{Field: "email", Message: "must be a single mailbox without a display name", Err: ErrInvalidEmail}
	}
	return normalized, nil
}

func normalizeName(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", &ValidationError{Field: "name", Message: "is required", Err: ErrInvalidName}
	}
	if !utf8.ValidString(normalized) || utf8.RuneCountInString(normalized) > maxNameRunes {
		return "", &ValidationError{Field: "name", Message: "must be valid UTF-8 with at most 100 characters", Err: ErrInvalidName}
	}
	return normalized, nil
}

func validatePassword(password string) error {
	// Password bytes are policy input and cryptographic material; trimming or
	// normalizing here would silently change the credential chosen by the user.
	if !utf8.ValidString(password) || len(password) < minPasswordBytes || len(password) > maxPasswordBytes {
		return &ValidationError{Field: "password", Message: "must be valid UTF-8 between 12 and 128 bytes", Err: ErrWeakPassword}
	}
	return nil
}

func cloneUser(user *User) *User {
	if user == nil {
		return nil
	}
	cloned := *user
	cloned.Roles = slices.Clone(user.Roles)
	for i := range cloned.Roles {
		cloned.Roles[i].Permissions = slices.Clone(user.Roles[i].Permissions)
	}
	return &cloned
}

func clonePage(page Page) Page {
	cloned := page
	cloned.Items = slices.Clone(page.Items)
	for i := range cloned.Items {
		cloned.Items[i] = *cloneUser(&page.Items[i])
	}
	return cloned
}

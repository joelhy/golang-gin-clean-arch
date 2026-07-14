package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var fixedTime = time.Date(2026, 7, 14, 9, 30, 0, 123, time.FixedZone("test", 8*60*60))

type fakeClock struct {
	now time.Time
}

func (c fakeClock) Now() time.Time { return c.now }

type fakePasswords struct {
	hashInput  string
	hashOutput string
	hashErr    error
}

func (p *fakePasswords) Hash(password string) (string, error) {
	p.hashInput = password
	if p.hashErr != nil {
		return "", p.hashErr
	}
	if p.hashOutput == "" {
		return "encoded-password", nil
	}
	return p.hashOutput, nil
}

func (*fakePasswords) Verify(string, string) (bool, error) { return true, nil }
func (*fakePasswords) NeedsRehash(string) bool             { return false }

type fakeStore struct {
	createUser        *User
	createRole        string
	createErr         error
	byIDUser          *User
	byIDErr           error
	byIDCalls         int
	byEmailCalls      int
	updateUser        *User
	updateExpected    uint64
	updateErr         error
	updateCalls       int
	listFilter        ListFilter
	listPage          Page
	listErr           error
	listCalls         int
	setStatusUserID   uint64
	setStatus         Status
	setStatusExpected uint64
	setStatusErr      error
	setStatusCalls    int
	replaceUserID     uint64
	replaceRoles      []string
	replaceExpected   uint64
	replaceErr        error
	replaceCalls      int
	rolesExist        bool
	rolesExistErr     error
	rolesExistInput   []string
	rolesExistCalls   int
	lastActiveAdmin   bool
	lastAdminErr      error
	lastAdminUserID   uint64
	lastAdminCalls    int
	permissions       []string
	permissionsErr    error
	permissionsID     uint64
	permissionsCalls  int
}

func newFakeStore() *fakeStore {
	return &fakeStore{rolesExist: true}
}

func (s *fakeStore) CreateWithRole(_ context.Context, user *User, role string) error {
	s.createRole = role
	if s.createErr == nil {
		user.ID = 42
	}
	s.createUser = cloneUser(user)
	return s.createErr
}

func (s *fakeStore) ByID(context.Context, uint64) (*User, error) {
	s.byIDCalls++
	if s.byIDUser == nil {
		return nil, s.byIDErr
	}
	return cloneUser(s.byIDUser), s.byIDErr
}

func (s *fakeStore) ByEmail(context.Context, string) (*User, error) {
	s.byEmailCalls++
	return nil, s.byIDErr
}

func (s *fakeStore) UpdateProfile(_ context.Context, user *User, expectedVersion uint64) error {
	s.updateCalls++
	s.updateUser = cloneUser(user)
	s.updateExpected = expectedVersion
	if s.updateErr == nil {
		user.Version = expectedVersion + 1
	}
	return s.updateErr
}

func (s *fakeStore) List(_ context.Context, filter ListFilter) (Page, error) {
	s.listCalls++
	s.listFilter = filter
	return s.listPage, s.listErr
}

func (s *fakeStore) SetStatus(_ context.Context, userID uint64, status Status, expectedVersion uint64) error {
	s.setStatusCalls++
	s.setStatusUserID = userID
	s.setStatus = status
	s.setStatusExpected = expectedVersion
	return s.setStatusErr
}

func (s *fakeStore) ReplaceRoles(_ context.Context, userID uint64, roles []string, expectedVersion uint64) error {
	s.replaceCalls++
	s.replaceUserID = userID
	s.replaceRoles = append([]string(nil), roles...)
	s.replaceExpected = expectedVersion
	return s.replaceErr
}

func (s *fakeStore) RoleNamesExist(_ context.Context, roles []string) (bool, error) {
	s.rolesExistCalls++
	s.rolesExistInput = append([]string(nil), roles...)
	return s.rolesExist, s.rolesExistErr
}

func (s *fakeStore) IsLastActiveAdmin(_ context.Context, userID uint64) (bool, error) {
	s.lastAdminCalls++
	s.lastAdminUserID = userID
	return s.lastActiveAdmin, s.lastAdminErr
}

func (s *fakeStore) Permissions(_ context.Context, userID uint64) ([]string, error) {
	s.permissionsCalls++
	s.permissionsID = userID
	return append([]string(nil), s.permissions...), s.permissionsErr
}

func cloneUser(user *User) *User {
	if user == nil {
		return nil
	}
	cloned := *user
	cloned.Roles = append([]Role(nil), user.Roles...)
	return &cloned
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trim and lowercase", input: "  USER@Example.COM ", want: "user@example.com"},
		{name: "empty", input: " \t ", wantErr: ErrInvalidEmail},
		{name: "malformed", input: "not-an-email", wantErr: ErrInvalidEmail},
		{name: "display name", input: "Joel <joel@example.com>", wantErr: ErrInvalidEmail},
		{name: "multiple addresses", input: "one@example.com,two@example.com", wantErr: ErrInvalidEmail},
		{name: "overlong", input: strings.Repeat("a", 310) + "@example.com", wantErr: ErrInvalidEmail},
		{name: "invalid UTF-8", input: string([]byte{0xff}) + "@example.com", wantErr: ErrInvalidEmail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeEmail(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeEmail() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("normalizeEmail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trim", input: "  Joel  ", want: "Joel"},
		{name: "required", input: " \t ", wantErr: ErrInvalidName},
		{name: "one hundred runes", input: strings.Repeat("界", 100), want: strings.Repeat("界", 100)},
		{name: "overlong", input: strings.Repeat("界", 101), wantErr: ErrInvalidName},
		{name: "invalid UTF-8", input: string([]byte{0xff}), wantErr: ErrInvalidName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeName(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeName() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("normalizeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "minimum bytes", input: strings.Repeat("a", 12)},
		{name: "maximum bytes", input: strings.Repeat("a", 128)},
		{name: "UTF-8 byte length", input: strings.Repeat("密", 4)},
		{name: "password is not trimmed", input: strings.Repeat(" ", 12)},
		{name: "too short", input: strings.Repeat("a", 11), wantErr: ErrWeakPassword},
		{name: "too long", input: strings.Repeat("a", 129), wantErr: ErrWeakPassword},
		{name: "invalid UTF-8", input: string([]byte{0xff, 0xfe}) + strings.Repeat("a", 12), wantErr: ErrWeakPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validatePassword() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
		})
	}
}

func TestValidationErrorPreservesSentinel(t *testing.T) {
	err := &ValidationError{Field: "email", Message: "is malformed", Err: ErrInvalidEmail}
	if !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("errors.Is(%v, ErrInvalidEmail) = false", err)
	}
	if got, want := err.Error(), "email is malformed"; got != want {
		t.Fatalf("ValidationError.Error() = %q, want %q", got, want)
	}
}

func TestSystemClockNowUsesWallClock(t *testing.T) {
	before := time.Now()
	got := (SystemClock{}).Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("SystemClock.Now() = %v, want between %v and %v", got, before, after)
	}
}

func TestServiceRegister(t *testing.T) {
	store := newFakeStore()
	passwords := &fakePasswords{hashOutput: "encoded-secret"}
	service := NewService(store, passwords, fakeClock{now: fixedTime})
	password := "  correct horse battery staple  "

	got, err := service.Register(t.Context(), RegisterInput{
		Email:    "  USER@Example.COM ",
		Name:     "  Joel  ",
		Password: password,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	want := &User{
		ID:           42,
		Email:        "user@example.com",
		Name:         "Joel",
		PasswordHash: "encoded-secret",
		Status:       StatusActive,
		Version:      1,
		Roles:        []Role{{Name: RoleCustomer}},
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Register() user mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, store.createUser); diff != "" {
		t.Fatalf("persisted user mismatch (-want +got):\n%s", diff)
	}
	if store.createRole != RoleCustomer {
		t.Fatalf("CreateWithRole() role = %q, want %q", store.createRole, RoleCustomer)
	}
	if passwords.hashInput != password {
		t.Fatalf("Hash() input = %q, want original password %q", passwords.hashInput, password)
	}
	if store.createUser.PasswordHash == password {
		t.Fatal("persisted user retained the plaintext password")
	}
	if store.byEmailCalls != 0 {
		t.Fatalf("ByEmail() calls = %d, want 0; registration must rely on the unique write", store.byEmailCalls)
	}
}

func TestServiceRegisterRejectsInvalidInputBeforeHashing(t *testing.T) {
	tests := []struct {
		name    string
		input   RegisterInput
		wantErr error
	}{
		{name: "email", input: RegisterInput{Email: "bad", Name: "Joel", Password: strings.Repeat("a", 12)}, wantErr: ErrInvalidEmail},
		{name: "name", input: RegisterInput{Email: "joel@example.com", Name: " ", Password: strings.Repeat("a", 12)}, wantErr: ErrInvalidName},
		{name: "password", input: RegisterInput{Email: "joel@example.com", Name: "Joel", Password: "short"}, wantErr: ErrWeakPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			passwords := &fakePasswords{}
			service := NewService(store, passwords, fakeClock{now: fixedTime})

			_, err := service.Register(t.Context(), tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Register() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if passwords.hashInput != "" {
				t.Fatalf("Hash() input = %q, want no call", passwords.hashInput)
			}
			if store.createUser != nil {
				t.Fatal("CreateWithRole() called for invalid input")
			}
		})
	}
}

func TestServiceRegisterPropagatesHashAndDuplicateErrors(t *testing.T) {
	t.Run("hash", func(t *testing.T) {
		store := newFakeStore()
		hashErr := errors.New("hash unavailable")
		service := NewService(store, &fakePasswords{hashErr: hashErr}, fakeClock{now: fixedTime})

		_, err := service.Register(t.Context(), validRegisterInput())
		if !errors.Is(err, hashErr) {
			t.Fatalf("Register() error = %v, want hash error", err)
		}
		if store.createUser != nil {
			t.Fatal("CreateWithRole() called after hash failure")
		}
	})

	t.Run("duplicate email", func(t *testing.T) {
		store := newFakeStore()
		store.createErr = fmt.Errorf("unique users email: %w", ErrEmailExists)
		service := NewService(store, &fakePasswords{}, fakeClock{now: fixedTime})

		_, err := service.Register(t.Context(), validRegisterInput())
		if !errors.Is(err, ErrEmailExists) {
			t.Fatalf("Register() error = %v, want errors.Is(_, ErrEmailExists)", err)
		}
	})
}

func TestServiceBootstrapAdminUsesRegistrationPath(t *testing.T) {
	store := newFakeStore()
	passwords := &fakePasswords{hashOutput: "admin-hash"}
	service := NewService(store, passwords, fakeClock{now: fixedTime})
	input := RegisterInput{Email: " ADMIN@Example.com ", Name: " Root ", Password: strings.Repeat("x", 12)}

	got, err := service.BootstrapAdmin(t.Context(), input)
	if err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	if store.createRole != RoleAdmin {
		t.Fatalf("CreateWithRole() role = %q, want %q", store.createRole, RoleAdmin)
	}
	if got.Email != "admin@example.com" || got.Name != "Root" || got.PasswordHash != "admin-hash" {
		t.Fatalf("BootstrapAdmin() user = %+v", got)
	}
	if diff := cmp.Diff([]Role{{Name: RoleAdmin}}, got.Roles); diff != "" {
		t.Fatalf("BootstrapAdmin() roles mismatch (-want +got):\n%s", diff)
	}

	store.createErr = fmt.Errorf("insert admin: %w", ErrEmailExists)
	_, err = service.BootstrapAdmin(t.Context(), input)
	if !errors.Is(err, ErrEmailExists) {
		t.Fatalf("BootstrapAdmin() duplicate error = %v, want ErrEmailExists", err)
	}
}

func TestServiceMeRejectsDisabledAccount(t *testing.T) {
	store := newFakeStore()
	store.byIDUser = &User{ID: 7, Status: StatusDisabled}
	service := NewService(store, &fakePasswords{}, fakeClock{now: fixedTime})

	_, err := service.Me(t.Context(), 7)
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("Me() error = %v, want errors.Is(_, ErrDisabled)", err)
	}
}

func TestServiceMePropagatesNotFound(t *testing.T) {
	store := newFakeStore()
	store.byIDErr = fmt.Errorf("select user: %w", ErrNotFound)
	service := NewService(store, &fakePasswords{}, fakeClock{now: fixedTime})

	_, err := service.Me(t.Context(), 404)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Me() error = %v, want errors.Is(_, ErrNotFound)", err)
	}
}

func TestServiceUpdateMeChangesOnlyNameWithExpectedVersion(t *testing.T) {
	store := newFakeStore()
	store.byIDUser = &User{
		ID: 7, Email: "joel@example.com", Name: "Before", PasswordHash: "hash",
		Status: StatusActive, Version: 3,
	}
	service := NewService(store, &fakePasswords{}, fakeClock{now: fixedTime})

	got, err := service.UpdateMe(t.Context(), 7, UpdateMeInput{Name: "  After  ", ExpectedVersion: 3})
	if err != nil {
		t.Fatalf("UpdateMe() error = %v", err)
	}
	if store.updateExpected != 3 {
		t.Fatalf("UpdateProfile() expected version = %d, want 3", store.updateExpected)
	}
	wantUpdated := &User{
		ID: 7, Email: "joel@example.com", Name: "After", PasswordHash: "hash",
		Status: StatusActive, Version: 3,
	}
	if diff := cmp.Diff(wantUpdated, store.updateUser); diff != "" {
		t.Fatalf("UpdateProfile() user mismatch (-want +got):\n%s", diff)
	}
	if got.Version != 4 || got.Name != "After" {
		t.Fatalf("UpdateMe() user = %+v, want updated name and store-assigned version", got)
	}
}

func TestServiceUpdateMePropagatesNotFoundAndConflict(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		store := newFakeStore()
		store.byIDErr = fmt.Errorf("lookup: %w", ErrNotFound)
		service := NewService(store, &fakePasswords{}, fakeClock{now: fixedTime})

		_, err := service.UpdateMe(t.Context(), 9, UpdateMeInput{Name: "Valid Name", ExpectedVersion: 1})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("UpdateMe() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		store := newFakeStore()
		store.byIDUser = &User{ID: 7, Status: StatusActive, Version: 2}
		store.updateErr = fmt.Errorf("optimistic update: %w", ErrConflict)
		service := NewService(store, &fakePasswords{}, fakeClock{now: fixedTime})

		_, err := service.UpdateMe(t.Context(), 7, UpdateMeInput{Name: "Valid Name", ExpectedVersion: 1})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("UpdateMe() error = %v, want ErrConflict", err)
		}
	})
}

func TestServiceUpdateMeValidatesNameBeforeReading(t *testing.T) {
	store := newFakeStore()
	service := NewService(store, &fakePasswords{}, fakeClock{now: fixedTime})

	_, err := service.UpdateMe(t.Context(), 7, UpdateMeInput{Name: " ", ExpectedVersion: 1})
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("UpdateMe() error = %v, want ErrInvalidName", err)
	}
	if store.byIDCalls != 0 || store.updateCalls != 0 {
		t.Fatalf("store calls = ByID %d, UpdateProfile %d; want none", store.byIDCalls, store.updateCalls)
	}
}

func validRegisterInput() RegisterInput {
	return RegisterInput{
		Email:    "joel@example.com",
		Name:     "Joel",
		Password: "correct horse battery staple",
	}
}

package user

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const (
	testPassword   = "correct horse battery staple"
	testAccessRaw  = "signed-access-token"
	testRefreshRaw = "raw-refresh-token"
	testDigest     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

type authFakeUsers struct {
	byEmailInput  string
	byEmailUser   *User
	byEmailErr    error
	byIDInput     uint64
	byIDUser      *User
	byIDErr       error
	updateHash    string
	updateID      uint64
	updateVer     uint64
	updateUser    *User
	updateErr     error
	permissions   []string
	permissionID  uint64
	permissionErr error
}

func (s *authFakeUsers) ByEmail(_ context.Context, email string) (*User, error) {
	s.byEmailInput = email
	return cloneUser(s.byEmailUser), s.byEmailErr
}

func (s *authFakeUsers) ByID(_ context.Context, userID uint64) (*User, error) {
	s.byIDInput = userID
	return cloneUser(s.byIDUser), s.byIDErr
}

func (s *authFakeUsers) UpdatePasswordHash(_ context.Context, userID uint64, hash string, version uint64) (*User, error) {
	s.updateID, s.updateHash, s.updateVer = userID, hash, version
	return cloneUser(s.updateUser), s.updateErr
}

func (s *authFakeUsers) Permissions(_ context.Context, userID uint64) ([]string, error) {
	s.permissionID = userID
	return slices.Clone(s.permissions), s.permissionErr
}

type authFakePasswords struct {
	verifyEncoded string
	verifyRaw     string
	verifyOK      bool
	verifyErr     error
	needsRehash   bool
	hashInput     string
	hashOutput    string
	hashErr       error
}

func (p *authFakePasswords) Verify(encoded, raw string) (bool, error) {
	p.verifyEncoded, p.verifyRaw = encoded, raw
	return p.verifyOK, p.verifyErr
}

func (p *authFakePasswords) NeedsRehash(string) bool { return p.needsRehash }

func (p *authFakePasswords) Hash(raw string) (string, error) {
	p.hashInput = raw
	return p.hashOutput, p.hashErr
}

type authFakeSessions struct {
	createdSession *Session
	createdToken   *RefreshToken
	createErr      error
	activeInput    Identity
	activeNow      time.Time
	activeSession  *Session
	activeErr      error
	rotateInput    RotateSessionInput
	rotateResult   RotateSessionResult
	rotateErr      error
	revokeInput    Identity
	revokeNow      time.Time
	revokeErr      error
}

func (s *authFakeSessions) Create(_ context.Context, session *Session, token *RefreshToken) error {
	s.createdSession = cloneSession(session)
	s.createdToken = cloneRefreshToken(token)
	return s.createErr
}

func (s *authFakeSessions) Active(_ context.Context, sessionID string, userID uint64, now time.Time) (*Session, error) {
	s.activeInput = Identity{SessionID: sessionID, UserID: userID}
	s.activeNow = now
	return cloneSession(s.activeSession), s.activeErr
}

func (s *authFakeSessions) Rotate(_ context.Context, input RotateSessionInput) (RotateSessionResult, error) {
	s.rotateInput = input
	return s.rotateResult, s.rotateErr
}

func (s *authFakeSessions) Revoke(_ context.Context, sessionID string, userID uint64, now time.Time) error {
	s.revokeInput = Identity{SessionID: sessionID, UserID: userID}
	s.revokeNow = now
	return s.revokeErr
}

type authFakeAccess struct {
	issuedIdentity Identity
	issuedNow      time.Time
	issueRaw       string
	issueExpiry    time.Time
	issueErr       error
	parseRaw       string
	parseNow       time.Time
	parseIdentity  Identity
	parseErr       error
}

func (a *authFakeAccess) Issue(identity Identity, now time.Time) (string, time.Time, error) {
	a.issuedIdentity, a.issuedNow = identity, now
	return a.issueRaw, a.issueExpiry, a.issueErr
}

func (a *authFakeAccess) Parse(raw string, now time.Time) (Identity, error) {
	a.parseRaw, a.parseNow = raw, now
	return a.parseIdentity, a.parseErr
}

type authFakeRefresh struct {
	raw      string
	digest   string
	err      error
	digested string
}

func (r *authFakeRefresh) Generate() (string, string, error) { return r.raw, r.digest, r.err }
func (r *authFakeRefresh) Digest(raw string) string {
	r.digested = raw
	return r.digest
}

type authFakeIDs struct {
	ids   []string
	err   error
	calls int
}

func (g *authFakeIDs) NewID() (string, error) {
	g.calls++
	if g.err != nil {
		return "", g.err
	}
	if len(g.ids) == 0 {
		return "", errors.New("unexpected ID request")
	}
	id := g.ids[0]
	g.ids = g.ids[1:]
	return id, nil
}

func TestNewAuthServiceRejectsInvalidDependenciesAndTTL(t *testing.T) {
	users, sessions := &authFakeUsers{}, &authFakeSessions{}
	passwords := &authFakePasswords{}
	access, refresh := &authFakeAccess{}, &authFakeRefresh{}
	ids, clock := &authFakeIDs{}, fakeClock{now: fixedTime}

	tests := []struct {
		name      string
		users     AuthUserStore
		sessions  SessionStore
		passwords Passwords
		access    AccessTokens
		refresh   RefreshTokens
		ids       IDGenerator
		clock     Clock
		ttl       time.Duration
		want      string
	}{
		{name: "valid", users: users, sessions: sessions, passwords: passwords, access: access, refresh: refresh, ids: ids, clock: clock, ttl: time.Hour},
		{name: "users", sessions: sessions, passwords: passwords, access: access, refresh: refresh, ids: ids, clock: clock, ttl: time.Hour, want: "user store"},
		{name: "sessions", users: users, passwords: passwords, access: access, refresh: refresh, ids: ids, clock: clock, ttl: time.Hour, want: "session store"},
		{name: "passwords", users: users, sessions: sessions, access: access, refresh: refresh, ids: ids, clock: clock, ttl: time.Hour, want: "passwords"},
		{name: "access", users: users, sessions: sessions, passwords: passwords, refresh: refresh, ids: ids, clock: clock, ttl: time.Hour, want: "access tokens"},
		{name: "refresh", users: users, sessions: sessions, passwords: passwords, access: access, ids: ids, clock: clock, ttl: time.Hour, want: "refresh tokens"},
		{name: "ids", users: users, sessions: sessions, passwords: passwords, access: access, refresh: refresh, clock: clock, ttl: time.Hour, want: "ID generator"},
		{name: "clock", users: users, sessions: sessions, passwords: passwords, access: access, refresh: refresh, ids: ids, ttl: time.Hour, want: "clock"},
		{name: "zero TTL", users: users, sessions: sessions, passwords: passwords, access: access, refresh: refresh, ids: ids, clock: clock, want: "refresh TTL"},
		{name: "negative TTL", users: users, sessions: sessions, passwords: passwords, access: access, refresh: refresh, ids: ids, clock: clock, ttl: -time.Second, want: "refresh TTL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewAuthService(tt.users, tt.sessions, tt.passwords, tt.access, tt.refresh, tt.ids, tt.clock, tt.ttl)
			if tt.want == "" {
				if err != nil || got == nil {
					t.Fatalf("NewAuthService() = (%v, %v), want service", got, err)
				}
				return
			}
			if err == nil || got != nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewAuthService() = (%v, %v), want nil and error containing %q", got, err, tt.want)
			}
		})
	}
}

func TestAuthLoginCreatesOpaqueSessionAndTokens(t *testing.T) {
	now := fixedTime.UTC()
	userRecord := &User{ID: 7, Email: "user@example.com", PasswordHash: "encoded-password", Status: StatusActive, Version: 3}
	users := &authFakeUsers{byEmailUser: userRecord}
	passwords := &authFakePasswords{verifyOK: true}
	sessions := &authFakeSessions{}
	access := &authFakeAccess{issueRaw: testAccessRaw, issueExpiry: now.Add(15 * time.Minute)}
	refresh := &authFakeRefresh{raw: testRefreshRaw, digest: testDigest}
	ids := &authFakeIDs{ids: []string{"session-id", "refresh-id", "access-id"}}
	service := newTestAuthService(t, users, sessions, passwords, access, refresh, ids, time.Hour)

	got, err := service.Login(t.Context(), LoginInput{Email: " USER@Example.COM ", Password: testPassword})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	want := Tokens{AccessToken: testAccessRaw, RefreshToken: testRefreshRaw, AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(time.Hour)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Login() tokens mismatch (-want +got):\n%s", diff)
	}
	if users.byEmailInput != "user@example.com" {
		t.Fatalf("ByEmail() input = %q", users.byEmailInput)
	}
	if passwords.verifyEncoded != "encoded-password" || passwords.verifyRaw != testPassword {
		t.Fatalf("Verify() inputs = %q/%q", passwords.verifyEncoded, passwords.verifyRaw)
	}
	wantSession := &Session{ID: "session-id", UserID: 7, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if diff := cmp.Diff(wantSession, sessions.createdSession); diff != "" {
		t.Fatalf("created session mismatch (-want +got):\n%s", diff)
	}
	wantRefresh := &RefreshToken{ID: "refresh-id", SessionID: "session-id", Digest: testDigest, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if diff := cmp.Diff(wantRefresh, sessions.createdToken); diff != "" {
		t.Fatalf("created refresh mismatch (-want +got):\n%s", diff)
	}
	if sessions.createdToken.Digest == testRefreshRaw || strings.Contains(fmt.Sprintf("%+v", sessions.createdToken), testRefreshRaw) {
		t.Fatal("persisted refresh token contains plaintext bearer")
	}
	if diff := cmp.Diff(Identity{UserID: 7, SessionID: "session-id", TokenID: "access-id"}, access.issuedIdentity); diff != "" {
		t.Fatalf("Issue() identity mismatch (-want +got):\n%s", diff)
	}
}

func TestAuthLoginCollapsesCredentialFailures(t *testing.T) {
	tests := []struct {
		name      string
		user      *User
		lookupErr error
		verifyOK  bool
		verifyErr error
		want      error
	}{
		{name: "not found", lookupErr: ErrNotFound, want: ErrInvalidCredentials},
		{name: "wrong password", user: &User{ID: 7, PasswordHash: "encoded", Status: StatusActive}, want: ErrInvalidCredentials},
		{name: "disabled", user: &User{ID: 7, PasswordHash: "encoded", Status: StatusDisabled}, verifyOK: true, want: ErrDisabled},
		{name: "unknown status", user: &User{ID: 7, PasswordHash: "encoded", Status: "pending"}, verifyOK: true, want: ErrDisabled},
		{name: "verify failure", user: &User{ID: 7, PasswordHash: "encoded", Status: StatusActive}, verifyErr: errors.New("argon failure"), want: ErrInvalidCredentials},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &authFakeUsers{byEmailUser: tt.user, byEmailErr: tt.lookupErr}
			passwords := &authFakePasswords{verifyOK: tt.verifyOK, verifyErr: tt.verifyErr}
			service := newTestAuthService(t, users, &authFakeSessions{}, passwords, &authFakeAccess{}, &authFakeRefresh{}, &authFakeIDs{}, time.Hour)
			got, err := service.Login(t.Context(), LoginInput{Email: "user@example.com", Password: testPassword})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Login() error = %v, want %v", err, tt.want)
			}
			if got != (Tokens{}) {
				t.Fatalf("Login() tokens = %+v, want zero", got)
			}
			if strings.Contains(err.Error(), testPassword) {
				t.Fatal("Login() error exposed password")
			}
		})
	}
}

func TestAuthLoginRehashesPasswordAndAllowsConcurrentConflict(t *testing.T) {
	users := &authFakeUsers{
		byEmailUser: &User{ID: 7, PasswordHash: "old", Status: StatusActive, Version: 4},
		updateErr:   fmt.Errorf("version changed: %w", ErrConflict),
	}
	passwords := &authFakePasswords{verifyOK: true, needsRehash: true, hashOutput: "new"}
	service := newTestAuthService(t, users, &authFakeSessions{}, passwords,
		&authFakeAccess{issueRaw: testAccessRaw, issueExpiry: fixedTime.UTC().Add(time.Minute)},
		&authFakeRefresh{raw: testRefreshRaw, digest: testDigest},
		&authFakeIDs{ids: []string{"session-id", "refresh-id", "access-id"}}, time.Hour)

	if _, err := service.Login(t.Context(), LoginInput{Email: "user@example.com", Password: testPassword}); err != nil {
		t.Fatalf("Login() error = %v; a verified login may continue after a concurrent rehash conflict", err)
	}
	if passwords.hashInput != testPassword || users.updateID != 7 || users.updateHash != "new" || users.updateVer != 4 {
		t.Fatalf("rehash call = hash input %q, update %d/%q/%d", passwords.hashInput, users.updateID, users.updateHash, users.updateVer)
	}
}

func TestAuthLoginRehashStorageFailureStopsBeforeSession(t *testing.T) {
	storageErr := errors.New("database unavailable")
	users := &authFakeUsers{byEmailUser: &User{ID: 7, PasswordHash: "old", Status: StatusActive, Version: 4}, updateErr: storageErr}
	passwords := &authFakePasswords{verifyOK: true, needsRehash: true, hashOutput: "new"}
	sessions := &authFakeSessions{}
	service := newTestAuthService(t, users, sessions, passwords, &authFakeAccess{}, &authFakeRefresh{}, &authFakeIDs{}, time.Hour)

	_, err := service.Login(t.Context(), LoginInput{Email: "user@example.com", Password: testPassword})
	if !errors.Is(err, storageErr) {
		t.Fatalf("Login() error = %v, want storage error", err)
	}
	if sessions.createdSession != nil {
		t.Fatal("Login() created session after failed password rehash")
	}
}

func TestAuthAuthenticateChecksSessionAndCurrentAccount(t *testing.T) {
	now := fixedTime.UTC()
	identity := Identity{UserID: 7, SessionID: "session-id", TokenID: "access-id"}
	access := &authFakeAccess{parseIdentity: identity}
	sessions := &authFakeSessions{activeSession: &Session{ID: "session-id", UserID: 7, ExpiresAt: now.Add(time.Hour)}}
	users := &authFakeUsers{byIDUser: &User{ID: 7, Status: StatusActive}}
	service := newTestAuthService(t, users, sessions, &authFakePasswords{}, access, &authFakeRefresh{}, &authFakeIDs{}, time.Hour)

	got, err := service.Authenticate(t.Context(), testAccessRaw)
	if err != nil || got != identity {
		t.Fatalf("Authenticate() = (%+v, %v), want identity", got, err)
	}
	if sessions.activeInput.UserID != 7 || sessions.activeInput.SessionID != "session-id" || !sessions.activeNow.Equal(now) {
		t.Fatalf("Active() input = %+v at %v", sessions.activeInput, sessions.activeNow)
	}
	if users.byIDInput != 7 {
		t.Fatalf("ByID() input = %d", users.byIDInput)
	}
}

func TestAuthAuthenticateMapsInactiveSessionAndAccount(t *testing.T) {
	tests := []struct {
		name      string
		activeErr error
		user      *User
		userErr   error
		want      error
	}{
		{name: "revoked", activeErr: ErrSessionRevoked, want: ErrSessionRevoked},
		{name: "expired", activeErr: ErrSessionExpired, want: ErrSessionExpired},
		{name: "missing session", activeErr: ErrNotFound, want: ErrSessionRevoked},
		{name: "missing user", userErr: ErrNotFound, want: ErrSessionRevoked},
		{name: "disabled user", user: &User{ID: 7, Status: StatusDisabled}, want: ErrDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestAuthService(t,
				&authFakeUsers{byIDUser: tt.user, byIDErr: tt.userErr},
				&authFakeSessions{activeSession: &Session{ID: "session-id", UserID: 7}, activeErr: tt.activeErr},
				&authFakePasswords{},
				&authFakeAccess{parseIdentity: Identity{UserID: 7, SessionID: "session-id", TokenID: "access-id"}},
				&authFakeRefresh{}, &authFakeIDs{}, time.Hour)
			_, err := service.Authenticate(t.Context(), testAccessRaw)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Authenticate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAuthAuthorizeLoadsPermissionsOnEveryCallAndMatchesExactly(t *testing.T) {
	users := &authFakeUsers{permissions: []string{"users:write", "users:read", "users:write"}}
	service := newTestAuthService(t, users, &authFakeSessions{}, &authFakePasswords{}, &authFakeAccess{}, &authFakeRefresh{}, &authFakeIDs{}, time.Hour)
	identity := Identity{UserID: 7, SessionID: "session-id"}

	if err := service.Authorize(t.Context(), identity, "users:write"); err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	users.permissions = []string{"users:read"}
	if err := service.Authorize(t.Context(), identity, "users:write"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("second Authorize() error = %v, want ErrForbidden", err)
	}
	if err := service.Authorize(t.Context(), identity, "users"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("prefix Authorize() error = %v, want ErrForbidden", err)
	}
}

func TestAuthAuthorizeWrapsStoreFailure(t *testing.T) {
	storageErr := errors.New("database unavailable")
	service := newTestAuthService(t, &authFakeUsers{permissionErr: storageErr}, &authFakeSessions{}, &authFakePasswords{}, &authFakeAccess{}, &authFakeRefresh{}, &authFakeIDs{}, time.Hour)
	if err := service.Authorize(t.Context(), Identity{UserID: 7}, PermissionUsersRead); !errors.Is(err, storageErr) {
		t.Fatalf("Authorize() error = %v, want wrapped store error", err)
	}
}

func TestAuthRefreshRotatesAtomicallyAndIssuesAccess(t *testing.T) {
	now := fixedTime.UTC()
	session := &Session{ID: "session-id", UserID: 7, Rotation: 2, ExpiresAt: now.Add(time.Hour)}
	replaced := &RefreshToken{ID: "new-refresh-id", SessionID: session.ID, Digest: strings.Repeat("b", 64), ExpiresAt: now.Add(45 * time.Minute)}
	sessions := &authFakeSessions{rotateResult: RotateSessionResult{Session: session, RefreshToken: replaced}}
	access := &authFakeAccess{issueRaw: testAccessRaw, issueExpiry: now.Add(15 * time.Minute)}
	refresh := &authFakeRefresh{raw: "new-raw-refresh", digest: replaced.Digest}
	ids := &authFakeIDs{ids: []string{"new-refresh-id", "new-access-id"}}
	service := newTestAuthService(t, &authFakeUsers{}, sessions, &authFakePasswords{}, access, refresh, ids, time.Hour)

	got, err := service.Refresh(t.Context(), testRefreshRaw)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refresh.digested != testRefreshRaw {
		t.Fatalf("Digest() input = %q", refresh.digested)
	}
	if sessions.rotateInput.Digest != replaced.Digest || sessions.rotateInput.NewToken.ID != "new-refresh-id" || !sessions.rotateInput.Now.Equal(now) {
		t.Fatalf("Rotate() input = %+v", sessions.rotateInput)
	}
	if access.issuedIdentity != (Identity{UserID: 7, SessionID: "session-id", TokenID: "new-access-id"}) {
		t.Fatalf("Issue() identity = %+v", access.issuedIdentity)
	}
	want := Tokens{AccessToken: testAccessRaw, RefreshToken: "new-raw-refresh", AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(45 * time.Minute)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Refresh() tokens mismatch (-want +got):\n%s", diff)
	}
}

func TestAuthRefreshReturnsStableErrorsWithoutBearerLeak(t *testing.T) {
	tests := []error{ErrInvalidRefresh, ErrSessionExpired, ErrSessionRevoked, ErrRefreshReuse}
	for _, want := range tests {
		t.Run(want.Error(), func(t *testing.T) {
			service := newTestAuthService(t, &authFakeUsers{}, &authFakeSessions{rotateErr: want}, &authFakePasswords{}, &authFakeAccess{},
				&authFakeRefresh{raw: "new-raw", digest: testDigest}, &authFakeIDs{ids: []string{"refresh-id"}}, time.Hour)
			got, err := service.Refresh(t.Context(), testRefreshRaw)
			if !errors.Is(err, want) || got != (Tokens{}) {
				t.Fatalf("Refresh() = (%+v, %v), want zero/%v", got, err, want)
			}
			if strings.Contains(err.Error(), testRefreshRaw) || strings.Contains(err.Error(), "new-raw") {
				t.Fatal("Refresh() error exposed bearer token")
			}
		})
	}
}

func TestAuthLogoutRevokesMatchingSession(t *testing.T) {
	now := fixedTime.UTC()
	sessions := &authFakeSessions{}
	service := newTestAuthService(t, &authFakeUsers{}, sessions, &authFakePasswords{}, &authFakeAccess{}, &authFakeRefresh{}, &authFakeIDs{}, time.Hour)
	identity := Identity{UserID: 7, SessionID: "session-id", TokenID: "access-id"}
	if err := service.Logout(t.Context(), identity); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if sessions.revokeInput.UserID != 7 || sessions.revokeInput.SessionID != "session-id" || !sessions.revokeNow.Equal(now) {
		t.Fatalf("Revoke() input = %+v at %v", sessions.revokeInput, sessions.revokeNow)
	}
}

func newTestAuthService(t *testing.T, users AuthUserStore, sessions SessionStore, passwords Passwords, access AccessTokens, refresh RefreshTokens, ids IDGenerator, ttl time.Duration) *AuthService {
	t.Helper()
	service, err := NewAuthService(users, sessions, passwords, access, refresh, ids, fakeClock{now: fixedTime}, ttl)
	if err != nil {
		t.Fatalf("NewAuthService() error = %v", err)
	}
	return service
}

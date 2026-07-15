package user

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

type AuthService struct {
	users      AuthUserStore
	sessions   SessionStore
	passwords  Passwords
	access     AccessTokens
	refresh    RefreshTokens
	ids        IDGenerator
	clock      Clock
	refreshTTL time.Duration
}

func NewAuthService(
	users AuthUserStore,
	sessions SessionStore,
	passwords Passwords,
	access AccessTokens,
	refresh RefreshTokens,
	ids IDGenerator,
	clock Clock,
	refreshTTL time.Duration,
) (*AuthService, error) {
	switch {
	case users == nil:
		return nil, errors.New("new auth service: user store is required")
	case sessions == nil:
		return nil, errors.New("new auth service: session store is required")
	case passwords == nil:
		return nil, errors.New("new auth service: passwords are required")
	case access == nil:
		return nil, errors.New("new auth service: access tokens are required")
	case refresh == nil:
		return nil, errors.New("new auth service: refresh tokens are required")
	case ids == nil:
		return nil, errors.New("new auth service: ID generator is required")
	case clock == nil:
		return nil, errors.New("new auth service: clock is required")
	case refreshTTL <= 0:
		return nil, errors.New("new auth service: refresh TTL must be positive")
	}

	return &AuthService{
		users: users, sessions: sessions, passwords: passwords, access: access,
		refresh: refresh, ids: ids, clock: clock, refreshTTL: refreshTTL,
	}, nil
}

func (s *AuthService) Login(ctx context.Context, input LoginInput) (Tokens, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		// Authentication deliberately collapses malformed and unknown email input so
		// callers cannot use response categories to enumerate registered accounts.
		return Tokens{}, ErrInvalidCredentials
	}

	account, err := s.users.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Tokens{}, ErrInvalidCredentials
		}
		return Tokens{}, fmt.Errorf("login: load account: %w", err)
	}
	if account == nil {
		return Tokens{}, ErrInvalidCredentials
	}

	verified, err := s.passwords.Verify(account.PasswordHash, input.Password)
	if err != nil || !verified {
		// Password-decoder errors are indistinguishable from a wrong credential and
		// are not wrapped because third-party messages may include credential input.
		return Tokens{}, ErrInvalidCredentials
	}
	if account.Status != StatusActive {
		return Tokens{}, ErrDisabled
	}
	if err := s.rehashPassword(ctx, account, input.Password); err != nil {
		return Tokens{}, err
	}

	return s.startSession(ctx, account.ID)
}

func (s *AuthService) rehashPassword(ctx context.Context, account *User, password string) error {
	if !s.passwords.NeedsRehash(account.PasswordHash) {
		return nil
	}
	hash, err := s.passwords.Hash(password)
	if err != nil {
		// Hash implementations operate directly on plaintext. Their error strings
		// are not trusted to redact input, so keep the failure category text-only.
		return errors.New("login: refresh password hash failed")
	}
	_, err = s.users.UpdatePasswordHash(ctx, account.ID, hash, account.Version)
	if errors.Is(err, ErrConflict) {
		// The credential has already been verified. A version conflict means another
		// request updated the account first, so failing login would add availability
		// risk without strengthening authentication.
		return nil
	}
	if err != nil {
		return fmt.Errorf("login: persist password hash: %w", err)
	}
	return nil
}

func (s *AuthService) startSession(ctx context.Context, userID uint64) (Tokens, error) {
	now := s.clock.Now().UTC()
	sessionID, err := s.ids.NewID()
	if err != nil {
		return Tokens{}, fmt.Errorf("login: generate session ID: %w", err)
	}
	rawRefresh, digest, err := s.refresh.Generate()
	if err != nil {
		// A generator may return partial bearer material alongside an error. Its
		// message is therefore not trusted at this credential-handling boundary.
		return Tokens{}, errors.New("login: generate refresh token failed")
	}
	refreshID, err := s.ids.NewID()
	if err != nil {
		return Tokens{}, fmt.Errorf("login: generate refresh ID: %w", err)
	}
	expiresAt := now.Add(s.refreshTTL)
	session := &Session{ID: sessionID, UserID: userID, ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now}
	token := &RefreshToken{
		ID: refreshID, SessionID: sessionID, Digest: digest, ExpiresAt: expiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.sessions.Create(ctx, session, token); err != nil {
		return Tokens{}, fmt.Errorf("login: create session: %w", err)
	}

	accessID, err := s.ids.NewID()
	if err != nil {
		return Tokens{}, s.revokeAfterIssueFailure(ctx, "login", session, now, fmt.Errorf("generate access ID: %w", err))
	}
	rawAccess, accessExpiry, err := s.access.Issue(Identity{UserID: userID, SessionID: sessionID, TokenID: accessID}, now)
	if err != nil {
		return Tokens{}, s.revokeAfterIssueFailure(ctx, "login", session, now, errors.New("issue access token"))
	}
	return Tokens{
		AccessToken: rawAccess, RefreshToken: rawRefresh,
		AccessExpiresAt: accessExpiry.UTC(), RefreshExpiresAt: expiresAt,
	}, nil
}

func (s *AuthService) Authenticate(ctx context.Context, rawAccess string) (Identity, error) {
	now := s.clock.Now().UTC()
	identity, err := s.access.Parse(rawAccess, now)
	if err != nil {
		// Access-token parser errors can contain attacker-controlled bearer fragments;
		// retain a safe operation category without returning parser text.
		return Identity{}, fmt.Errorf("authenticate: %w", ErrInvalidAccessToken)
	}
	if identity.UserID == 0 || identity.SessionID == "" || identity.TokenID == "" {
		return Identity{}, fmt.Errorf("authenticate: %w", ErrInvalidAccessToken)
	}

	session, err := s.sessions.Active(ctx, identity.SessionID, identity.UserID, now)
	if err != nil {
		switch {
		case errors.Is(err, ErrSessionExpired):
			return Identity{}, fmt.Errorf("authenticate: %w", ErrSessionExpired)
		case errors.Is(err, ErrSessionRevoked), errors.Is(err, ErrNotFound):
			return Identity{}, fmt.Errorf("authenticate: %w", ErrSessionRevoked)
		default:
			return Identity{}, fmt.Errorf("authenticate: check session: %w", err)
		}
	}
	if session == nil || session.ID != identity.SessionID || session.UserID != identity.UserID {
		return Identity{}, fmt.Errorf("authenticate: %w", ErrSessionRevoked)
	}

	account, err := s.users.ByID(ctx, identity.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Identity{}, fmt.Errorf("authenticate: %w", ErrSessionRevoked)
		}
		return Identity{}, fmt.Errorf("authenticate: load account: %w", err)
	}
	if account == nil || account.ID != identity.UserID {
		return Identity{}, fmt.Errorf("authenticate: %w", ErrSessionRevoked)
	}
	if account.Status != StatusActive {
		return Identity{}, fmt.Errorf("authenticate: %w", ErrDisabled)
	}
	return identity, nil
}

func (s *AuthService) Authorize(ctx context.Context, identity Identity, permission string) error {
	permissions, err := s.users.Permissions(ctx, identity.UserID)
	if err != nil {
		return fmt.Errorf("authorize account: load permissions: %w", err)
	}
	// Permissions are read live on every decision so role revocation takes effect
	// immediately rather than remaining valid for an access token's lifetime.
	if permission == "" || !slices.Contains(permissions, permission) {
		return ErrForbidden
	}
	return nil
}

func (s *AuthService) Refresh(ctx context.Context, rawRefresh string) (Tokens, error) {
	now := s.clock.Now().UTC()
	presentedDigest := s.refresh.Digest(rawRefresh)
	rawReplacement, replacementDigest, err := s.refresh.Generate()
	if err != nil {
		// A faulty generator may return partial bearer material together with an
		// error. Do not retain its text at this credential-handling boundary.
		return Tokens{}, errors.New("refresh session: generate replacement failed")
	}
	replacementID, err := s.ids.NewID()
	if err != nil {
		return Tokens{}, fmt.Errorf("refresh session: generate replacement ID: %w", err)
	}
	replacement := &RefreshToken{
		ID: replacementID, Digest: replacementDigest, ExpiresAt: now.Add(s.refreshTTL),
		CreatedAt: now, UpdatedAt: now,
	}
	rotated, err := s.sessions.Rotate(ctx, RotateSessionInput{Digest: presentedDigest, Now: now, NewToken: replacement})
	if err != nil {
		return Tokens{}, fmt.Errorf("refresh session: %w", err)
	}
	if rotated.Session == nil || rotated.RefreshToken == nil || rotated.Session.ID == "" || rotated.Session.UserID == 0 {
		return Tokens{}, ErrInvalidRefresh
	}
	account, err := s.users.ByID(ctx, rotated.Session.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Tokens{}, s.revokeAfterIssueFailure(ctx, "refresh session", rotated.Session, now, ErrSessionRevoked)
		}
		return Tokens{}, fmt.Errorf("refresh session: load account: %w", err)
	}
	if account == nil || account.ID != rotated.Session.UserID {
		return Tokens{}, s.revokeAfterIssueFailure(ctx, "refresh session", rotated.Session, now, ErrSessionRevoked)
	}
	if account.Status != StatusActive {
		// Rotation already persisted a replacement token, so a concurrent disable or
		// delete must revoke the whole family before returning a domain error.
		return Tokens{}, s.revokeAfterIssueFailure(ctx, "refresh session", rotated.Session, now, ErrDisabled)
	}

	accessID, err := s.ids.NewID()
	if err != nil {
		return Tokens{}, s.revokeAfterIssueFailure(ctx, "refresh session", rotated.Session, now, fmt.Errorf("generate access ID: %w", err))
	}
	rawAccess, accessExpiry, err := s.access.Issue(Identity{
		UserID: rotated.Session.UserID, SessionID: rotated.Session.ID, TokenID: accessID,
	}, now)
	if err != nil {
		return Tokens{}, s.revokeAfterIssueFailure(ctx, "refresh session", rotated.Session, now, errors.New("issue access token"))
	}
	return Tokens{
		AccessToken: rawAccess, RefreshToken: rawReplacement,
		AccessExpiresAt: accessExpiry.UTC(), RefreshExpiresAt: rotated.RefreshToken.ExpiresAt.UTC(),
	}, nil
}

func (s *AuthService) Logout(ctx context.Context, identity Identity) error {
	if identity.UserID == 0 || identity.SessionID == "" {
		return ErrSessionRevoked
	}
	if err := s.sessions.Revoke(ctx, identity.SessionID, identity.UserID, s.clock.Now().UTC()); err != nil {
		return fmt.Errorf("logout: revoke session: %w", err)
	}
	return nil
}

func (s *AuthService) revokeAfterIssueFailure(ctx context.Context, operation string, session *Session, now time.Time, issueErr error) error {
	// Once a session or replacement refresh has been persisted, returning no tokens
	// would strand an unknown live credential family. Revoke it before surfacing the
	// failure; joining preserves a context cancellation or database category.
	revokeErr := s.sessions.Revoke(ctx, session.ID, session.UserID, now)
	return fmt.Errorf("%s: create access token: %w", operation, errors.Join(issueErr, revokeErr))
}

func cloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}
	cloned := *session
	return &cloned
}

func cloneRefreshToken(token *RefreshToken) *RefreshToken {
	if token == nil {
		return nil
	}
	cloned := *token
	return &cloned
}

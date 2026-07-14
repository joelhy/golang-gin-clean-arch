package security

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"clean-arch-gin/user"
	jwtlib "github.com/golang-jwt/jwt/v5"
)

const (
	minimumJWTKeyBytes = 32
	maximumJWTLeeway   = 5 * time.Minute
	maximumJWTBytes    = 8 * 1024
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("expired token")
)

type JWTConfig struct {
	Key      []byte
	Issuer   string
	Audience string
	TTL      time.Duration
	Leeway   time.Duration
}

// JWT issues and parses short-lived HS256 access tokens using immutable configuration.
type JWT struct {
	key      []byte
	issuer   string
	audience string
	ttl      time.Duration
	leeway   time.Duration
}

type accessClaims struct {
	SessionID string `json:"sid"`
	jwtlib.RegisteredClaims
}

func NewJWT(cfg JWTConfig) (*JWT, error) {
	switch {
	case len(cfg.Key) < minimumJWTKeyBytes:
		return nil, fmt.Errorf("JWT key must be at least %d bytes", minimumJWTKeyBytes)
	case strings.TrimSpace(cfg.Issuer) == "":
		return nil, errors.New("JWT issuer is required")
	case strings.TrimSpace(cfg.Audience) == "":
		return nil, errors.New("JWT audience is required")
	case cfg.TTL <= 0:
		return nil, errors.New("JWT TTL must be positive")
	case cfg.Leeway < 0 || cfg.Leeway > maximumJWTLeeway || cfg.Leeway > cfg.TTL:
		return nil, fmt.Errorf("JWT leeway must be non-negative and no greater than %s or the TTL", maximumJWTLeeway)
	}

	// The caller may reuse or clear its configuration buffer after construction;
	// retaining an alias would silently change the signing key of a live service.
	key := bytes.Clone(cfg.Key)
	return &JWT{
		key:      key,
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		ttl:      cfg.TTL,
		leeway:   cfg.Leeway,
	}, nil
}

func (m *JWT) Issue(identity user.Identity, now time.Time) (string, time.Time, error) {
	if identity.UserID == 0 || identity.SessionID == "" || identity.TokenID == "" {
		return "", time.Time{}, ErrInvalidToken
	}

	expiresAt := representableJWTExpiration(now, m.ttl)
	claims := accessClaims{
		SessionID: identity.SessionID,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   strconv.FormatUint(identity.UserID, 10),
			Audience:  jwtlib.ClaimStrings{m.audience},
			ExpiresAt: jwtlib.NewNumericDate(expiresAt),
			NotBefore: jwtlib.NewNumericDate(now),
			IssuedAt:  jwtlib.NewNumericDate(now),
			ID:        identity.TokenID,
		},
	}
	raw, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(m.key)
	if err != nil {
		// Signing failures collapse into the same safe category and deliberately do
		// not wrap library details that could reveal key or token material.
		return "", time.Time{}, ErrInvalidToken
	}
	if len(raw) > maximumJWTBytes {
		return "", time.Time{}, ErrInvalidToken
	}
	return raw, expiresAt, nil
}

func (m *JWT) Parse(raw string, now time.Time) (user.Identity, error) {
	// Reject oversized bearer input before base64 and JSON parsing can allocate in
	// proportion to unauthenticated data. Issued access tokens are far below 8 KiB.
	if len(raw) == 0 || len(raw) > maximumJWTBytes {
		return user.Identity{}, ErrInvalidToken
	}

	claims := new(accessClaims)
	parser := jwtlib.NewParser(
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		jwtlib.WithIssuer(m.issuer),
		jwtlib.WithAudience(m.audience),
		jwtlib.WithExpirationRequired(),
		jwtlib.WithIssuedAt(),
		jwtlib.WithLeeway(m.leeway),
		jwtlib.WithTimeFunc(func() time.Time { return now }),
	)

	token, err := parser.ParseWithClaims(raw, claims, func(token *jwtlib.Token) (any, error) {
		// ValidMethods is the primary algorithm allowlist; the concrete-method check
		// adds defense in depth against future registrations reusing the same name.
		if token.Method != jwtlib.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return m.key, nil
	})
	identity, claimsValid := m.identityFromClaims(claims)
	if err != nil {
		// Signature verification precedes claim validation in jwt/v5. An expiration
		// category is safe only when every structural claim is present and the
		// validator reported no independent issuer, audience, or time violation.
		if token != nil && errors.Is(err, jwtlib.ErrTokenExpired) && claimsValid &&
			!hasNonExpirationJWTError(err) {
			return user.Identity{}, ErrExpiredToken
		}
		// Parser errors may include attacker-controlled token fragments. Return only
		// the stable category so transport logs cannot disclose bearer credentials.
		return user.Identity{}, ErrInvalidToken
	}
	if token == nil || !token.Valid || !claimsValid {
		return user.Identity{}, ErrInvalidToken
	}
	return identity, nil
}

func (m *JWT) identityFromClaims(claims *accessClaims) (user.Identity, bool) {
	if claims == nil || claims.ExpiresAt == nil || claims.IssuedAt == nil ||
		claims.NotBefore == nil || claims.ID == "" || claims.SessionID == "" ||
		claims.Issuer != m.issuer || len(claims.Audience) != 1 || claims.Audience[0] != m.audience ||
		claims.IssuedAt.Time.After(claims.ExpiresAt.Time) || claims.NotBefore.Time.After(claims.ExpiresAt.Time) {
		return user.Identity{}, false
	}

	userID, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil || userID == 0 || claims.Subject != strconv.FormatUint(userID, 10) {
		return user.Identity{}, false
	}
	return user.Identity{UserID: userID, SessionID: claims.SessionID, TokenID: claims.ID}, true
}

func hasNonExpirationJWTError(err error) bool {
	otherErrors := [...]error{
		jwtlib.ErrTokenRequiredClaimMissing,
		jwtlib.ErrTokenInvalidAudience,
		jwtlib.ErrTokenUsedBeforeIssued,
		jwtlib.ErrTokenInvalidIssuer,
		jwtlib.ErrTokenInvalidSubject,
		jwtlib.ErrTokenNotValidYet,
		jwtlib.ErrTokenInvalidId,
	}
	for _, other := range otherErrors {
		if errors.Is(err, other) {
			return true
		}
	}
	return false
}

func representableJWTExpiration(now time.Time, ttl time.Duration) time.Time {
	deadline := now.Add(ttl)
	precision := jwtlib.TimePrecision
	if precision <= 0 {
		return deadline
	}
	expiresAt := deadline.Truncate(precision)
	if expiresAt.Before(deadline) {
		// NumericDate truncates to its configured precision. Rounding expiration
		// upward prevents a positive sub-precision TTL from expiring at issuance.
		expiresAt = expiresAt.Add(precision)
	}
	return expiresAt
}

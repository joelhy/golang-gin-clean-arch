package security

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"clean-arch-gin/user"
	jwtlib "github.com/golang-jwt/jwt/v5"
)

var (
	testJWTKey = bytes.Repeat([]byte("k"), 32)
	testJWTNow = time.Date(2026, time.July, 14, 10, 30, 0, 0, time.UTC)
)

func TestJWTIssueAndParse(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{
		Key:      testJWTKey,
		Issuer:   "test-issuer",
		Audience: "test-api",
		TTL:      15 * time.Minute,
	})
	wantIdentity := user.Identity{UserID: 42, SessionID: "session-123", TokenID: "token-456"}

	raw, expiresAt, err := manager.Issue(wantIdentity, testJWTNow)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if expiresAt != testJWTNow.Add(15*time.Minute) {
		t.Fatalf("Issue() expiresAt = %v, want %v", expiresAt, testJWTNow.Add(15*time.Minute))
	}

	gotIdentity, err := manager.Parse(raw, testJWTNow)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if gotIdentity != wantIdentity {
		t.Fatalf("Parse() identity = %#v, want %#v", gotIdentity, wantIdentity)
	}

	parser := jwtlib.NewParser()
	claims := jwtlib.MapClaims{}
	if _, _, err := parser.ParseUnverified(raw, claims); err != nil {
		t.Fatalf("ParseUnverified() error = %v", err)
	}
	assertMapClaim(t, claims, "iss", "test-issuer")
	assertMapClaim(t, claims, "sub", "42")
	assertMapClaim(t, claims, "jti", "token-456")
	assertMapClaim(t, claims, "sid", "session-123")
	audience, err := claims.GetAudience()
	if err != nil || len(audience) != 1 || audience[0] != "test-api" {
		t.Fatalf("aud claim = %v, %v, want [test-api]", audience, err)
	}
	assertNumericDate(t, claims, "iat", testJWTNow)
	assertNumericDate(t, claims, "nbf", testJWTNow)
	assertNumericDate(t, claims, "exp", expiresAt)
}

func TestJWTClonesSigningKey(t *testing.T) {
	key := bytes.Repeat([]byte("c"), 32)
	manager := newTestJWT(t, JWTConfig{Key: key, Issuer: "issuer", Audience: "api", TTL: time.Minute})
	raw, _, err := manager.Issue(user.Identity{UserID: 1, SessionID: "session", TokenID: "token"}, testJWTNow)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	key[0] ^= 0xff

	if _, err := manager.Parse(raw, testJWTNow); err != nil {
		t.Fatalf("Parse() after caller key mutation error = %v", err)
	}
}

func TestJWTIssueUsesRepresentableFutureExpiration(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: 500 * time.Millisecond})
	now := testJWTNow.Add(123 * time.Millisecond)
	raw, expiresAt, err := manager.Issue(user.Identity{UserID: 1, SessionID: "session", TokenID: "token"}, now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if !expiresAt.After(now) {
		t.Fatalf("Issue() expiresAt = %v, want representable time after now", expiresAt)
	}
	if _, err := manager.Parse(raw, now); err != nil {
		t.Fatalf("Parse(new token) error = %v", err)
	}

	claims := jwtlib.MapClaims{}
	if _, _, err := jwtlib.NewParser().ParseUnverified(raw, claims); err != nil {
		t.Fatalf("ParseUnverified() error = %v", err)
	}
	assertNumericDate(t, claims, "exp", expiresAt)
}

func TestNewJWTRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*JWTConfig)
	}{
		{name: "missing key", mutate: func(c *JWTConfig) { c.Key = nil }},
		{name: "short key", mutate: func(c *JWTConfig) { c.Key = bytes.Repeat([]byte("k"), 31) }},
		{name: "missing issuer", mutate: func(c *JWTConfig) { c.Issuer = " " }},
		{name: "missing audience", mutate: func(c *JWTConfig) { c.Audience = "" }},
		{name: "zero TTL", mutate: func(c *JWTConfig) { c.TTL = 0 }},
		{name: "negative TTL", mutate: func(c *JWTConfig) { c.TTL = -time.Second }},
		{name: "negative leeway", mutate: func(c *JWTConfig) { c.Leeway = -time.Second }},
		{name: "excessive leeway", mutate: func(c *JWTConfig) { c.Leeway = 5*time.Minute + time.Nanosecond }},
		{name: "leeway exceeds TTL", mutate: func(c *JWTConfig) { c.TTL = time.Minute; c.Leeway = time.Minute + time.Nanosecond }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: 15 * time.Minute}
			tt.mutate(&cfg)
			if _, err := NewJWT(cfg); err == nil {
				t.Fatal("NewJWT() error = nil, want configuration error")
			}
		})
	}
}

func TestJWTIssueRejectsIncompleteIdentity(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute})
	tests := []struct {
		name     string
		identity user.Identity
	}{
		{name: "zero user", identity: user.Identity{SessionID: "session", TokenID: "token"}},
		{name: "missing session", identity: user.Identity{UserID: 1, TokenID: "token"}},
		{name: "missing token ID", identity: user.Identity{UserID: 1, SessionID: "session"}},
		{name: "oversized identity", identity: user.Identity{UserID: 1, SessionID: strings.Repeat("s", 8*1024), TokenID: "token"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := manager.Issue(tt.identity, testJWTNow); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Issue() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestJWTRejectsUnexpectedAlgorithms(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute})
	claims := validTestClaims()

	none := jwtlib.NewWithClaims(jwtlib.SigningMethodNone, claims)
	noneRaw, err := none.SignedString(jwtlib.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none token: %v", err)
	}
	hs384Raw := signTestToken(t, jwtlib.SigningMethodHS384, claims, testJWTKey)

	for name, raw := range map[string]string{"none": noneRaw, "HS384": hs384Raw} {
		t.Run(name, func(t *testing.T) {
			if _, err := manager.Parse(raw, testJWTNow); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestJWTRejectsInvalidSignatureAndTokenShape(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute})
	badSignature := signTestToken(t, jwtlib.SigningMethodHS256, validTestClaims(), bytes.Repeat([]byte("x"), 32))

	for name, raw := range map[string]string{
		"bad signature": badSignature,
		"malformed":     "not.a.jwt",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := manager.Parse(raw, testJWTNow)
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
			}
			if strings.Contains(err.Error(), raw) {
				t.Fatalf("Parse() error disclosed raw token: %v", err)
			}
		})
	}
}

func TestJWTRejectsOversizedRawToken(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute})
	claims := validTestClaims()
	claims.SessionID = strings.Repeat("s", 8*1024)
	raw := signTestToken(t, jwtlib.SigningMethodHS256, claims, testJWTKey)
	if len(raw) <= 8*1024 {
		t.Fatalf("test token length = %d, want over input limit", len(raw))
	}

	if _, err := manager.Parse(raw, testJWTNow); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
	}
}

func TestJWTRejectsWrongIssuerOrAudience(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute})
	tests := []struct {
		name   string
		mutate func(*testAccessClaims)
	}{
		{name: "wrong issuer", mutate: func(c *testAccessClaims) { c.Issuer = "other" }},
		{name: "missing issuer", mutate: func(c *testAccessClaims) { c.Issuer = "" }},
		{name: "wrong audience", mutate: func(c *testAccessClaims) { c.Audience = jwtlib.ClaimStrings{"other"} }},
		{name: "missing audience", mutate: func(c *testAccessClaims) { c.Audience = nil }},
		{name: "extra audience", mutate: func(c *testAccessClaims) { c.Audience = jwtlib.ClaimStrings{"api", "other"} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := validTestClaims()
			tt.mutate(&claims)
			raw := signTestToken(t, jwtlib.SigningMethodHS256, claims, testJWTKey)
			if _, err := manager.Parse(raw, testJWTNow); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestJWTRequiresIdentityAndTimeClaims(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute})
	tests := []struct {
		name   string
		mutate func(*testAccessClaims)
	}{
		{name: "missing expiration", mutate: func(c *testAccessClaims) { c.ExpiresAt = nil }},
		{name: "missing issued at", mutate: func(c *testAccessClaims) { c.IssuedAt = nil }},
		{name: "missing not before", mutate: func(c *testAccessClaims) { c.NotBefore = nil }},
		{name: "missing token ID", mutate: func(c *testAccessClaims) { c.ID = "" }},
		{name: "missing session", mutate: func(c *testAccessClaims) { c.SessionID = "" }},
		{name: "missing subject", mutate: func(c *testAccessClaims) { c.Subject = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := validTestClaims()
			tt.mutate(&claims)
			raw := signTestToken(t, jwtlib.SigningMethodHS256, claims, testJWTKey)
			if _, err := manager.Parse(raw, testJWTNow); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestJWTRejectsInvalidUserSubjects(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute})
	for _, subject := range []string{"abc", "0", "-1", "+1", "01", "18446744073709551616"} {
		t.Run(subject, func(t *testing.T) {
			claims := validTestClaims()
			claims.Subject = subject
			raw := signTestToken(t, jwtlib.SigningMethodHS256, claims, testJWTKey)
			if _, err := manager.Parse(raw, testJWTNow); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestJWTValidatesTimeAgainstSuppliedNow(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute})

	expired := validTestClaims()
	expired.ExpiresAt = jwtlib.NewNumericDate(testJWTNow.Add(-time.Second))
	expired.IssuedAt = jwtlib.NewNumericDate(testJWTNow.Add(-time.Minute))
	expired.NotBefore = jwtlib.NewNumericDate(testJWTNow.Add(-time.Minute))
	expiredRaw := signTestToken(t, jwtlib.SigningMethodHS256, expired, testJWTKey)
	if _, err := manager.Parse(expiredRaw, testJWTNow); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Parse(expired) error = %v, want ErrExpiredToken", err)
	}

	for name, mutate := range map[string]func(*testAccessClaims){
		"not yet valid":    func(c *testAccessClaims) { c.NotBefore = jwtlib.NewNumericDate(testJWTNow.Add(time.Second)) },
		"issued in future": func(c *testAccessClaims) { c.IssuedAt = jwtlib.NewNumericDate(testJWTNow.Add(time.Second)) },
	} {
		t.Run(name, func(t *testing.T) {
			claims := validTestClaims()
			mutate(&claims)
			raw := signTestToken(t, jwtlib.SigningMethodHS256, claims, testJWTKey)
			if _, err := manager.Parse(raw, testJWTNow); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestJWTExpiredClassificationRequiresOtherwiseValidClaims(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute})
	tests := []struct {
		name   string
		mutate func(*testAccessClaims)
	}{
		{name: "wrong issuer", mutate: func(c *testAccessClaims) { c.Issuer = "other" }},
		{name: "missing session", mutate: func(c *testAccessClaims) { c.SessionID = "" }},
		{name: "future not before", mutate: func(c *testAccessClaims) { c.NotBefore = jwtlib.NewNumericDate(testJWTNow.Add(time.Minute)) }},
		{name: "issued after expiration", mutate: func(c *testAccessClaims) { c.IssuedAt = jwtlib.NewNumericDate(testJWTNow) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := validTestClaims()
			claims.ExpiresAt = jwtlib.NewNumericDate(testJWTNow.Add(-time.Second))
			claims.IssuedAt = jwtlib.NewNumericDate(testJWTNow.Add(-time.Minute))
			claims.NotBefore = jwtlib.NewNumericDate(testJWTNow.Add(-time.Minute))
			tt.mutate(&claims)
			raw := signTestToken(t, jwtlib.SigningMethodHS256, claims, testJWTKey)
			if _, err := manager.Parse(raw, testJWTNow); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestJWTAppliesBoundedLeeway(t *testing.T) {
	manager := newTestJWT(t, JWTConfig{
		Key: testJWTKey, Issuer: "issuer", Audience: "api", TTL: time.Minute, Leeway: time.Minute,
	})
	withinLeeway := validTestClaims()
	withinLeeway.ExpiresAt = jwtlib.NewNumericDate(testJWTNow.Add(-30 * time.Second))
	withinLeeway.IssuedAt = jwtlib.NewNumericDate(testJWTNow.Add(-2 * time.Minute))
	withinLeeway.NotBefore = jwtlib.NewNumericDate(testJWTNow.Add(-2 * time.Minute))
	withinRaw := signTestToken(t, jwtlib.SigningMethodHS256, withinLeeway, testJWTKey)
	if _, err := manager.Parse(withinRaw, testJWTNow); err != nil {
		t.Fatalf("Parse(within leeway) error = %v", err)
	}

	beyondLeeway := validTestClaims()
	beyondLeeway.ExpiresAt = jwtlib.NewNumericDate(testJWTNow.Add(-61 * time.Second))
	beyondLeeway.IssuedAt = jwtlib.NewNumericDate(testJWTNow.Add(-2 * time.Minute))
	beyondLeeway.NotBefore = jwtlib.NewNumericDate(testJWTNow.Add(-2 * time.Minute))
	beyondRaw := signTestToken(t, jwtlib.SigningMethodHS256, beyondLeeway, testJWTKey)
	if _, err := manager.Parse(beyondRaw, testJWTNow); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Parse(beyond leeway) error = %v, want ErrExpiredToken", err)
	}
}

type testAccessClaims struct {
	SessionID string `json:"sid"`
	jwtlib.RegisteredClaims
}

func validTestClaims() testAccessClaims {
	return testAccessClaims{
		SessionID: "session",
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    "issuer",
			Subject:   strconv.FormatUint(42, 10),
			Audience:  jwtlib.ClaimStrings{"api"},
			ExpiresAt: jwtlib.NewNumericDate(testJWTNow.Add(time.Minute)),
			NotBefore: jwtlib.NewNumericDate(testJWTNow),
			IssuedAt:  jwtlib.NewNumericDate(testJWTNow),
			ID:        "token",
		},
	}
}

func signTestToken(t *testing.T, method jwtlib.SigningMethod, claims jwtlib.Claims, key []byte) string {
	t.Helper()
	raw, err := jwtlib.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return raw
}

func newTestJWT(t *testing.T, cfg JWTConfig) *JWT {
	t.Helper()
	manager, err := NewJWT(cfg)
	if err != nil {
		t.Fatalf("NewJWT() error = %v", err)
	}
	return manager
}

func assertMapClaim(t *testing.T, claims jwtlib.MapClaims, name, want string) {
	t.Helper()
	if got, ok := claims[name].(string); !ok || got != want {
		t.Fatalf("claim %q = %#v, want %q", name, claims[name], want)
	}
}

func assertNumericDate(t *testing.T, claims jwtlib.MapClaims, name string, want time.Time) {
	t.Helper()
	got, ok := claims[name].(float64)
	if !ok || int64(got) != want.Unix() {
		t.Fatalf("claim %q = %#v, want Unix %d", name, claims[name], want.Unix())
	}
}

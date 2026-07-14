package security

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

var testPasswordConfig = PasswordConfig{
	Memory:      19 * 1024,
	Iterations:  1,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

func TestPasswordManagerHashAndVerify(t *testing.T) {
	manager := newTestPasswordManager(t, testPasswordConfig, bytes.NewReader(bytes.Repeat([]byte{0x5a}, 16)))

	encoded, err := manager.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=19456,t=1,p=1$") {
		t.Fatal("Hash() did not use the standard Argon2id encoding")
	}

	matched, err := manager.Verify(encoded, "correct horse battery staple")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !matched {
		t.Fatal("Verify() = false, want true")
	}

	matched, err = manager.Verify(encoded, "wrong password")
	if err != nil {
		t.Fatalf("Verify(wrong password) error = %v", err)
	}
	if matched {
		t.Fatal("Verify(wrong password) = true, want false")
	}
}

func TestPasswordManagerUsesInjectedRandomReader(t *testing.T) {
	salt := bytes.Repeat([]byte{0xa5}, 16)
	manager := newTestPasswordManager(t, testPasswordConfig, bytes.NewReader(salt))

	encoded, err := manager.Hash("deterministic password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		t.Fatalf("Hash() field count = %d, want 6", len(parts))
	}
	if parts[4] != base64.RawStdEncoding.EncodeToString(salt) {
		t.Fatalf("Hash() salt = %q, want deterministic salt", parts[4])
	}
}

func TestPasswordManagerPropagatesRandomReaderFailure(t *testing.T) {
	sourceErr := errors.New("random source unavailable")
	manager := newTestPasswordManager(t, testPasswordConfig, failingReader{err: sourceErr})

	_, err := manager.Hash("do not disclose this password")
	if !errors.Is(err, ErrRandomSource) || !errors.Is(err, sourceErr) {
		t.Fatalf("Hash() error = %v, want ErrRandomSource and source error", err)
	}
	if strings.Contains(err.Error(), "do not disclose") {
		t.Fatalf("Hash() error disclosed password: %v", err)
	}
}

func TestNewPasswordManagerRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PasswordConfig)
	}{
		{name: "memory below minimum", mutate: func(c *PasswordConfig) { c.Memory = 19*1024 - 1 }},
		{name: "memory above maximum", mutate: func(c *PasswordConfig) { c.Memory = 1024*1024 + 1 }},
		{name: "negative memory", mutate: func(c *PasswordConfig) { c.Memory = -1 }},
		{name: "iterations zero", mutate: func(c *PasswordConfig) { c.Iterations = 0 }},
		{name: "iterations above maximum", mutate: func(c *PasswordConfig) { c.Iterations = 11 }},
		{name: "parallelism zero", mutate: func(c *PasswordConfig) { c.Parallelism = 0 }},
		{name: "parallelism above maximum", mutate: func(c *PasswordConfig) { c.Parallelism = 33 }},
		{name: "salt too short", mutate: func(c *PasswordConfig) { c.SaltLength = 15 }},
		{name: "salt too long", mutate: func(c *PasswordConfig) { c.SaltLength = 65 }},
		{name: "key too short", mutate: func(c *PasswordConfig) { c.KeyLength = 15 }},
		{name: "key too long", mutate: func(c *PasswordConfig) { c.KeyLength = 65 }},
		{name: "signed conversion overflow", mutate: func(c *PasswordConfig) { c.Memory = math.MaxInt64 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testPasswordConfig
			tt.mutate(&cfg)
			if _, err := NewPasswordManager(cfg, bytes.NewReader(nil)); err == nil {
				t.Fatal("NewPasswordManager() error = nil, want configuration error")
			}
		})
	}

	if _, err := NewPasswordManager(testPasswordConfig, nil); !errors.Is(err, ErrRandomSource) {
		t.Fatalf("NewPasswordManager(nil reader) error = %v, want ErrRandomSource", err)
	}
}

func TestPasswordManagerRejectsMalformedHashesBeforeArgon2(t *testing.T) {
	manager := newTestPasswordManager(t, testPasswordConfig, bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	validSalt := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	validHash := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))

	tests := []struct {
		name    string
		encoded string
	}{
		{name: "empty", encoded: ""},
		{name: "wrong field count", encoded: "$argon2id$v=19$m=19456,t=1,p=1$" + validSalt},
		{name: "extra field", encoded: "$argon2id$v=19$m=19456,t=1,p=1$" + validSalt + "$" + validHash + "$extra"},
		{name: "wrong algorithm", encoded: "$argon2i$v=19$m=19456,t=1,p=1$" + validSalt + "$" + validHash},
		{name: "wrong version", encoded: "$argon2id$v=18$m=19456,t=1,p=1$" + validSalt + "$" + validHash},
		{name: "malformed version", encoded: "$argon2id$v=x$m=19456,t=1,p=1$" + validSalt + "$" + validHash},
		{name: "missing parameter", encoded: "$argon2id$v=19$m=19456,t=1$" + validSalt + "$" + validHash},
		{name: "duplicate parameter", encoded: "$argon2id$v=19$m=19456,m=19456,p=1$" + validSalt + "$" + validHash},
		{name: "unknown parameter", encoded: "$argon2id$v=19$m=19456,t=1,x=1$" + validSalt + "$" + validHash},
		{name: "invalid parameter number", encoded: "$argon2id$v=19$m=lots,t=1,p=1$" + validSalt + "$" + validHash},
		{name: "oversized memory", encoded: "$argon2id$v=19$m=1048577,t=1,p=1$" + validSalt + "$" + validHash},
		{name: "oversized iterations", encoded: "$argon2id$v=19$m=19456,t=11,p=1$" + validSalt + "$" + validHash},
		{name: "oversized parallelism", encoded: "$argon2id$v=19$m=19456,t=1,p=33$" + validSalt + "$" + validHash},
		{name: "overflow parameter", encoded: "$argon2id$v=19$m=18446744073709551615,t=1,p=1$" + validSalt + "$" + validHash},
		{name: "invalid salt base64", encoded: "$argon2id$v=19$m=19456,t=1,p=1$not+raw/std$" + validHash},
		{name: "short salt", encoded: "$argon2id$v=19$m=19456,t=1,p=1$" + base64.RawStdEncoding.EncodeToString([]byte("short")) + "$" + validHash},
		{name: "oversized salt field", encoded: "$argon2id$v=19$m=19456,t=1,p=1$" + strings.Repeat("A", 100) + "$" + validHash},
		{name: "invalid hash base64", encoded: "$argon2id$v=19$m=19456,t=1,p=1$" + validSalt + "$not+raw/std"},
		{name: "short hash", encoded: "$argon2id$v=19$m=19456,t=1,p=1$" + validSalt + "$" + base64.RawStdEncoding.EncodeToString([]byte("short"))},
		{name: "oversized hash field", encoded: "$argon2id$v=19$m=19456,t=1,p=1$" + validSalt + "$" + strings.Repeat("A", 100)},
		{name: "oversized encoding", encoded: strings.Repeat("A", 1024)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := manager.Verify(tt.encoded, "password")
			if matched || !errors.Is(err, ErrInvalidPasswordHash) {
				t.Fatalf("Verify() = (%v, %v), want (false, ErrInvalidPasswordHash)", matched, err)
			}
			if strings.Contains(err.Error(), tt.encoded) && tt.encoded != "" {
				t.Fatalf("Verify() error disclosed encoded hash: %v", err)
			}
		})
	}
}

func TestPasswordManagerRejectsArgon2MemoryBelowParallelismRequirement(t *testing.T) {
	manager := newTestPasswordManager(t, testPasswordConfig, bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	salt := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	hash := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	encoded := "$argon2id$v=19$m=8,t=1,p=2$" + salt + "$" + hash

	if _, err := manager.Verify(encoded, "password"); !errors.Is(err, ErrInvalidPasswordHash) {
		t.Fatalf("Verify() error = %v, want ErrInvalidPasswordHash", err)
	}
}

func TestPasswordManagerRejectsHashesAboveConfiguredWorkLimit(t *testing.T) {
	manager := newTestPasswordManager(t, testPasswordConfig, bytes.NewReader(bytes.Repeat([]byte{1}, 16)))
	salt := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	hash := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	tests := []struct {
		name       string
		parameters string
	}{
		{name: "memory", parameters: "m=19457,t=1,p=1"},
		{name: "iterations", parameters: "m=19456,t=2,p=1"},
		{name: "parallelism", parameters: "m=19456,t=1,p=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := "$argon2id$v=19$" + tt.parameters + "$" + salt + "$" + hash
			if _, err := manager.Verify(encoded, "password"); !errors.Is(err, ErrInvalidPasswordHash) {
				t.Fatalf("Verify() error = %v, want ErrInvalidPasswordHash", err)
			}
		})
	}
}

func TestPasswordManagerVerifiesOlderWeakerHash(t *testing.T) {
	old := newTestPasswordManager(t, testPasswordConfig, bytes.NewReader(bytes.Repeat([]byte{6}, 16)))
	oldHash, err := old.Hash("password")
	if err != nil {
		t.Fatalf("Hash(old) error = %v", err)
	}

	currentConfig := testPasswordConfig
	currentConfig.Memory++
	currentConfig.Iterations++
	currentConfig.Parallelism++
	current := newTestPasswordManager(t, currentConfig, bytes.NewReader(bytes.Repeat([]byte{7}, 16)))
	matched, err := current.Verify(oldHash, "password")
	if err != nil || !matched {
		t.Fatalf("Verify(old hash) = (%v, %v), want (true, nil)", matched, err)
	}
	if !current.NeedsRehash(oldHash) {
		t.Fatal("NeedsRehash(old hash) = false, want true")
	}
}

func TestPasswordManagerDetectsCorruptedHash(t *testing.T) {
	manager := newTestPasswordManager(t, testPasswordConfig, bytes.NewReader(bytes.Repeat([]byte{3}, 16)))
	encoded, err := manager.Hash("correct password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	lastSeparator := strings.LastIndexByte(encoded, '$')
	replacement := byte('A')
	if encoded[lastSeparator+1] == replacement {
		replacement = 'B'
	}
	corrupted := encoded[:lastSeparator+1] + string(replacement) + encoded[lastSeparator+2:]

	matched, err := manager.Verify(corrupted, "correct password")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if matched {
		t.Fatal("Verify(corrupted hash) = true, want false")
	}
}

func TestPasswordManagerNeedsRehash(t *testing.T) {
	current := newTestPasswordManager(t, testPasswordConfig, bytes.NewReader(bytes.Repeat([]byte{4}, 32)))
	currentHash, err := current.Hash("password")
	if err != nil {
		t.Fatalf("Hash(current) error = %v", err)
	}
	if current.NeedsRehash(currentHash) {
		t.Fatal("NeedsRehash(current hash) = true, want false")
	}

	oldConfig := testPasswordConfig
	oldConfig.Memory++
	old := newTestPasswordManager(t, oldConfig, bytes.NewReader(bytes.Repeat([]byte{5}, 16)))
	oldHash, err := old.Hash("password")
	if err != nil {
		t.Fatalf("Hash(old) error = %v", err)
	}
	if !current.NeedsRehash(oldHash) {
		t.Fatal("NeedsRehash(old hash) = false, want true")
	}
	if !current.NeedsRehash("not-a-hash") {
		t.Fatal("NeedsRehash(malformed hash) = false, want true")
	}
}

func newTestPasswordManager(t *testing.T, cfg PasswordConfig, reader io.Reader) *PasswordManager {
	t.Helper()
	manager, err := NewPasswordManager(cfg, reader)
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	return manager
}

type failingReader struct {
	err error
}

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

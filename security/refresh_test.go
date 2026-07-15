package security

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestRefreshGeneratorGenerateAndDigest(t *testing.T) {
	random := bytes.Repeat([]byte{0x7b}, 32)
	generator, err := NewRefreshGenerator(bytes.NewReader(random))
	if err != nil {
		t.Fatalf("NewRefreshGenerator() error = %v", err)
	}

	raw, digest, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	wantRaw := base64.RawURLEncoding.EncodeToString(random)
	sum := sha256.Sum256([]byte(wantRaw))
	wantDigest := hex.EncodeToString(sum[:])
	if raw != wantRaw {
		t.Fatal("Generate() raw token did not match the injected random bytes")
	}
	if digest != wantDigest || generator.Digest(raw) != wantDigest {
		t.Fatalf("Generate() digest = %q and Digest() = %q, want %q", digest, generator.Digest(raw), wantDigest)
	}
	if strings.Contains(digest, raw) || len(digest) != sha256.Size*2 || digest != strings.ToLower(digest) {
		t.Fatalf("Generate() digest = %q, want separate lowercase SHA-256 hex", digest)
	}
}

func TestRefreshGeneratorPropagatesRandomReaderFailure(t *testing.T) {
	sourceErr := errors.New("entropy failure")
	generator, err := NewRefreshGenerator(failingReader{err: sourceErr})
	if err != nil {
		t.Fatalf("NewRefreshGenerator() error = %v", err)
	}

	raw, digest, err := generator.Generate()
	if raw != "" || digest != "" {
		t.Fatalf("Generate() = (%q, %q, %v), want empty token and digest", raw, digest, err)
	}
	if !errors.Is(err, ErrRandomSource) || !errors.Is(err, sourceErr) {
		t.Fatalf("Generate() error = %v, want ErrRandomSource and source error", err)
	}
}

func TestNewRefreshGeneratorRejectsNilReader(t *testing.T) {
	if _, err := NewRefreshGenerator(nil); !errors.Is(err, ErrRandomSource) {
		t.Fatalf("NewRefreshGenerator(nil) error = %v, want ErrRandomSource", err)
	}
}

func TestRefreshGeneratorProducesUniqueTokens(t *testing.T) {
	generator, err := NewRefreshGenerator(rand.Reader)
	if err != nil {
		t.Fatalf("NewRefreshGenerator() error = %v", err)
	}
	seen := make(map[string]struct{}, 10_000)
	for range 10_000 {
		raw, digest, err := generator.Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		if _, exists := seen[raw]; exists {
			t.Fatal("Generate() produced a duplicate refresh token")
		}
		seen[raw] = struct{}{}
		if digest != generator.Digest(raw) {
			t.Fatal("Generate() digest does not match Digest(raw)")
		}
	}
}

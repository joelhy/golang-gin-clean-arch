package security

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
)

func TestRandomIDGeneratorUses128BitsAndBase64URL(t *testing.T) {
	random := bytes.Repeat([]byte{0xfe}, 16)
	generator, err := NewRandomIDGenerator(bytes.NewReader(random))
	if err != nil {
		t.Fatalf("NewRandomIDGenerator() error = %v", err)
	}

	got, err := generator.NewID()
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}
	want := base64.RawURLEncoding.EncodeToString(random)
	if got != want {
		t.Fatalf("NewID() = %q, want %q", got, want)
	}
	if len(got) != base64.RawURLEncoding.EncodedLen(16) {
		t.Fatalf("NewID() length = %d, want base64url encoding of 16 bytes", len(got))
	}
}

func TestRandomIDGeneratorPropagatesRandomReaderFailure(t *testing.T) {
	sourceErr := errors.New("entropy failure")
	generator, err := NewRandomIDGenerator(failingReader{err: sourceErr})
	if err != nil {
		t.Fatalf("NewRandomIDGenerator() error = %v", err)
	}

	got, err := generator.NewID()
	if got != "" || !errors.Is(err, ErrRandomSource) || !errors.Is(err, sourceErr) {
		t.Fatalf("NewID() = (%q, %v), want empty ID, ErrRandomSource, and source error", got, err)
	}
}

func TestNewRandomIDGeneratorRejectsNilReader(t *testing.T) {
	if _, err := NewRandomIDGenerator(nil); !errors.Is(err, ErrRandomSource) {
		t.Fatalf("NewRandomIDGenerator(nil) error = %v, want ErrRandomSource", err)
	}
}

func TestRandomIDGeneratorProducesUniqueIDs(t *testing.T) {
	generator, err := NewRandomIDGenerator(rand.Reader)
	if err != nil {
		t.Fatalf("NewRandomIDGenerator() error = %v", err)
	}
	seen := make(map[string]struct{}, 10_000)
	for range 10_000 {
		id, err := generator.NewID()
		if err != nil {
			t.Fatalf("NewID() error = %v", err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("NewID() produced duplicate ID %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestRandomIDGeneratorSupportsConcurrentUse(t *testing.T) {
	generator, err := NewRandomIDGenerator(rand.Reader)
	if err != nil {
		t.Fatalf("NewRandomIDGenerator() error = %v", err)
	}

	const count = 1_000
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			id, err := generator.NewID()
			ids <- id
			errs <- err
		})
	}
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("NewID() error = %v", err)
		}
	}
	seen := make(map[string]struct{}, count)
	for id := range ids {
		if _, exists := seen[id]; exists {
			t.Fatalf("NewID() produced duplicate concurrent ID %q", id)
		}
		seen[id] = struct{}{}
	}
}

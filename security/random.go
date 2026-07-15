package security

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"
)

const randomIDBytes = 16

var ErrRandomSource = errors.New("random source failed")

// lockedRandomReader serializes access because injected readers such as bytes.Reader
// are often not concurrency-safe. The caller transfers reader ownership to the
// security component and must not read from it independently after construction.
type lockedRandomReader struct {
	mu     sync.Mutex
	reader io.Reader
}

func newLockedRandomReader(reader io.Reader) (*lockedRandomReader, error) {
	if reader == nil {
		return nil, ErrRandomSource
	}
	return &lockedRandomReader{reader: reader}, nil
}

func (r *lockedRandomReader) readFull(destination []byte, operation string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := io.ReadFull(r.reader, destination); err != nil {
		// Join preserves both the stable classification and the reader's concrete
		// failure for errors.Is without exposing any generated secret bytes.
		return fmt.Errorf("%s: %w", operation, errors.Join(ErrRandomSource, err))
	}
	return nil
}

// RandomIDGenerator creates opaque 128-bit identifiers for security records and JWT claims.
type RandomIDGenerator struct {
	random *lockedRandomReader
}

// NewRandomIDGenerator takes exclusive ownership of reader so concurrent ID
// generation cannot interleave reads on a non-thread-safe injected source.
func NewRandomIDGenerator(reader io.Reader) (*RandomIDGenerator, error) {
	random, err := newLockedRandomReader(reader)
	if err != nil {
		return nil, err
	}
	return &RandomIDGenerator{random: random}, nil
}

func (g *RandomIDGenerator) NewID() (string, error) {
	random := make([]byte, randomIDBytes)
	if err := g.random.readFull(random, "generate random identifier"); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

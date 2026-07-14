package security

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
)

const refreshTokenBytes = 32

// RefreshGenerator returns raw bearer tokens separately from the one-way digest
// that persistence adapters store. Keeping that boundary explicit prevents an
// accidental database write of a credential that can authenticate by itself.
type RefreshGenerator struct {
	random *lockedRandomReader
}

// NewRefreshGenerator takes exclusive ownership of reader so concurrent token
// generation cannot interleave reads on a non-thread-safe injected source.
func NewRefreshGenerator(reader io.Reader) (*RefreshGenerator, error) {
	random, err := newLockedRandomReader(reader)
	if err != nil {
		return nil, err
	}
	return &RefreshGenerator{random: random}, nil
}

func (g *RefreshGenerator) Generate() (raw, digest string, err error) {
	random := make([]byte, refreshTokenBytes)
	if err := g.random.readFull(random, "generate refresh token"); err != nil {
		return "", "", err
	}

	raw = base64.RawURLEncoding.EncodeToString(random)
	return raw, g.Digest(raw), nil
}

func (*RefreshGenerator) Digest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

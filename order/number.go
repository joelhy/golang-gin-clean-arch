package order

import (
	"encoding/base32"
	"fmt"
	"io"
	"time"
)

type randomNumberGenerator struct {
	random io.Reader
}

func NewNumberGenerator(random io.Reader) NumberGenerator {
	return &randomNumberGenerator{random: random}
}

func (g *randomNumberGenerator) New(now time.Time) (string, error) {
	var randomBits [10]byte
	if _, err := io.ReadFull(g.random, randomBits[:]); err != nil {
		return "", fmt.Errorf("read random suffix: %w", err)
	}
	suffix := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBits[:])
	return "ORD-" + now.UTC().Format("20060102") + "-" + suffix, nil
}

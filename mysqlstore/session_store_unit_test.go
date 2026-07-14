package mysqlstore

import (
	"bytes"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"clean-arch-gin/user"
)

func TestRefreshDigestMatchesRequiresExact32ByteDigest(t *testing.T) {
	digest := bytes.Repeat([]byte{0x42}, 32)
	different := bytes.Clone(digest)
	different[31] ^= 0xff

	tests := []struct {
		name      string
		stored    []byte
		presented []byte
		want      bool
	}{
		{name: "equal", stored: digest, presented: bytes.Clone(digest), want: true},
		{name: "different", stored: digest, presented: different},
		{name: "short stored", stored: digest[:31], presented: digest},
		{name: "short presented", stored: digest, presented: digest[:31]},
		{name: "long stored", stored: append(bytes.Clone(digest), 0), presented: digest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refreshDigestMatches(tt.stored, tt.presented); got != tt.want {
				t.Fatalf("refreshDigestMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewSessionStoreRejectsNilDatabase(t *testing.T) {
	store, err := NewSessionStore(nil)
	if err == nil || store != nil {
		t.Fatalf("NewSessionStore(nil) = (%v, %v), want nil/error", store, err)
	}
}

func TestSessionStoreRejectsNonCanonicalDigestWithoutLeak(t *testing.T) {
	sqlDB := sql.OpenDB(&stubConnector{})
	t.Cleanup(func() { _ = sqlDB.Close() })
	store, err := NewSessionStore(newTestGORMDB(t, sqlDB))
	if err != nil {
		t.Fatalf("NewSessionStore() error = %v", err)
	}
	now := time.Now().UTC()
	invalid := strings.Repeat("A", 64)
	err = store.Create(t.Context(),
		&user.Session{ID: strings.Repeat("s", 22), UserID: 1, ExpiresAt: now.Add(time.Hour)},
		&user.RefreshToken{ID: strings.Repeat("t", 22), SessionID: strings.Repeat("s", 22), Digest: invalid, ExpiresAt: now.Add(time.Hour)},
	)
	if !errors.Is(err, user.ErrInvalidRefresh) {
		t.Fatalf("Create() error = %v, want ErrInvalidRefresh", err)
	}
	if strings.Contains(err.Error(), invalid) {
		t.Fatal("Create() error exposed invalid digest")
	}

	_, err = store.Rotate(t.Context(), user.RotateSessionInput{
		Digest: "not-hex",
		Now:    now,
		NewToken: &user.RefreshToken{
			ID: strings.Repeat("n", 22), Digest: strings.Repeat("1", 64), ExpiresAt: now.Add(time.Hour),
		},
	})
	if !errors.Is(err, user.ErrInvalidRefresh) || strings.Contains(err.Error(), "not-hex") {
		t.Fatalf("Rotate() error = %v, want redacted ErrInvalidRefresh", err)
	}
}

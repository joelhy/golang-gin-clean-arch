//go:build integration

package mysqlstore

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"clean-arch-gin/mysqlstore/query"
	"clean-arch-gin/user"
	"gorm.io/gorm"
)

func TestSessionStoreLifecycleAndReuseRevocation(t *testing.T) {
	db := newTestDB(t)
	users := mustUserStore(t, db)
	account := createStoredUser(t, users, "session-user@example.com", user.RoleCustomer)
	store := mustSessionStore(t, db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	session := &user.Session{ID: strings.Repeat("s", 22), UserID: account.ID, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	first := &user.RefreshToken{
		ID: strings.Repeat("a", 22), SessionID: session.ID, Digest: strings.Repeat("1", 64),
		ExpiresAt: session.ExpiresAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Create(t.Context(), session, first); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	active, err := store.Active(t.Context(), session.ID, account.ID, now)
	if err != nil || active.ID != session.ID || active.ExpiresAt.Location() != time.UTC {
		t.Fatalf("Active() = (%+v, %v)", active, err)
	}

	replacement := &user.RefreshToken{
		ID: strings.Repeat("b", 22), Digest: strings.Repeat("2", 64),
		ExpiresAt: now.Add(2 * time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	rotated, err := store.Rotate(t.Context(), user.RotateSessionInput{Digest: first.Digest, Now: now.Add(time.Minute), NewToken: replacement})
	if err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}
	if rotated.Session.Rotation != 1 || rotated.RefreshToken.SessionID != session.ID || !rotated.RefreshToken.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatalf("Rotate() = %+v", rotated)
	}
	if replacement.SessionID != "" {
		t.Fatal("Rotate() mutated caller-owned replacement")
	}

	if _, err := store.Rotate(t.Context(), user.RotateSessionInput{
		Digest: first.Digest, Now: now.Add(2 * time.Minute),
		NewToken: &user.RefreshToken{ID: strings.Repeat("c", 22), Digest: strings.Repeat("3", 64), ExpiresAt: session.ExpiresAt},
	}); !errors.Is(err, user.ErrRefreshReuse) {
		t.Fatalf("reused Rotate() error = %v, want ErrRefreshReuse", err)
	}
	if _, err := store.Active(t.Context(), session.ID, account.ID, now.Add(3*time.Minute)); !errors.Is(err, user.ErrSessionRevoked) {
		t.Fatalf("Active() after reuse error = %v, want ErrSessionRevoked", err)
	}

	q := query.Use(db)
	persistedSession, err := q.Session.WithContext(t.Context()).Where(q.Session.ID.Eq(session.ID)).First()
	if err != nil || persistedSession.RevokedAt == nil || persistedSession.ReuseDetectedAt == nil {
		t.Fatalf("persisted session after reuse = (%+v, %v)", persistedSession, err)
	}
	persistedReplacement, err := q.RefreshToken.WithContext(t.Context()).Where(q.RefreshToken.ID.Eq(strings.Repeat("b", 22))).First()
	if err != nil || persistedReplacement.RevokedAt == nil {
		t.Fatalf("replacement after reuse = (%+v, %v)", persistedReplacement, err)
	}
}

func TestSessionStoreLogoutAndExpiry(t *testing.T) {
	db := newTestDB(t)
	users := mustUserStore(t, db)
	account := createStoredUser(t, users, "logout-user@example.com", user.RoleCustomer)
	store := mustSessionStore(t, db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	session := &user.Session{ID: strings.Repeat("l", 22), UserID: account.ID, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	token := &user.RefreshToken{ID: strings.Repeat("t", 22), SessionID: session.ID, Digest: strings.Repeat("4", 64), ExpiresAt: session.ExpiresAt, CreatedAt: now, UpdatedAt: now}
	if err := store.Create(t.Context(), session, token); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := store.Revoke(t.Context(), session.ID, account.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if err := store.Revoke(t.Context(), session.ID, account.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("idempotent Revoke() error = %v", err)
	}
	if _, err := store.Active(t.Context(), session.ID, account.ID, now.Add(3*time.Minute)); !errors.Is(err, user.ErrSessionRevoked) {
		t.Fatalf("Active() after logout error = %v", err)
	}

	expired := &user.Session{ID: strings.Repeat("e", 22), UserID: account.ID, ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	expiredToken := &user.RefreshToken{ID: strings.Repeat("x", 22), SessionID: expired.ID, Digest: strings.Repeat("5", 64), ExpiresAt: expired.ExpiresAt, CreatedAt: expired.CreatedAt, UpdatedAt: expired.UpdatedAt}
	if err := store.Create(t.Context(), expired, expiredToken); err != nil {
		t.Fatalf("Create(expired) error = %v", err)
	}
	if _, err := store.Active(t.Context(), expired.ID, account.ID, now); !errors.Is(err, user.ErrSessionExpired) {
		t.Fatalf("Active(expired) error = %v, want ErrSessionExpired", err)
	}
	if _, err := store.Rotate(t.Context(), user.RotateSessionInput{
		Digest: expiredToken.Digest, Now: now,
		NewToken: &user.RefreshToken{ID: strings.Repeat("n", 22), Digest: strings.Repeat("6", 64), ExpiresAt: now.Add(time.Hour)},
	}); !errors.Is(err, user.ErrSessionExpired) {
		t.Fatalf("Rotate(expired) error = %v, want ErrSessionExpired", err)
	}
}

func TestSessionStoreConcurrentRefreshDetectsReuseAndRevokesFamily(t *testing.T) {
	db := newTestDB(t)
	users := mustUserStore(t, db)
	account := createStoredUser(t, users, "concurrent-session@example.com", user.RoleCustomer)
	store := mustSessionStore(t, db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	session := &user.Session{ID: strings.Repeat("q", 22), UserID: account.ID, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	first := &user.RefreshToken{ID: strings.Repeat("r", 22), SessionID: session.ID, Digest: strings.Repeat("7", 64), ExpiresAt: session.ExpiresAt, CreatedAt: now, UpdatedAt: now}
	if err := store.Create(t.Context(), session, first); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	start := make(chan struct{})
	var ready, workers sync.WaitGroup
	ready.Add(2)
	workers.Add(2)
	errs := make([]error, 2)
	for index := range 2 {
		go func() {
			defer workers.Done()
			ready.Done()
			<-start
			character := byte('u' + index)
			_, errs[index] = store.Rotate(t.Context(), user.RotateSessionInput{
				Digest: first.Digest, Now: now.Add(time.Minute),
				NewToken: &user.RefreshToken{
					ID: strings.Repeat(string(character), 22), Digest: strings.Repeat(string(byte('8'+index)), 64),
					ExpiresAt: session.ExpiresAt, CreatedAt: now, UpdatedAt: now,
				},
			})
		}()
	}
	ready.Wait()
	close(start)
	workers.Wait()

	successes, reuses := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, user.ErrRefreshReuse):
			reuses++
		default:
			t.Fatalf("concurrent Rotate() error = %v", err)
		}
	}
	if successes != 1 || reuses != 1 {
		t.Fatalf("concurrent Rotate() = success:%d reuse:%d errors:%v", successes, reuses, errs)
	}
	if _, err := store.Active(t.Context(), session.ID, account.ID, now.Add(2*time.Minute)); !errors.Is(err, user.ErrSessionRevoked) {
		t.Fatalf("Active() after concurrent reuse error = %v", err)
	}
}

func mustSessionStore(t *testing.T, db *gorm.DB) *SessionStore {
	t.Helper()
	store, err := NewSessionStore(db)
	if err != nil {
		t.Fatalf("NewSessionStore() error = %v", err)
	}
	return store
}

package mysqlstore

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"clean-arch-gin/mysqlstore/model"
	"clean-arch-gin/mysqlstore/query"
	"clean-arch-gin/user"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SessionStore struct {
	db *gorm.DB
	q  *query.Query
}

var _ user.SessionStore = (*SessionStore)(nil)

func NewSessionStore(db *gorm.DB) (*SessionStore, error) {
	if db == nil {
		return nil, errors.New("new MySQL session store: database is required")
	}
	return &SessionStore{db: db, q: query.Use(db)}, nil
}

func (s *SessionStore) Create(ctx context.Context, session *user.Session, token *user.RefreshToken) error {
	if ctx == nil {
		return errors.New("create session: context is required")
	}
	if session == nil || token == nil || session.ID == "" || session.UserID == 0 || token.ID == "" || token.SessionID != session.ID {
		return user.ErrInvalidRefresh
	}
	digest, err := decodeRefreshDigest(token.Digest)
	if err != nil {
		return err
	}
	persistedSession := sessionToModel(session)
	persistedToken := refreshToModel(token, digest)
	err = query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		if err := tx.Session.WithContext(ctx).Create(persistedSession); err != nil {
			return fmt.Errorf("create session: insert session: %w", err)
		}
		if err := tx.RefreshToken.WithContext(ctx).Create(persistedToken); err != nil {
			return mapRefreshWriteError("create session: insert refresh", err)
		}
		return nil
	})
	return err
}

func (s *SessionStore) Active(ctx context.Context, sessionID string, userID uint64, now time.Time) (*user.Session, error) {
	if ctx == nil {
		return nil, errors.New("load active session: context is required")
	}
	row, err := s.q.Session.WithContext(ctx).
		Where(s.q.Session.ID.Eq(sessionID), s.q.Session.UserID.Eq(userID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, user.ErrNotFound
		}
		return nil, fmt.Errorf("load active session: %w", err)
	}
	if row.RevokedAt != nil {
		return nil, user.ErrSessionRevoked
	}
	if !now.UTC().Before(row.ExpiresAt.UTC()) {
		return nil, user.ErrSessionExpired
	}
	return modelToSession(row), nil
}

func (s *SessionStore) Rotate(ctx context.Context, input user.RotateSessionInput) (user.RotateSessionResult, error) {
	if ctx == nil {
		return user.RotateSessionResult{}, errors.New("rotate session: context is required")
	}
	presentedDigest, err := decodeRefreshDigest(input.Digest)
	if err != nil {
		return user.RotateSessionResult{}, err
	}
	if input.NewToken == nil || input.NewToken.ID == "" {
		return user.RotateSessionResult{}, user.ErrInvalidRefresh
	}
	replacementDigest, err := decodeRefreshDigest(input.NewToken.Digest)
	if err != nil {
		return user.RotateSessionResult{}, err
	}
	now := input.Now.UTC()

	base := query.Use(s.db.WithContext(ctx))
	tx := base.Begin()
	if tx.Error != nil {
		return user.RotateSessionResult{}, fmt.Errorf("rotate session: begin transaction: %w", tx.Error)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, err := tx.RefreshToken.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(tx.RefreshToken.Digest.Eq(presentedDigest)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return user.RotateSessionResult{}, user.ErrInvalidRefresh
		}
		return user.RotateSessionResult{}, fmt.Errorf("rotate session: lock refresh token: %w", err)
	}
	// The unique binary lookup already narrows the row, while an explicit
	// constant-time comparison keeps credential equality at this trust boundary
	// independent of driver/database comparison behavior.
	if !refreshDigestMatches(current.Digest, presentedDigest) {
		return user.RotateSessionResult{}, user.ErrInvalidRefresh
	}
	session, err := tx.Session.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(tx.Session.ID.Eq(current.SessionID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return user.RotateSessionResult{}, user.ErrInvalidRefresh
		}
		return user.RotateSessionResult{}, fmt.Errorf("rotate session: lock session: %w", err)
	}

	if current.ConsumedAt != nil {
		if err := persistRefreshReuse(ctx, tx.Query, session, now); err != nil {
			return user.RotateSessionResult{}, err
		}
		// Reuse is a successful security-state transition. Returning the sentinel
		// before Commit would roll back the family revocation and reopen the session.
		if err := tx.Commit(); err != nil {
			return user.RotateSessionResult{}, fmt.Errorf("rotate session: commit reuse revocation: %w", err)
		}
		committed = true
		return user.RotateSessionResult{}, user.ErrRefreshReuse
	}
	if current.RevokedAt != nil || session.RevokedAt != nil {
		return user.RotateSessionResult{}, user.ErrSessionRevoked
	}
	if !now.Before(session.ExpiresAt.UTC()) {
		return user.RotateSessionResult{}, user.ErrSessionExpired
	}
	if !now.Before(current.ExpiresAt.UTC()) {
		return user.RotateSessionResult{}, user.ErrInvalidRefresh
	}

	expiresAt := input.NewToken.ExpiresAt.UTC()
	if expiresAt.After(session.ExpiresAt.UTC()) {
		expiresAt = session.ExpiresAt.UTC()
	}
	if !now.Before(expiresAt) {
		return user.RotateSessionResult{}, user.ErrInvalidRefresh
	}
	replacement := &model.RefreshToken{
		ID: input.NewToken.ID, SessionID: session.ID, Digest: replacementDigest,
		ExpiresAt: expiresAt, CreatedAt: normalizedTokenTime(input.NewToken.CreatedAt, now),
		UpdatedAt: normalizedTokenTime(input.NewToken.UpdatedAt, now),
	}
	// The schema's replaced_by foreign key requires the replacement row to exist
	// before linking the consumed token. Both writes remain invisible until commit.
	if err := tx.RefreshToken.WithContext(ctx).Create(replacement); err != nil {
		return user.RotateSessionResult{}, mapRefreshWriteError("rotate session: insert replacement", err)
	}
	updatedCurrent, err := tx.RefreshToken.WithContext(ctx).
		Where(tx.RefreshToken.ID.Eq(current.ID), tx.RefreshToken.ConsumedAt.IsNull()).
		UpdateSimple(
			tx.RefreshToken.ConsumedAt.Value(now),
			tx.RefreshToken.ReplacedByID.Value(replacement.ID),
			tx.RefreshToken.UpdatedAt.Value(now),
		)
	if err != nil {
		return user.RotateSessionResult{}, fmt.Errorf("rotate session: consume refresh token: %w", err)
	}
	if updatedCurrent.RowsAffected != 1 {
		return user.RotateSessionResult{}, user.ErrInvalidRefresh
	}
	updatedSession, err := tx.Session.WithContext(ctx).
		Where(tx.Session.ID.Eq(session.ID)).
		UpdateSimple(tx.Session.Rotation.Add(1), tx.Session.UpdatedAt.Value(now))
	if err != nil {
		return user.RotateSessionResult{}, fmt.Errorf("rotate session: increment rotation: %w", err)
	}
	if updatedSession.RowsAffected != 1 {
		return user.RotateSessionResult{}, user.ErrInvalidRefresh
	}
	if err := tx.Commit(); err != nil {
		return user.RotateSessionResult{}, fmt.Errorf("rotate session: commit: %w", err)
	}
	committed = true

	session.Rotation++
	session.UpdatedAt = now
	return user.RotateSessionResult{
		Session:      modelToSession(session),
		RefreshToken: modelToRefresh(replacement),
	}, nil
}

func (s *SessionStore) Revoke(ctx context.Context, sessionID string, userID uint64, now time.Time) error {
	if ctx == nil {
		return errors.New("revoke session: context is required")
	}
	now = now.UTC()
	return query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		// Token rows are locked before the session in ID order, matching Rotate's
		// token-before-session order and preventing a logout/refresh deadlock cycle.
		if _, err := tx.RefreshToken.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(tx.RefreshToken.SessionID.Eq(sessionID)).
			Order(tx.RefreshToken.ID.Asc()).Find(); err != nil {
			return fmt.Errorf("revoke session: lock refresh family: %w", err)
		}
		session, err := tx.Session.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(tx.Session.ID.Eq(sessionID), tx.Session.UserID.Eq(userID)).First()
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return user.ErrNotFound
			}
			return fmt.Errorf("revoke session: lock session: %w", err)
		}
		if session.RevokedAt == nil {
			if _, err := tx.Session.WithContext(ctx).Where(tx.Session.ID.Eq(sessionID)).
				UpdateSimple(tx.Session.RevokedAt.Value(now), tx.Session.UpdatedAt.Value(now)); err != nil {
				return fmt.Errorf("revoke session: mark session: %w", err)
			}
		}
		if _, err := tx.RefreshToken.WithContext(ctx).
			Where(
				tx.RefreshToken.SessionID.Eq(sessionID),
				tx.RefreshToken.ConsumedAt.IsNull(),
				tx.RefreshToken.RevokedAt.IsNull(),
			).
			UpdateSimple(tx.RefreshToken.RevokedAt.Value(now), tx.RefreshToken.UpdatedAt.Value(now)); err != nil {
			return fmt.Errorf("revoke session: revoke refresh family: %w", err)
		}
		return nil
	})
}

func persistRefreshReuse(ctx context.Context, tx *query.Query, session *model.Session, now time.Time) error {
	assignments := []field.AssignExpr{tx.Session.UpdatedAt.Value(now)}
	if session.ReuseDetectedAt == nil {
		assignments = append(assignments, tx.Session.ReuseDetectedAt.Value(now))
	}
	if session.RevokedAt == nil {
		assignments = append(assignments, tx.Session.RevokedAt.Value(now))
	}
	if _, err := tx.Session.WithContext(ctx).Where(tx.Session.ID.Eq(session.ID)).UpdateSimple(assignments...); err != nil {
		return fmt.Errorf("rotate session: persist reuse detection: %w", err)
	}
	if _, err := tx.RefreshToken.WithContext(ctx).
		Where(
			tx.RefreshToken.SessionID.Eq(session.ID),
			tx.RefreshToken.ConsumedAt.IsNull(),
			tx.RefreshToken.RevokedAt.IsNull(),
		).
		UpdateSimple(tx.RefreshToken.RevokedAt.Value(now), tx.RefreshToken.UpdatedAt.Value(now)); err != nil {
		return fmt.Errorf("rotate session: revoke reused family: %w", err)
	}
	return nil
}

func refreshDigestMatches(stored, presented []byte) bool {
	if len(stored) != 32 || len(presented) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare(stored, presented) == 1
}

func decodeRefreshDigest(value string) ([]byte, error) {
	if len(value) != 64 || value != strings.ToLower(value) {
		return nil, user.ErrInvalidRefresh
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != value {
		// Never wrap the decoder error or echo value: both are derived from bearer
		// material and do not belong in logs or transport error bodies.
		return nil, user.ErrInvalidRefresh
	}
	return decoded, nil
}

func sessionToModel(session *user.Session) *model.Session {
	return &model.Session{
		ID: session.ID, UserID: session.UserID, Rotation: session.Rotation,
		ExpiresAt: session.ExpiresAt.UTC(), RevokedAt: utcTimePointer(session.RevokedAt),
		ReuseDetectedAt: utcTimePointer(session.ReuseDetectedAt),
		CreatedAt:       session.CreatedAt.UTC(), UpdatedAt: session.UpdatedAt.UTC(),
	}
}

func refreshToModel(token *user.RefreshToken, digest []byte) *model.RefreshToken {
	var replacedBy *string
	if token.ReplacedByID != "" {
		value := token.ReplacedByID
		replacedBy = &value
	}
	return &model.RefreshToken{
		ID: token.ID, SessionID: token.SessionID, Digest: digest, ExpiresAt: token.ExpiresAt.UTC(),
		ConsumedAt: utcTimePointer(token.ConsumedAt), RevokedAt: utcTimePointer(token.RevokedAt),
		ReplacedByID: replacedBy, CreatedAt: token.CreatedAt.UTC(), UpdatedAt: token.UpdatedAt.UTC(),
	}
}

func modelToSession(session *model.Session) *user.Session {
	if session == nil {
		return nil
	}
	return &user.Session{
		ID: session.ID, UserID: session.UserID, Rotation: session.Rotation,
		ExpiresAt: session.ExpiresAt.UTC(), RevokedAt: utcTimePointer(session.RevokedAt),
		ReuseDetectedAt: utcTimePointer(session.ReuseDetectedAt),
		CreatedAt:       session.CreatedAt.UTC(), UpdatedAt: session.UpdatedAt.UTC(),
	}
}

func modelToRefresh(token *model.RefreshToken) *user.RefreshToken {
	if token == nil {
		return nil
	}
	replacedBy := ""
	if token.ReplacedByID != nil {
		replacedBy = *token.ReplacedByID
	}
	return &user.RefreshToken{
		ID: token.ID, SessionID: token.SessionID, Digest: hex.EncodeToString(token.Digest),
		ExpiresAt: token.ExpiresAt.UTC(), ConsumedAt: utcTimePointer(token.ConsumedAt),
		RevokedAt: utcTimePointer(token.RevokedAt), ReplacedByID: replacedBy,
		CreatedAt: token.CreatedAt.UTC(), UpdatedAt: token.UpdatedAt.UTC(),
	}
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func normalizedTokenTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value.UTC()
}

func mapRefreshWriteError(operation string, err error) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		if mysqlDuplicateKeyName(mysqlErr.Message) == "uk_refresh_tokens_digest" {
			// Duplicate digest messages contain credential-derived bytes; collapse them
			// to the stable invalid-refresh category without retaining driver text.
			return fmt.Errorf("%s: %w", operation, user.ErrInvalidRefresh)
		}
		// Other duplicate constraints signal an unexpected storage write failure.
		// Keep the driver error available to errors.As while redacting duplicate values.
		return &redactedStorageError{operation: operation + ": duplicate database constraint", cause: err}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

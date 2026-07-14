package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

type UserStore struct {
	db *gorm.DB
	q  *query.Query
}

var (
	_ user.Store         = (*UserStore)(nil)
	_ user.AuthUserStore = (*UserStore)(nil)
)

func NewUserStore(db *gorm.DB) (*UserStore, error) {
	if db == nil {
		return nil, errors.New("new MySQL user store: database is required")
	}
	return &UserStore{db: db, q: query.Use(db)}, nil
}

func (s *UserStore) CreateWithRole(ctx context.Context, account *user.User, roleName string) error {
	if ctx == nil {
		return errors.New("create user with role: context is required")
	}
	if account == nil {
		return errors.New("create user with role: user is required")
	}
	roleName = normalizeRoleName(roleName)
	if roleName == "" {
		return user.ErrRoleNotFound
	}

	persisted := userToModel(account)
	err := query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		role, err := tx.Role.WithContext(ctx).Where(tx.Role.Name.Eq(roleName)).First()
		if err != nil {
			return mapRoleLookupError("create user with role", err)
		}
		if err := tx.User.WithContext(ctx).Create(persisted); err != nil {
			return mapCreateUserError(err)
		}
		mapping := &model.UserRole{UserID: persisted.ID, RoleID: role.ID, AssignedAt: time.Now().UTC()}
		if err := tx.UserRole.WithContext(ctx).Create(mapping); err != nil {
			return fmt.Errorf("create user with role: assign role: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	loaded, err := s.ByID(ctx, persisted.ID)
	if err != nil {
		return fmt.Errorf("create user with role: reload: %w", err)
	}
	*account = *loaded
	return nil
}

func (s *UserStore) ByID(ctx context.Context, userID uint64) (*user.User, error) {
	if ctx == nil {
		return nil, errors.New("find user by ID: context is required")
	}
	row, err := s.q.User.WithContext(ctx).Where(s.q.User.ID.Eq(userID)).First()
	if err != nil {
		return nil, mapUserLookupError("find user by ID", err)
	}
	return s.hydrateOne(ctx, row)
}

func (s *UserStore) ByEmail(ctx context.Context, email string) (*user.User, error) {
	if ctx == nil {
		return nil, errors.New("find user by email: context is required")
	}
	row, err := s.q.User.WithContext(ctx).Where(s.q.User.Email.Eq(email)).First()
	if err != nil {
		return nil, mapUserLookupError("find user by email", err)
	}
	return s.hydrateOne(ctx, row)
}

func (s *UserStore) UpdateProfile(ctx context.Context, account *user.User, expectedVersion uint64) (*user.User, error) {
	if ctx == nil {
		return nil, errors.New("update user profile: context is required")
	}
	if account == nil {
		return nil, errors.New("update user profile: user is required")
	}
	now := time.Now().UTC()
	err := query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		result, err := tx.User.WithContext(ctx).
			Where(tx.User.ID.Eq(account.ID), tx.User.Version.Eq(expectedVersion)).
			UpdateSimple(
				tx.User.DisplayName.Value(account.Name),
				tx.User.Version.Add(1),
				tx.User.UpdatedAt.Value(now),
			)
		if err != nil {
			return fmt.Errorf("update user profile: %w", err)
		}
		if result.RowsAffected != 0 {
			return nil
		}
		exists, err := userExists(ctx, tx, account.ID)
		if err != nil {
			return fmt.Errorf("update user profile: inspect update miss: %w", err)
		}
		return conditionalUpdateMissError(exists)
	})
	if err != nil {
		return nil, err
	}
	return s.ByID(ctx, account.ID)
}

func (s *UserStore) UpdatePasswordHash(ctx context.Context, userID uint64, hash string, expectedVersion uint64) (*user.User, error) {
	if ctx == nil {
		return nil, errors.New("update password hash: context is required")
	}
	now := time.Now().UTC()
	err := query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		result, err := tx.User.WithContext(ctx).
			Where(tx.User.ID.Eq(userID), tx.User.Version.Eq(expectedVersion)).
			UpdateSimple(
				tx.User.PasswordHash.Value(hash),
				tx.User.Version.Add(1),
				tx.User.UpdatedAt.Value(now),
			)
		if err != nil {
			return fmt.Errorf("update password hash: %w", err)
		}
		if result.RowsAffected != 0 {
			return nil
		}
		exists, err := userExists(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("update password hash: inspect update miss: %w", err)
		}
		return conditionalUpdateMissError(exists)
	})
	if err != nil {
		return nil, err
	}
	return s.ByID(ctx, userID)
}

func userExists(ctx context.Context, tx *query.Query, userID uint64) (bool, error) {
	// Physical deletion is not part of the account model, so checking existence in
	// the failed update's transaction cleanly separates a stale version from a
	// caller referring to an account that never existed.
	count, err := tx.User.WithContext(ctx).Where(tx.User.ID.Eq(userID)).Count()
	return count > 0, err
}

func conditionalUpdateMissError(exists bool) error {
	if exists {
		return user.ErrConflict
	}
	return user.ErrNotFound
}

func (s *UserStore) List(ctx context.Context, filter user.ListFilter) (user.Page, error) {
	if ctx == nil {
		return user.Page{}, errors.New("list users: context is required")
	}
	if err := filter.Validate(); err != nil {
		return user.Page{}, fmt.Errorf("list users: %w", err)
	}

	dao := s.q.User.WithContext(ctx)
	if filter.Email != "" {
		dao = dao.Where(s.q.User.Email.Like("%" + filter.Email + "%"))
	}
	if filter.Name != "" {
		dao = dao.Where(s.q.User.DisplayName.Like("%" + filter.Name + "%"))
	}
	if filter.Status != "" {
		dao = dao.Where(s.q.User.Status.Eq(string(filter.Status)))
	}
	if filter.Role != "" {
		// The joined tables and columns are compile-time constants. Values remain
		// bound parameters, so filtering by role cannot introduce SQL identifiers.
		dao = dao.
			Join(model.UserRole{}, s.q.User.ID.EqCol(s.q.UserRole.UserID)).
			Join(model.Role{}, s.q.UserRole.RoleID.EqCol(s.q.Role.ID)).
			Where(s.q.Role.Name.Eq(filter.Role))
	}

	total, err := dao.Count()
	if err != nil {
		return user.Page{}, fmt.Errorf("list users: count: %w", err)
	}
	var order field.Expr
	switch filter.Sort {
	case "id":
		order = s.q.User.ID
	case "email":
		order = s.q.User.Email
	case "name":
		order = s.q.User.DisplayName
	case "status":
		order = s.q.User.Status
	case "created_at":
		order = s.q.User.CreatedAt
	case "updated_at":
		order = s.q.User.UpdatedAt
	default:
		return user.Page{}, fmt.Errorf("list users: %w", user.ErrInvalidFilter)
	}
	if filter.Descending {
		order = order.Desc()
	} else {
		order = order.Asc()
	}
	dao = dao.Order(order)
	if filter.Sort != "id" {
		dao = dao.Order(s.q.User.ID.Asc())
	}
	rows, err := dao.Offset(filter.Offset).Limit(filter.Limit).Find()
	if err != nil {
		return user.Page{}, fmt.Errorf("list users: query page: %w", err)
	}
	items, err := s.hydrateMany(ctx, rows)
	if err != nil {
		return user.Page{}, fmt.Errorf("list users: %w", err)
	}
	return user.Page{Items: items, Total: uint64(total), Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s *UserStore) RoleNamesExist(ctx context.Context, names []string) (bool, error) {
	if ctx == nil {
		return false, errors.New("check role names: context is required")
	}
	names = normalizedRoleNames(names)
	if len(names) == 0 {
		return false, nil
	}
	count, err := s.q.Role.WithContext(ctx).Where(s.q.Role.Name.In(names...)).Count()
	if err != nil {
		return false, fmt.Errorf("check role names: %w", err)
	}
	return count == int64(len(names)), nil
}

func (s *UserStore) Permissions(ctx context.Context, userID uint64) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("load user permissions: context is required")
	}
	var names []string
	err := s.q.Permission.WithContext(ctx).
		Join(model.RolePermission{}, s.q.Permission.ID.EqCol(s.q.RolePermission.PermissionID)).
		Join(model.UserRole{}, s.q.RolePermission.RoleID.EqCol(s.q.UserRole.RoleID)).
		Where(s.q.UserRole.UserID.Eq(userID)).
		Distinct(s.q.Permission.Name).
		Order(s.q.Permission.Name.Asc()).
		Pluck(s.q.Permission.Name, &names)
	if err != nil {
		return nil, fmt.Errorf("load user permissions: %w", err)
	}
	return names, nil
}

func (s *UserStore) SetStatus(ctx context.Context, userID uint64, status user.Status, expectedVersion uint64) error {
	if ctx == nil {
		return errors.New("set user status: context is required")
	}
	if status != user.StatusActive && status != user.StatusDisabled {
		return user.ErrInvalidStatus
	}
	return query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		adminRole, target, err := lockAdminGuardAndUser(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("set user status: %w", err)
		}
		if target.Status == string(user.StatusActive) && status != user.StatusActive {
			admin, err := userHasRole(ctx, tx, userID, adminRole.ID)
			if err != nil {
				return fmt.Errorf("set user status: inspect roles: %w", err)
			}
			if admin {
				count, err := activeAdminCount(ctx, tx, adminRole.ID)
				if err != nil {
					return fmt.Errorf("set user status: count active admins: %w", err)
				}
				if count <= 1 {
					return user.ErrLastAdmin
				}
			}
		}
		result, err := tx.User.WithContext(ctx).
			Where(tx.User.ID.Eq(userID), tx.User.Version.Eq(expectedVersion)).
			UpdateSimple(tx.User.Status.Value(string(status)), tx.User.Version.Add(1), tx.User.UpdatedAt.Value(time.Now().UTC()))
		if err != nil {
			return fmt.Errorf("set user status: update: %w", err)
		}
		if result.RowsAffected == 0 {
			return user.ErrConflict
		}
		return nil
	})
}

func (s *UserStore) ReplaceRoles(ctx context.Context, userID uint64, names []string, expectedVersion uint64) error {
	if ctx == nil {
		return errors.New("replace user roles: context is required")
	}
	names = normalizedRoleNames(names)
	if len(names) == 0 {
		return user.ErrRoleNotFound
	}
	return query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		adminRole, target, err := lockAdminGuardAndUser(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("replace user roles: %w", err)
		}
		roles, err := tx.Role.WithContext(ctx).Where(tx.Role.Name.In(names...)).Order(tx.Role.Name.Asc()).Find()
		if err != nil {
			return fmt.Errorf("replace user roles: load roles: %w", err)
		}
		if len(roles) != len(names) {
			return user.ErrRoleNotFound
		}

		removesAdmin := !slices.Contains(names, user.RoleAdmin)
		if target.Status == string(user.StatusActive) && removesAdmin {
			admin, err := userHasRole(ctx, tx, userID, adminRole.ID)
			if err != nil {
				return fmt.Errorf("replace user roles: inspect roles: %w", err)
			}
			if admin {
				count, err := activeAdminCount(ctx, tx, adminRole.ID)
				if err != nil {
					return fmt.Errorf("replace user roles: count active admins: %w", err)
				}
				if count <= 1 {
					return user.ErrLastAdmin
				}
			}
		}

		// Incrementing the account version before changing mappings makes the caller's
		// optimistic expectation govern the entire role replacement transaction.
		result, err := tx.User.WithContext(ctx).
			Where(tx.User.ID.Eq(userID), tx.User.Version.Eq(expectedVersion)).
			UpdateSimple(tx.User.Version.Add(1), tx.User.UpdatedAt.Value(time.Now().UTC()))
		if err != nil {
			return fmt.Errorf("replace user roles: update version: %w", err)
		}
		if result.RowsAffected == 0 {
			return user.ErrConflict
		}
		if _, err := tx.UserRole.WithContext(ctx).Where(tx.UserRole.UserID.Eq(userID)).Delete(); err != nil {
			return fmt.Errorf("replace user roles: clear mappings: %w", err)
		}
		now := time.Now().UTC()
		mappings := make([]*model.UserRole, 0, len(roles))
		for _, role := range roles {
			mappings = append(mappings, &model.UserRole{UserID: userID, RoleID: role.ID, AssignedAt: now})
		}
		if err := tx.UserRole.WithContext(ctx).Create(mappings...); err != nil {
			return fmt.Errorf("replace user roles: insert mappings: %w", err)
		}
		return nil
	})
}

func (s *UserStore) IsLastActiveAdmin(ctx context.Context, userID uint64) (bool, error) {
	if ctx == nil {
		return false, errors.New("inspect last active admin: context is required")
	}
	var last bool
	err := query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		adminRole, target, err := lockAdminGuardAndUser(ctx, tx, userID)
		if err != nil {
			return fmt.Errorf("inspect last active admin: %w", err)
		}
		admin, err := userHasRole(ctx, tx, userID, adminRole.ID)
		if err != nil {
			return fmt.Errorf("inspect last active admin: roles: %w", err)
		}
		if target.Status != string(user.StatusActive) || !admin {
			last = false
			return nil
		}
		count, err := activeAdminCount(ctx, tx, adminRole.ID)
		if err != nil {
			return fmt.Errorf("inspect last active admin: count: %w", err)
		}
		last = count <= 1
		return nil
	})
	if err != nil {
		return false, err
	}
	return last, nil
}

func lockAdminGuardAndUser(ctx context.Context, tx *query.Query, userID uint64) (*model.Role, *model.User, error) {
	// Both authority-reducing writes lock this stable row first. The shared order
	// serializes their active-admin recounts and prevents cross-operation write-skew.
	adminRole, err := tx.Role.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(tx.Role.Name.Eq(user.RoleAdmin)).First()
	if err != nil {
		return nil, nil, mapRoleLookupError("lock admin guard", err)
	}
	target, err := tx.User.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(tx.User.ID.Eq(userID)).First()
	if err != nil {
		return nil, nil, mapUserLookupError("lock target user", err)
	}
	return adminRole, target, nil
}

func userHasRole(ctx context.Context, q *query.Query, userID, roleID uint64) (bool, error) {
	_, err := q.UserRole.WithContext(ctx).
		Where(q.UserRole.UserID.Eq(userID), q.UserRole.RoleID.Eq(roleID)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func activeAdminCount(ctx context.Context, q *query.Query, adminRoleID uint64) (int64, error) {
	return q.User.WithContext(ctx).
		Join(model.UserRole{}, q.User.ID.EqCol(q.UserRole.UserID)).
		Where(q.User.Status.Eq(string(user.StatusActive)), q.UserRole.RoleID.Eq(adminRoleID)).Count()
}

type rolePermissionRow struct {
	UserID         uint64
	RoleID         uint64
	RoleName       string
	PermissionID   *uint64
	PermissionName *string
}

func (s *UserStore) hydrateOne(ctx context.Context, row *model.User) (*user.User, error) {
	items, err := s.hydrateMany(ctx, []*model.User{row})
	if err != nil {
		return nil, err
	}
	return &items[0], nil
}

func (s *UserStore) hydrateMany(ctx context.Context, rows []*model.User) ([]user.User, error) {
	items := make([]user.User, len(rows))
	ids := make([]uint64, len(rows))
	for index, row := range rows {
		items[index] = modelToUser(row)
		ids[index] = row.ID
	}
	if len(ids) == 0 {
		return items, nil
	}

	var joined []rolePermissionRow
	// GORM Gen cannot materialize the nested many-to-many graph directly, so the
	// typed joins are scanned into flat rows and folded below. One query hydrates
	// the whole page while every table, field, and ordering expression stays fixed.
	err := s.q.UserRole.WithContext(ctx).
		Select(
			s.q.UserRole.UserID,
			s.q.Role.ID.As("role_id"),
			s.q.Role.Name.As("role_name"),
			s.q.Permission.ID.As("permission_id"),
			s.q.Permission.Name.As("permission_name"),
		).
		Join(model.Role{}, s.q.UserRole.RoleID.EqCol(s.q.Role.ID)).
		LeftJoin(model.RolePermission{}, s.q.Role.ID.EqCol(s.q.RolePermission.RoleID)).
		LeftJoin(model.Permission{}, s.q.RolePermission.PermissionID.EqCol(s.q.Permission.ID)).
		Where(s.q.UserRole.UserID.In(ids...)).
		Order(
			s.q.UserRole.UserID.Asc(), s.q.Role.Name.Asc(), s.q.Role.ID.Asc(),
			s.q.Permission.Name.Asc(), s.q.Permission.ID.Asc(),
		).
		Scan(&joined)
	if err != nil {
		return nil, fmt.Errorf("load user roles and permissions: %w", err)
	}

	byID := make(map[uint64]*user.User, len(items))
	for index := range items {
		byID[items[index].ID] = &items[index]
	}
	var previousUserID, previousRoleID uint64
	for _, joinedRow := range joined {
		account := byID[joinedRow.UserID]
		if account == nil {
			continue
		}
		if joinedRow.UserID != previousUserID || joinedRow.RoleID != previousRoleID {
			account.Roles = append(account.Roles, user.Role{ID: joinedRow.RoleID, Name: joinedRow.RoleName})
			previousUserID, previousRoleID = joinedRow.UserID, joinedRow.RoleID
		}
		if joinedRow.PermissionID != nil && joinedRow.PermissionName != nil {
			last := len(account.Roles) - 1
			account.Roles[last].Permissions = append(account.Roles[last].Permissions, user.Permission{ID: *joinedRow.PermissionID, Name: *joinedRow.PermissionName})
		}
	}
	return items, nil
}

func userToModel(account *user.User) *model.User {
	return &model.User{
		ID: account.ID, Email: account.Email, DisplayName: account.Name, PasswordHash: account.PasswordHash,
		Status: string(account.Status), Version: account.Version,
		CreatedAt: account.CreatedAt.UTC(), UpdatedAt: account.UpdatedAt.UTC(),
	}
}

func modelToUser(row *model.User) user.User {
	return user.User{
		ID: row.ID, Email: row.Email, Name: row.DisplayName, PasswordHash: row.PasswordHash,
		Status: user.Status(row.Status), Version: row.Version,
		CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
}

func normalizedRoleNames(names []string) []string {
	normalized := make([]string, 0, len(names))
	for _, name := range names {
		// Retain normalized-empty input so the subsequent exact count cannot report
		// that every requested role exists after silently discarding an invalid name.
		normalized = append(normalized, normalizeRoleName(name))
	}
	slices.Sort(normalized)
	return slices.Compact(normalized)
}

func normalizeRoleName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func mapUserLookupError(operation string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s: %w", operation, user.ErrNotFound)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func mapRoleLookupError(operation string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s: %w", operation, user.ErrRoleNotFound)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func mapCreateUserError(err error) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		if mysqlDuplicateKeyName(mysqlErr.Message) == "uk_users_email" {
			// Driver duplicate messages include the conflicting email; return only the
			// stable domain category so storage failures do not disclose account input.
			return fmt.Errorf("create user with role: %w", user.ErrEmailExists)
		}
		// Other duplicate constraints indicate a storage/schema problem rather than
		// an email conflict. Preserve errors.As without exposing the duplicate value.
		return &redactedStorageError{operation: "create user with role: duplicate database constraint", cause: err}
	}
	return fmt.Errorf("create user with role: insert user: %w", err)
}

func mysqlDuplicateKeyName(message string) string {
	const marker = " for key "
	index := strings.LastIndex(message, marker)
	if index < 0 {
		return ""
	}
	raw := strings.TrimSpace(message[index+len(marker):])
	if len(raw) < 2 || (raw[0] != '\'' && raw[0] != '`') {
		return ""
	}
	quote := raw[0]
	end := strings.IndexByte(raw[1:], quote)
	if end < 0 {
		return ""
	}
	key := raw[1 : end+1]
	if qualifier := strings.LastIndexByte(key, '.'); qualifier >= 0 {
		key = key[qualifier+1:]
	}
	return key
}

type redactedStorageError struct {
	operation string
	cause     error
}

func (e *redactedStorageError) Error() string { return e.operation }
func (e *redactedStorageError) Unwrap() error { return e.cause }

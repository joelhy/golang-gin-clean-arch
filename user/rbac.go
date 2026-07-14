package user

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

func (a Actor) HasPermission(permission string) bool {
	// Capabilities are exact stable names: prefix and wildcard matching would let a
	// narrower grant accidentally authorize future, more privileged operations.
	if permission == "" {
		return false
	}
	for _, granted := range a.Permissions {
		if granted == permission {
			return true
		}
	}
	return false
}

func (s *Service) List(ctx context.Context, actor Actor, filter ListFilter) (Page, error) {
	if err := requirePermission(actor, PermissionUsersRead); err != nil {
		return Page{}, fmt.Errorf("list users: %w", err)
	}
	filter.Email = strings.ToLower(strings.TrimSpace(filter.Email))
	filter.Name = strings.TrimSpace(filter.Name)
	filter.Role = strings.ToLower(strings.TrimSpace(filter.Role))
	if err := filter.Validate(); err != nil {
		return Page{}, fmt.Errorf("list users: %w", err)
	}

	page, err := s.store.List(ctx, filter)
	if err != nil {
		return Page{}, fmt.Errorf("list users: %w", err)
	}
	return clonePage(page), nil
}

func (s *Service) SetStatus(ctx context.Context, actor Actor, input SetStatusInput) error {
	if err := requirePermission(actor, PermissionUsersWrite); err != nil {
		return fmt.Errorf("set user %d status: %w", input.UserID, err)
	}
	if !validStatus(input.Status) {
		return fmt.Errorf("set user %d status: %w", input.UserID, &ValidationError{
			Field: "status", Message: fmt.Sprintf("%q is not supported", input.Status), Err: ErrInvalidStatus,
		})
	}
	if input.Status == StatusDisabled {
		last, err := s.store.IsLastActiveAdmin(ctx, input.UserID)
		if err != nil {
			return fmt.Errorf("set user %d status: inspect admin invariant: %w", input.UserID, err)
		}
		if last {
			return fmt.Errorf("set user %d status: %w", input.UserID, ErrLastAdmin)
		}
	}

	// This preflight is feedback only. The store must serialize both status and role
	// revocations on one shared guard before its transactional recount and write;
	// otherwise separate targets can suffer last-admin write-skew.
	if err := s.store.SetStatus(ctx, input.UserID, input.Status, input.ExpectedVersion); err != nil {
		return fmt.Errorf("set user %d status: %w", input.UserID, err)
	}
	return nil
}

func (s *Service) ReplaceRoles(ctx context.Context, actor Actor, input ReplaceRolesInput) error {
	if err := requirePermission(actor, PermissionUsersRoles); err != nil {
		return fmt.Errorf("replace user %d roles: %w", input.UserID, err)
	}
	roles, err := normalizeRoleNames(input.Roles)
	if err != nil {
		return fmt.Errorf("replace user %d roles: %w", input.UserID, err)
	}
	exist, err := s.store.RoleNamesExist(ctx, roles)
	if err != nil {
		return fmt.Errorf("replace user %d roles: validate roles: %w", input.UserID, err)
	}
	if !exist {
		return fmt.Errorf("replace user %d roles: %w", input.UserID, ErrRoleNotFound)
	}
	if !slices.Contains(roles, RoleAdmin) {
		last, err := s.store.IsLastActiveAdmin(ctx, input.UserID)
		if err != nil {
			return fmt.Errorf("replace user %d roles: inspect admin invariant: %w", input.UserID, err)
		}
		if last {
			return fmt.Errorf("replace user %d roles: %w", input.UserID, ErrLastAdmin)
		}
	}

	// The store uses the same serialization guard and lock order as SetStatus before
	// its transactional recount and write; this preflight is stale by write time.
	if err := s.store.ReplaceRoles(ctx, input.UserID, roles, input.ExpectedVersion); err != nil {
		return fmt.Errorf("replace user %d roles: %w", input.UserID, err)
	}
	return nil
}

func (s *Service) Permissions(ctx context.Context, userID uint64) ([]string, error) {
	permissions, err := s.store.Permissions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user %d permissions: %w", userID, err)
	}
	return slices.Clone(permissions), nil
}

func requirePermission(actor Actor, permission string) error {
	if !actor.HasPermission(permission) {
		return fmt.Errorf("permission %q required: %w", permission, ErrForbidden)
	}
	return nil
}

func normalizeRoleNames(values []string) ([]string, error) {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !utf8.ValidString(value) {
			return nil, fmt.Errorf("role name must be valid UTF-8: %w", ErrRoleNotFound)
		}
		name := strings.ToLower(strings.TrimSpace(value))
		if name == "" {
			return nil, fmt.Errorf("role name is required: %w", ErrRoleNotFound)
		}
		unique[name] = struct{}{}
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("at least one role is required: %w", ErrRoleNotFound)
	}

	roles := make([]string, 0, len(unique))
	for name := range unique {
		roles = append(roles, name)
	}
	slices.Sort(roles)
	return roles, nil
}

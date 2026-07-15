package user

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRBACConstantsAreStable(t *testing.T) {
	wantRoles := []string{"admin", "customer"}
	gotRoles := []string{RoleAdmin, RoleCustomer}
	if diff := cmp.Diff(wantRoles, gotRoles); diff != "" {
		t.Fatalf("role constants mismatch (-want +got):\n%s", diff)
	}

	wantPermissions := []string{
		"orders:manage",
		"orders:read_all",
		"products:stock",
		"products:write",
		"stats:read",
		"users:read",
		"users:roles",
		"users:write",
	}
	gotPermissions := []string{
		PermissionOrdersManage,
		PermissionOrdersReadAll,
		PermissionProductsStock,
		PermissionProductsWrite,
		PermissionStatsRead,
		PermissionUsersRead,
		PermissionUsersRoles,
		PermissionUsersWrite,
	}
	if diff := cmp.Diff(wantPermissions, gotPermissions); diff != "" {
		t.Fatalf("permission constants mismatch (-want +got):\n%s", diff)
	}
}

func TestActorHasPermissionRequiresExactMatch(t *testing.T) {
	actor := Actor{UserID: 7, Permissions: []string{PermissionUsersWrite, ""}}
	tests := []struct {
		name       string
		permission string
		want       bool
	}{
		{name: "exact", permission: PermissionUsersWrite, want: true},
		{name: "prefix", permission: "users", want: false},
		{name: "suffix", permission: "write", want: false},
		{name: "case sensitive", permission: "USERS:WRITE", want: false},
		{name: "empty fails closed", permission: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := actor.HasPermission(tt.permission); got != tt.want {
				t.Fatalf("HasPermission(%q) = %v, want %v", tt.permission, got, tt.want)
			}
		})
	}

	if (Actor{}).HasPermission(PermissionUsersWrite) {
		t.Fatal("zero Actor unexpectedly has a permission")
	}
}

func TestListFilterValidate(t *testing.T) {
	validSorts := []string{"id", "email", "name", "status", "created_at", "updated_at"}
	for _, sortField := range validSorts {
		t.Run("valid sort "+sortField, func(t *testing.T) {
			filter := ListFilter{Sort: sortField, Limit: 20}
			if err := filter.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}

	tests := []struct {
		name   string
		filter ListFilter
	}{
		{name: "invalid status", filter: ListFilter{Status: "pending", Sort: "id", Limit: 20}},
		{name: "empty sort", filter: ListFilter{Sort: "", Limit: 20}},
		{name: "unknown sort", filter: ListFilter{Sort: "password_hash", Limit: 20}},
		{name: "zero limit", filter: ListFilter{Sort: "id", Limit: 0}},
		{name: "limit above maximum", filter: ListFilter{Sort: "id", Limit: 101}},
		{name: "negative offset", filter: ListFilter{Sort: "id", Limit: 20, Offset: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var validationErr *ValidationError
			if err := tt.filter.Validate(); !errors.As(err, &validationErr) {
				t.Fatalf("Validate() error = %v, want *ValidationError", err)
			}
		})
	}
}

func TestServiceListNormalizesFiltersAndDelegates(t *testing.T) {
	store := newFakeStore()
	store.listPage = Page{Items: []User{{ID: 1}}, Total: 1, Limit: 100, Offset: 2}
	service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})
	filter := ListFilter{
		Email: " Alice@Example.COM ", Name: " Alice ", Status: StatusActive,
		Role: " CUSTOMER ", Sort: "name", Descending: true, Limit: 100, Offset: 2,
	}

	got, err := service.List(t.Context(), Actor{UserID: 9, Permissions: []string{PermissionUsersRead}}, filter)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	wantFilter := ListFilter{
		Email: "alice@example.com", Name: "Alice", Status: StatusActive,
		Role: "customer", Sort: "name", Descending: true, Limit: 100, Offset: 2,
	}
	if diff := cmp.Diff(wantFilter, store.listFilter); diff != "" {
		t.Fatalf("List() filter mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(store.listPage, got); diff != "" {
		t.Fatalf("List() page mismatch (-want +got):\n%s", diff)
	}
}

func TestServiceListReturnsDeepOwnedPage(t *testing.T) {
	store := newFakeStore()
	store.listPage = Page{
		Items: []User{{
			ID: 1, Name: "Joel",
			Roles: []Role{{Name: RoleAdmin, Permissions: []Permission{{Name: PermissionUsersWrite}}}},
		}},
		Total: 1, Limit: 20,
	}
	service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

	got, err := service.List(t.Context(), Actor{Permissions: []string{PermissionUsersRead}}, ListFilter{Sort: "id", Limit: 20})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	got.Items[0].Name = "caller mutation"
	got.Items[0].Roles[0].Name = RoleCustomer
	got.Items[0].Roles[0].Permissions[0].Name = PermissionUsersRead

	backing := store.listPage.Items[0]
	if backing.Name != "Joel" || backing.Roles[0].Name != RoleAdmin || backing.Roles[0].Permissions[0].Name != PermissionUsersWrite {
		t.Fatalf("caller mutation contaminated store page: %+v", store.listPage)
	}
}

func TestServiceAdminByIDRequiresUsersReadAndReturnsClone(t *testing.T) {
	store := newFakeStore()
	store.byIDUser = &User{
		ID: 7, Email: "admin@example.com", Name: "Admin", Status: StatusActive,
		Roles: []Role{{Name: RoleAdmin, Permissions: []Permission{{Name: PermissionUsersRead}}}},
	}
	service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

	got, err := service.AdminByID(t.Context(), Actor{UserID: 9, Permissions: []string{PermissionUsersRead}}, 7)
	if err != nil {
		t.Fatalf("AdminByID() error = %v", err)
	}
	if store.byIDCalls != 1 {
		t.Fatalf("ByID() calls = %d, want 1", store.byIDCalls)
	}
	got.Name = "mutated"
	got.Roles[0].Name = RoleCustomer
	if store.byIDUser.Name != "Admin" || store.byIDUser.Roles[0].Name != RoleAdmin {
		t.Fatalf("AdminByID() exposed mutable backing data: %+v", store.byIDUser)
	}
}

func TestServiceAdminByIDFailsClosed(t *testing.T) {
	store := newFakeStore()
	store.byIDUser = &User{ID: 7, Email: "admin@example.com", Name: "Admin", Status: StatusActive}
	service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

	if _, err := service.AdminByID(t.Context(), Actor{Permissions: []string{"users"}}, 7); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AdminByID() error = %v, want ErrForbidden", err)
	}
	if store.byIDCalls != 0 {
		t.Fatalf("ByID() calls = %d, want 0", store.byIDCalls)
	}

	store.byIDUser = nil
	if _, err := service.AdminByID(t.Context(), Actor{Permissions: []string{PermissionUsersRead}}, 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AdminByID() error = %v, want ErrNotFound", err)
	}
}

func TestServiceListFailsClosed(t *testing.T) {
	t.Run("forbidden", func(t *testing.T) {
		store := newFakeStore()
		service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

		_, err := service.List(t.Context(), Actor{Permissions: []string{"users"}}, ListFilter{Sort: "id", Limit: 20})
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("List() error = %v, want ErrForbidden", err)
		}
		if store.listCalls != 0 {
			t.Fatalf("List() store calls = %d, want 0", store.listCalls)
		}
	})

	t.Run("invalid filter is not clamped", func(t *testing.T) {
		store := newFakeStore()
		service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

		_, err := service.List(t.Context(), Actor{Permissions: []string{PermissionUsersRead}}, ListFilter{Sort: "id", Limit: 101})
		if err == nil {
			t.Fatal("List() error = nil, want validation error")
		}
		if store.listCalls != 0 {
			t.Fatalf("List() store calls = %d, want 0", store.listCalls)
		}
	})
}

func TestServiceSetStatus(t *testing.T) {
	store := newFakeStore()
	service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})
	input := SetStatusInput{UserID: 7, Status: StatusDisabled, ExpectedVersion: 3}

	err := service.SetStatus(t.Context(), Actor{UserID: 9, Permissions: []string{PermissionUsersWrite}}, input)
	if err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}
	if store.lastAdminCalls != 1 || store.lastAdminUserID != 7 {
		t.Fatalf("IsLastActiveAdmin() calls/id = %d/%d, want 1/7", store.lastAdminCalls, store.lastAdminUserID)
	}
	if store.setStatusCalls != 1 || store.setStatusUserID != 7 || store.setStatus != StatusDisabled || store.setStatusExpected != 3 {
		t.Fatalf("SetStatus() store call = calls:%d id:%d status:%q version:%d", store.setStatusCalls, store.setStatusUserID, store.setStatus, store.setStatusExpected)
	}
}

func TestServiceSetStatusRejectsUnauthorizedInvalidAndLastAdmin(t *testing.T) {
	tests := []struct {
		name      string
		actor     Actor
		input     SetStatusInput
		configure func(*fakeStore)
		wantErr   error
	}{
		{
			name: "forbidden", actor: Actor{Permissions: []string{"users"}},
			input: SetStatusInput{UserID: 7, Status: StatusDisabled, ExpectedVersion: 1}, wantErr: ErrForbidden,
		},
		{
			name: "invalid status", actor: Actor{Permissions: []string{PermissionUsersWrite}},
			input: SetStatusInput{UserID: 7, Status: "deleted", ExpectedVersion: 1}, wantErr: ErrInvalidStatus,
		},
		{
			name: "last active admin", actor: Actor{Permissions: []string{PermissionUsersWrite}},
			input:     SetStatusInput{UserID: 7, Status: StatusDisabled, ExpectedVersion: 1},
			configure: func(store *fakeStore) { store.lastActiveAdmin = true }, wantErr: ErrLastAdmin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			if tt.configure != nil {
				tt.configure(store)
			}
			service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

			err := service.SetStatus(t.Context(), tt.actor, tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetStatus() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if store.setStatusCalls != 0 {
				t.Fatalf("SetStatus() store calls = %d, want 0", store.setStatusCalls)
			}
		})
	}
}

func TestServiceSetStatusPropagatesPreflightAndWriteErrors(t *testing.T) {
	t.Run("preflight not found", func(t *testing.T) {
		store := newFakeStore()
		store.lastAdminErr = fmt.Errorf("inspect target: %w", ErrNotFound)
		service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

		err := service.SetStatus(t.Context(), Actor{Permissions: []string{PermissionUsersWrite}}, SetStatusInput{
			UserID: 404, Status: StatusDisabled, ExpectedVersion: 1,
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("SetStatus() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("optimistic conflict", func(t *testing.T) {
		store := newFakeStore()
		store.setStatusErr = fmt.Errorf("update: %w", ErrConflict)
		service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

		err := service.SetStatus(t.Context(), Actor{Permissions: []string{PermissionUsersWrite}}, SetStatusInput{
			UserID: 7, Status: StatusActive, ExpectedVersion: 2,
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("SetStatus() error = %v, want ErrConflict", err)
		}
	})
}

func TestServiceReplaceRolesNormalizesDeduplicatesAndSorts(t *testing.T) {
	store := newFakeStore()
	service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})
	input := ReplaceRolesInput{UserID: 7, Roles: []string{" Customer ", "admin", "CUSTOMER"}, ExpectedVersion: 5}

	err := service.ReplaceRoles(t.Context(), Actor{UserID: 9, Permissions: []string{PermissionUsersRoles}}, input)
	if err != nil {
		t.Fatalf("ReplaceRoles() error = %v", err)
	}
	wantRoles := []string{"admin", "customer"}
	if diff := cmp.Diff(wantRoles, store.rolesExistInput); diff != "" {
		t.Fatalf("RoleNamesExist() roles mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantRoles, store.replaceRoles); diff != "" {
		t.Fatalf("ReplaceRoles() roles mismatch (-want +got):\n%s", diff)
	}
	if store.replaceUserID != 7 || store.replaceExpected != 5 {
		t.Fatalf("ReplaceRoles() store id/version = %d/%d, want 7/5", store.replaceUserID, store.replaceExpected)
	}
	if store.lastAdminCalls != 0 {
		t.Fatalf("IsLastActiveAdmin() calls = %d, want 0 while admin role remains", store.lastAdminCalls)
	}
}

func TestServiceReplaceRolesValidatesAuthorizationAndRoles(t *testing.T) {
	tests := []struct {
		name      string
		actor     Actor
		roles     []string
		configure func(*fakeStore)
		wantErr   error
	}{
		{name: "forbidden", actor: Actor{Permissions: []string{"users:write"}}, roles: []string{RoleCustomer}, wantErr: ErrForbidden},
		{name: "requires one role", actor: Actor{Permissions: []string{PermissionUsersRoles}}, roles: nil, wantErr: ErrRoleNotFound},
		{name: "normalized empty role", actor: Actor{Permissions: []string{PermissionUsersRoles}}, roles: []string{" \t "}, wantErr: ErrRoleNotFound},
		{name: "invalid UTF-8 role", actor: Actor{Permissions: []string{PermissionUsersRoles}}, roles: []string{string([]byte{0xff})}, wantErr: ErrRoleNotFound},
		{
			name: "unknown role", actor: Actor{Permissions: []string{PermissionUsersRoles}}, roles: []string{"operator"},
			configure: func(store *fakeStore) { store.rolesExist = false }, wantErr: ErrRoleNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			if tt.configure != nil {
				tt.configure(store)
			}
			service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

			err := service.ReplaceRoles(t.Context(), tt.actor, ReplaceRolesInput{UserID: 7, Roles: tt.roles, ExpectedVersion: 1})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReplaceRoles() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if store.replaceCalls != 0 {
				t.Fatalf("ReplaceRoles() store calls = %d, want 0", store.replaceCalls)
			}
		})
	}
}

func TestServiceReplaceRolesProtectsLastActiveAdmin(t *testing.T) {
	store := newFakeStore()
	store.lastActiveAdmin = true
	service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

	err := service.ReplaceRoles(t.Context(), Actor{Permissions: []string{PermissionUsersRoles}}, ReplaceRolesInput{
		UserID: 7, Roles: []string{RoleCustomer}, ExpectedVersion: 2,
	})
	if !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("ReplaceRoles() error = %v, want ErrLastAdmin", err)
	}
	if store.lastAdminCalls != 1 || store.replaceCalls != 0 {
		t.Fatalf("store calls = last admin %d, replace %d; want 1, 0", store.lastAdminCalls, store.replaceCalls)
	}
}

func TestServiceReplaceRolesPropagatesRoleLookupAndConflict(t *testing.T) {
	t.Run("role lookup not found", func(t *testing.T) {
		store := newFakeStore()
		store.rolesExistErr = fmt.Errorf("query roles: %w", ErrNotFound)
		service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

		err := service.ReplaceRoles(t.Context(), Actor{Permissions: []string{PermissionUsersRoles}}, ReplaceRolesInput{
			UserID: 7, Roles: []string{RoleCustomer}, ExpectedVersion: 1,
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("ReplaceRoles() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("optimistic conflict", func(t *testing.T) {
		store := newFakeStore()
		store.replaceErr = fmt.Errorf("replace: %w", ErrConflict)
		service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

		err := service.ReplaceRoles(t.Context(), Actor{Permissions: []string{PermissionUsersRoles}}, ReplaceRolesInput{
			UserID: 7, Roles: []string{RoleAdmin}, ExpectedVersion: 4,
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("ReplaceRoles() error = %v, want ErrConflict", err)
		}
	})
}

func TestServicePermissionsDelegatesAndPreservesErrors(t *testing.T) {
	store := newFakeStore()
	store.permissions = []string{PermissionUsersRead, PermissionProductsWrite}
	service := newTestService(t, store, &fakePasswords{}, fakeClock{now: fixedTime})

	got, err := service.Permissions(t.Context(), 7)
	if err != nil {
		t.Fatalf("Permissions() error = %v", err)
	}
	if diff := cmp.Diff(store.permissions, got); diff != "" {
		t.Fatalf("Permissions() mismatch (-want +got):\n%s", diff)
	}
	if store.permissionsCalls != 1 || store.permissionsID != 7 {
		t.Fatalf("Permissions() store calls/id = %d/%d, want 1/7", store.permissionsCalls, store.permissionsID)
	}
	got[0] = PermissionUsersRoles
	if store.permissions[0] != PermissionUsersRead {
		t.Fatalf("caller mutation contaminated store permissions: %v", store.permissions)
	}

	store.permissionsErr = fmt.Errorf("permissions lookup: %w", ErrNotFound)
	_, err = service.Permissions(t.Context(), 404)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Permissions() error = %v, want ErrNotFound", err)
	}
}

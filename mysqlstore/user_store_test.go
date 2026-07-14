//go:build integration

package mysqlstore

import (
	stdcmp "cmp"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"clean-arch-gin/user"
	"github.com/google/go-cmp/cmp"
	"gorm.io/gorm"
)

func TestUserStoreMappingUniqueListAndVersion(t *testing.T) {
	store := mustUserStore(t, newTestDB(t))
	now := time.Date(2026, 7, 14, 1, 2, 3, 456000000, time.FixedZone("test", 8*60*60))
	admin := &user.User{
		Email: "admin@example.com", Name: "Admin", PasswordHash: "encoded-admin", Status: user.StatusActive,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateWithRole(t.Context(), admin, user.RoleAdmin); err != nil {
		t.Fatalf("CreateWithRole() error = %v", err)
	}
	if admin.ID == 0 {
		t.Fatal("CreateWithRole() did not assign ID")
	}

	got, err := store.ByEmail(t.Context(), "admin@example.com")
	if err != nil {
		t.Fatalf("ByEmail() error = %v", err)
	}
	if got.ID != admin.ID || got.Name != "Admin" || got.CreatedAt.Location() != time.UTC || got.UpdatedAt.Location() != time.UTC {
		t.Fatalf("ByEmail() = %+v", got)
	}
	wantRoles := []user.Role{{ID: got.Roles[0].ID, Name: user.RoleAdmin, Permissions: got.Roles[0].Permissions}}
	if diff := cmp.Diff(wantRoles, got.Roles); diff != "" {
		t.Fatalf("roles mismatch (-want +got):\n%s", diff)
	}
	if len(got.Roles[0].Permissions) != 8 || !slices.IsSortedFunc(got.Roles[0].Permissions, func(a, b user.Permission) int { return stdcmp.Compare(a.Name, b.Name) }) {
		t.Fatalf("admin permissions are not complete and sorted: %+v", got.Roles[0].Permissions)
	}

	duplicate := &user.User{Email: admin.Email, Name: "Other", PasswordHash: "encoded", Status: user.StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWithRole(t.Context(), duplicate, user.RoleCustomer); !errors.Is(err, user.ErrEmailExists) {
		t.Fatalf("duplicate CreateWithRole() error = %v, want ErrEmailExists", err)
	}

	got.Name = "Updated"
	updated, err := store.UpdateProfile(t.Context(), got, 1)
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.Name != "Updated" || updated.Version != 2 {
		t.Fatalf("UpdateProfile() = %+v", updated)
	}
	if _, err := store.UpdateProfile(t.Context(), got, 1); !errors.Is(err, user.ErrConflict) {
		t.Fatalf("stale UpdateProfile() error = %v, want ErrConflict", err)
	}
	missing := &user.User{ID: ^uint64(0), Name: "Missing"}
	if _, err := store.UpdateProfile(t.Context(), missing, 1); !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("missing UpdateProfile() error = %v, want ErrNotFound", err)
	}
	passwordUpdated, err := store.UpdatePasswordHash(t.Context(), admin.ID, "new-encoded", 2)
	if err != nil || passwordUpdated.PasswordHash != "new-encoded" || passwordUpdated.Version != 3 {
		t.Fatalf("UpdatePasswordHash() = (%+v, %v)", passwordUpdated, err)
	}
	if _, err := store.UpdatePasswordHash(t.Context(), admin.ID, "stale-encoded", 2); !errors.Is(err, user.ErrConflict) {
		t.Fatalf("stale UpdatePasswordHash() error = %v, want ErrConflict", err)
	}
	if _, err := store.UpdatePasswordHash(t.Context(), ^uint64(0), "missing-encoded", 1); !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("missing UpdatePasswordHash() error = %v, want ErrNotFound", err)
	}

	customer := createStoredUser(t, store, "customer@example.com", user.RoleCustomer)
	page, err := store.List(t.Context(), user.ListFilter{Role: user.RoleCustomer, Sort: "email", Descending: true, Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != customer.ID || len(page.Items[0].Roles) != 1 {
		t.Fatalf("List() = %+v", page)
	}
	exists, err := store.RoleNamesExist(t.Context(), []string{"admin", "customer"})
	if err != nil || !exists {
		t.Fatalf("RoleNamesExist() = (%v, %v)", exists, err)
	}
	exists, err = store.RoleNamesExist(t.Context(), []string{"admin", "missing"})
	if err != nil || exists {
		t.Fatalf("RoleNamesExist(missing) = (%v, %v)", exists, err)
	}
	permissions, err := store.Permissions(t.Context(), admin.ID)
	if err != nil || len(permissions) != 8 || !slices.IsSorted(permissions) {
		t.Fatalf("Permissions() = (%v, %v)", permissions, err)
	}
	if permissionsWithDuplicate := append(slices.Clone(permissions), permissions...); len(permissionsWithDuplicate) != 16 {
		t.Fatal("test setup failure")
	}
}

func TestUserStoreMissingRowsAndUnknownRole(t *testing.T) {
	store := mustUserStore(t, newTestDB(t))
	if _, err := store.ByID(t.Context(), 404); !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("ByID() error = %v, want ErrNotFound", err)
	}
	account := &user.User{Email: "unknown-role@example.com", Name: "Unknown", PasswordHash: "encoded", Status: user.StatusActive, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := store.CreateWithRole(t.Context(), account, "operator"); !errors.Is(err, user.ErrRoleNotFound) {
		t.Fatalf("CreateWithRole() error = %v, want ErrRoleNotFound", err)
	}
	if _, err := store.ByEmail(t.Context(), account.Email); !errors.Is(err, user.ErrNotFound) {
		t.Fatalf("rolled-back account lookup error = %v, want ErrNotFound", err)
	}
}

func TestUserStoreConcurrentDisableKeepsOneAdmin(t *testing.T) {
	store := mustUserStore(t, newTestDB(t))
	first := createStoredUser(t, store, "admin-one@example.com", user.RoleAdmin)
	second := createStoredUser(t, store, "admin-two@example.com", user.RoleAdmin)
	errs := runConcurrentAdminWrites(func(index int) error {
		ids := []uint64{first.ID, second.ID}
		return store.SetStatus(t.Context(), ids[index], user.StatusDisabled, 1)
	})
	assertOneAdminMutation(t, errs)
	assertActiveAdminCount(t, store, first.ID, second.ID)
}

func TestUserStoreConcurrentRoleRemovalKeepsOneAdmin(t *testing.T) {
	store := mustUserStore(t, newTestDB(t))
	first := createStoredUser(t, store, "role-admin-one@example.com", user.RoleAdmin)
	second := createStoredUser(t, store, "role-admin-two@example.com", user.RoleAdmin)
	errs := runConcurrentAdminWrites(func(index int) error {
		ids := []uint64{first.ID, second.ID}
		return store.ReplaceRoles(t.Context(), ids[index], []string{user.RoleCustomer}, 1)
	})
	assertOneAdminMutation(t, errs)
	assertActiveAdminCount(t, store, first.ID, second.ID)
}

func runConcurrentAdminWrites(write func(int) error) []error {
	start := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(2)
	var workers sync.WaitGroup
	workers.Add(2)
	errs := make([]error, 2)
	for index := range 2 {
		go func() {
			defer workers.Done()
			ready.Done()
			<-start
			errs[index] = write(index)
		}()
	}
	ready.Wait()
	close(start)
	workers.Wait()
	return errs
}

func assertOneAdminMutation(t *testing.T, errs []error) {
	t.Helper()
	succeeded := 0
	lastAdmin := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, user.ErrLastAdmin):
			lastAdmin++
		default:
			t.Fatalf("concurrent mutation error = %v", err)
		}
	}
	if succeeded != 1 || lastAdmin != 1 {
		t.Fatalf("concurrent mutations = success:%d last-admin:%d errors:%v", succeeded, lastAdmin, errs)
	}
}

func assertActiveAdminCount(t *testing.T, store *UserStore, ids ...uint64) {
	t.Helper()
	activeAdmins := 0
	for _, id := range ids {
		account, err := store.ByID(t.Context(), id)
		if err != nil {
			t.Fatalf("ByID(%d) error = %v", id, err)
		}
		if account.Status == user.StatusActive && slices.ContainsFunc(account.Roles, func(role user.Role) bool { return role.Name == user.RoleAdmin }) {
			activeAdmins++
		}
	}
	if activeAdmins != 1 {
		t.Fatalf("active admins = %d, want 1", activeAdmins)
	}
}

func mustUserStore(t *testing.T, db *gorm.DB) *UserStore {
	t.Helper()
	store, err := NewUserStore(db)
	if err != nil {
		t.Fatalf("NewUserStore() error = %v", err)
	}
	return store
}

func createStoredUser(t *testing.T, store *UserStore, email, role string) *user.User {
	t.Helper()
	now := time.Now().UTC()
	account := &user.User{Email: email, Name: fmt.Sprintf("User %s", email), PasswordHash: "encoded", Status: user.StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWithRole(t.Context(), account, role); err != nil {
		t.Fatalf("CreateWithRole(%q) error = %v", role, err)
	}
	return account
}

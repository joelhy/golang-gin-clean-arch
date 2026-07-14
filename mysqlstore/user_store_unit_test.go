package mysqlstore

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNewUserStoreRejectsNilDatabase(t *testing.T) {
	store, err := NewUserStore(nil)
	if err == nil || store != nil {
		t.Fatalf("NewUserStore(nil) = (%v, %v), want nil/error", store, err)
	}
}

func TestNormalizedRoleNamesRetainsInvalidNameForFailClosedLookup(t *testing.T) {
	got := normalizedRoleNames([]string{" admin ", " ", "ADMIN"})
	want := []string{"", "admin"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("normalizedRoleNames() mismatch (-want +got):\n%s", diff)
	}
}

package mysqlstore

import (
	"errors"
	"strings"
	"testing"

	"clean-arch-gin/user"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/go-cmp/cmp"
)

func TestMapCreateUserErrorOnlyMapsEmailConstraint(t *testing.T) {
	const sensitiveValue = "private@example.com"
	tests := []struct {
		name      string
		message   string
		wantEmail bool
	}{
		{name: "email key", message: "Duplicate entry '" + sensitiveValue + "' for key 'uk_users_email'", wantEmail: true},
		{name: "qualified email key", message: "Duplicate entry '" + sensitiveValue + "' for key 'users.uk_users_email'", wantEmail: true},
		{name: "primary key", message: "Duplicate entry '42' for key 'PRIMARY'"},
		{name: "other unique key", message: "Duplicate entry '" + sensitiveValue + "' for key 'uk_users_external_id'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cause := &mysqldriver.MySQLError{Number: 1062, Message: tt.message}
			err := mapCreateUserError(cause)
			if got := errors.Is(err, user.ErrEmailExists); got != tt.wantEmail {
				t.Fatalf("errors.Is(mapCreateUserError(), ErrEmailExists) = %v, want %v; error %v", got, tt.wantEmail, err)
			}
			if strings.Contains(err.Error(), sensitiveValue) {
				t.Fatal("mapCreateUserError() exposed duplicate value")
			}
			if !tt.wantEmail {
				var mysqlErr *mysqldriver.MySQLError
				if !errors.As(err, &mysqlErr) {
					t.Fatalf("mapCreateUserError() = %v, want safely wrapped MySQL error", err)
				}
			}
		})
	}
}

func TestConditionalUpdateMissErrorDistinguishesMissingAndConflict(t *testing.T) {
	tests := []struct {
		name   string
		exists bool
		want   error
	}{
		{name: "missing", want: user.ErrNotFound},
		{name: "existing version mismatch", exists: true, want: user.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := conditionalUpdateMissError(tt.exists); !errors.Is(err, tt.want) {
				t.Fatalf("conditionalUpdateMissError(%v) = %v, want %v", tt.exists, err, tt.want)
			}
		})
	}
}

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

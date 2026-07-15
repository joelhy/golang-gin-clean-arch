package order

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestListFilterValidate(t *testing.T) {
	from := fixedTime.Add(-24 * time.Hour)
	to := fixedTime
	min := int64(100)
	max := int64(500)

	tests := []struct {
		name    string
		filter  ListFilter
		wantErr error
	}{
		{
			name: "valid",
			filter: ListFilter{
				UserID:         7,
				Statuses:       []Status{StatusPending, StatusConfirmed},
				CreatedFrom:    &from,
				CreatedTo:      &to,
				MinTotalAmount: &min,
				MaxTotalAmount: &max,
				Sort:           "created_at",
				Limit:          20,
			},
		},
		{name: "unknown status", filter: ListFilter{Statuses: []Status{"paused"}, Sort: "created_at", Limit: 20}, wantErr: ErrInvalidFilter},
		{name: "inverted time range", filter: ListFilter{CreatedFrom: &to, CreatedTo: &from, Sort: "created_at", Limit: 20}, wantErr: ErrInvalidFilter},
		{name: "inverted amount range", filter: ListFilter{MinTotalAmount: &max, MaxTotalAmount: &min, Sort: "created_at", Limit: 20}, wantErr: ErrInvalidFilter},
		{name: "unknown sort field", filter: ListFilter{Sort: "random", Limit: 20}, wantErr: ErrInvalidFilter},
		{name: "limit too small", filter: ListFilter{Sort: "created_at", Limit: 0}, wantErr: ErrInvalidFilter},
		{name: "limit too large", filter: ListFilter{Sort: "created_at", Limit: 101}, wantErr: ErrInvalidFilter},
		{name: "negative offset", filter: ListFilter{Sort: "created_at", Limit: 20, Offset: -1}, wantErr: ErrInvalidFilter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListFilter.Validate() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
		})
	}
}

func TestByIDEnforcesOwnershipUnlessAdmin(t *testing.T) {
	reader := &fakeReader{
		byIDOrder: &Order{
			ID:        7,
			UserID:    55,
			Status:    StatusPending,
			Total:     Money{Amount: 100, Currency: "USD"},
			CreatedAt: fixedTime,
			UpdatedAt: fixedTime,
		},
	}
	svc := newTestService(t, fakeTransactor{tx: &fakeTx{}}, reader, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.ByID(t.Context(), Actor{UserID: 99}, 7)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ByID() error = %v, want errors.Is(_, %v)", err, ErrForbidden)
	}

	got, err := svc.ByID(t.Context(), Actor{UserID: 1, Admin: true}, 7)
	if err != nil {
		t.Fatalf("ByID() admin error = %v", err)
	}
	if got.UserID != 55 {
		t.Fatalf("ByID() user id = %d, want 55", got.UserID)
	}
}

func TestListForUserScopesFilterAndClonesPage(t *testing.T) {
	reader := &fakeReader{
		listPage: Page{
			Items: []Order{{
				ID:        1,
				UserID:    7,
				Status:    StatusPending,
				Number:    "ORD-1",
				Total:     Money{Amount: 100, Currency: "USD"},
				CreatedAt: fixedTime,
				UpdatedAt: fixedTime,
			}},
			Total:  1,
			Limit:  20,
			Offset: 0,
		},
	}
	svc := newTestService(t, fakeTransactor{tx: &fakeTx{}}, reader, fakeClock{now: fixedTime}, fakeNumbers{})

	got, err := svc.List(t.Context(), Actor{UserID: 7}, ListFilter{Sort: "created_at", Limit: 20})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if reader.listFilter.UserID != 7 {
		t.Fatalf("List() filter user id = %d, want 7", reader.listFilter.UserID)
	}

	got.Items[0].Number = "mutated"
	if reader.listPage.Items[0].Number != "ORD-1" {
		t.Fatalf("List() should clone returned page, got source number %q", reader.listPage.Items[0].Number)
	}
}

func TestListForAdminKeepsRequestedUserScope(t *testing.T) {
	reader := &fakeReader{}
	svc := newTestService(t, fakeTransactor{tx: &fakeTx{}}, reader, fakeClock{now: fixedTime}, fakeNumbers{})

	_, _ = svc.List(t.Context(), Actor{UserID: 1, Admin: true}, ListFilter{UserID: 99, Sort: "created_at", Limit: 20})
	if reader.listFilter.UserID != 99 {
		t.Fatalf("List() admin filter user id = %d, want 99", reader.listFilter.UserID)
	}
}

func TestListDefaultsEmptySortBeforeDelegating(t *testing.T) {
	reader := &fakeReader{}
	svc := newTestService(t, fakeTransactor{tx: &fakeTx{}}, reader, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.List(t.Context(), Actor{UserID: 7}, ListFilter{Limit: 20})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if reader.listFilter.Sort != "created_at" {
		t.Fatalf("List() delegated sort = %q, want %q", reader.listFilter.Sort, "created_at")
	}
}

func TestSortOrdersUsesStableOrdering(t *testing.T) {
	items := []Order{
		{ID: 2, Number: "B", Total: Money{Amount: 100, Currency: "USD"}, CreatedAt: fixedTime, UpdatedAt: fixedTime},
		{ID: 1, Number: "A", Total: Money{Amount: 100, Currency: "USD"}, CreatedAt: fixedTime, UpdatedAt: fixedTime},
		{ID: 3, Number: "C", Total: Money{Amount: 200, Currency: "USD"}, CreatedAt: fixedTime.Add(time.Minute), UpdatedAt: fixedTime.Add(time.Minute)},
	}

	sortOrders(items, ListFilter{Sort: "total", Limit: 20})
	if diff := cmp.Diff([]uint64{2, 1, 3}, []uint64{items[0].ID, items[1].ID, items[2].ID}); diff != "" {
		t.Fatalf("sortOrders() stable mismatch (-want +got):\n%s", diff)
	}

	sortOrders(items, ListFilter{Sort: "id", Descending: true, Limit: 20})
	if diff := cmp.Diff([]uint64{3, 2, 1}, []uint64{items[0].ID, items[1].ID, items[2].ID}); diff != "" {
		t.Fatalf("sortOrders() descending mismatch (-want +got):\n%s", diff)
	}
}

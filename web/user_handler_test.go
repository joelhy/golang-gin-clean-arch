package web

import (
	"context"
	"net/http"
	"testing"
	"time"

	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type fakeUserHandlerService struct {
	meFn           func(context.Context, uint64) (*user.User, error)
	updateMeFn     func(context.Context, uint64, user.UpdateMeInput) (*user.User, error)
	listFn         func(context.Context, user.Actor, user.ListFilter) (user.Page, error)
	adminByIDFn    func(context.Context, user.Actor, uint64) (*user.User, error)
	setStatusFn    func(context.Context, user.Actor, user.SetStatusInput) error
	replaceRolesFn func(context.Context, user.Actor, user.ReplaceRolesInput) error
	permissionsFn  func(context.Context, uint64) ([]string, error)
}

func (f fakeUserHandlerService) Me(ctx context.Context, userID uint64) (*user.User, error) {
	return f.meFn(ctx, userID)
}

func (f fakeUserHandlerService) UpdateMe(ctx context.Context, userID uint64, input user.UpdateMeInput) (*user.User, error) {
	return f.updateMeFn(ctx, userID, input)
}

func (f fakeUserHandlerService) List(ctx context.Context, actor user.Actor, filter user.ListFilter) (user.Page, error) {
	return f.listFn(ctx, actor, filter)
}

func (f fakeUserHandlerService) AdminByID(ctx context.Context, actor user.Actor, userID uint64) (*user.User, error) {
	return f.adminByIDFn(ctx, actor, userID)
}

func (f fakeUserHandlerService) SetStatus(ctx context.Context, actor user.Actor, input user.SetStatusInput) error {
	return f.setStatusFn(ctx, actor, input)
}

func (f fakeUserHandlerService) ReplaceRoles(ctx context.Context, actor user.Actor, input user.ReplaceRolesInput) error {
	return f.replaceRolesFn(ctx, actor, input)
}

func (f fakeUserHandlerService) Permissions(ctx context.Context, userID uint64) ([]string, error) {
	return f.permissionsFn(ctx, userID)
}

func TestUserHandlerMe(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	now := time.Date(2026, 7, 15, 10, 0, 0, 123, time.FixedZone("CST", 8*3600))
	handler := NewUserHandler(fakeUserHandlerService{
		meFn: func(_ context.Context, userID uint64) (*user.User, error) {
			if userID != 11 {
				t.Fatalf("userID = %d", userID)
			}
			return &user.User{
				ID:        11,
				Email:     "me@example.com",
				Name:      "Me",
				Status:    user.StatusActive,
				Version:   3,
				CreatedAt: now,
				UpdatedAt: now.Add(time.Minute),
				Roles: []user.Role{
					{Name: user.RoleCustomer},
				},
				PasswordHash: "secret",
			}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/users/me", injectIdentity(user.Identity{UserID: 11}), handler.Me)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/users/me", nil, nil)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.id", float64(11))
	assertJSONPath(t, rec.Body.Bytes(), "data.updated_at", now.Add(time.Minute).UTC().Format(time.RFC3339Nano))
	assertJSONPath(t, rec.Body.Bytes(), "data.roles.0.name", "customer")
	assertJSONMissing(t, rec.Body.Bytes(), "data.password_hash")
}

func TestUserHandlerUpdateMe(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewUserHandler(fakeUserHandlerService{
		updateMeFn: func(_ context.Context, userID uint64, input user.UpdateMeInput) (*user.User, error) {
			if userID != 11 || input.Name != "Renamed" || input.ExpectedVersion != 5 {
				t.Fatalf("update user=%d input=%+v", userID, input)
			}
			return &user.User{ID: 11, Email: "me@example.com", Name: input.Name, Status: user.StatusActive, Version: 6}, nil
		},
	})

	router := gin.New()
	router.PATCH("/api/v1/users/me", injectIdentity(user.Identity{UserID: 11}), handler.UpdateMe)

	rec := performJSON(t, router, http.MethodPatch, "/api/v1/users/me", `{"name":"Renamed","version":5}`)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.name", "Renamed")
	assertJSONPath(t, rec.Body.Bytes(), "data.version", float64(6))
}

func TestUserHandlerAdminList(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewUserHandler(fakeUserHandlerService{
		listFn: func(_ context.Context, actor user.Actor, filter user.ListFilter) (user.Page, error) {
			if actor.UserID != 77 || len(actor.Permissions) != 1 || actor.Permissions[0] != user.PermissionUsersRead {
				t.Fatalf("actor = %+v", actor)
			}
			if filter.Email != "staff@example.com" || filter.Name != "Alice" || filter.Status != user.StatusActive || filter.Role != "admin" {
				t.Fatalf("filter = %+v", filter)
			}
			if filter.Sort != "created_at" || !filter.Descending || filter.Limit != 10 || filter.Offset != 20 {
				t.Fatalf("filter = %+v", filter)
			}
			return user.Page{
				Items:  []user.User{{ID: 1, Email: "staff@example.com", Name: "Alice", Status: user.StatusActive}},
				Total:  1,
				Limit:  10,
				Offset: 20,
			}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/users", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionUsersRead), handler.List)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/users?email=staff@example.com&name=Alice&status=active&role=admin&sort=created_at&direction=desc&limit=10&offset=20", nil, nil)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.items.0.email", "staff@example.com")
	assertJSONPath(t, rec.Body.Bytes(), "data.pagination.limit", float64(10))
	assertJSONMissing(t, rec.Body.Bytes(), "data.items.0.password_hash")
}

func TestUserHandlerAdminListRejectsInvalidQuery(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewUserHandler(fakeUserHandlerService{
		listFn: func(_ context.Context, _ user.Actor, _ user.ListFilter) (user.Page, error) {
			t.Fatal("List must not run for invalid query parameters")
			return user.Page{}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/users", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionUsersRead), handler.List)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/users?direction=sideways", nil, nil)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeValidation))
}

func TestUserHandlerAdminByID(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewUserHandler(fakeUserHandlerService{
		adminByIDFn: func(_ context.Context, actor user.Actor, userID uint64) (*user.User, error) {
			if actor.UserID != 77 || len(actor.Permissions) != 1 || actor.Permissions[0] != user.PermissionUsersRead {
				t.Fatalf("actor = %+v", actor)
			}
			if userID != 15 {
				t.Fatalf("userID = %d", userID)
			}
			return &user.User{ID: 15, Email: "staff@example.com", Name: "Alice", Status: user.StatusActive, Version: 2}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/users/:id", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionUsersRead), handler.GetAdmin)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/users/15", nil, nil)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.id", float64(15))
	assertJSONPath(t, rec.Body.Bytes(), "data.email", "staff@example.com")
}

func TestUserHandlerSetStatus(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewUserHandler(fakeUserHandlerService{
		setStatusFn: func(_ context.Context, actor user.Actor, input user.SetStatusInput) error {
			if actor.UserID != 77 || input.UserID != 15 || input.Status != user.StatusDisabled || input.ExpectedVersion != 9 {
				t.Fatalf("actor=%+v input=%+v", actor, input)
			}
			return nil
		},
	})

	router := gin.New()
	router.PATCH("/api/v1/admin/users/:id/status", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionUsersWrite), handler.SetStatus)

	rec := performJSON(t, router, http.MethodPatch, "/api/v1/admin/users/15/status", `{"status":"disabled","version":9}`)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeOK))
}

func TestUserHandlerReplaceRoles(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewUserHandler(fakeUserHandlerService{
		replaceRolesFn: func(_ context.Context, actor user.Actor, input user.ReplaceRolesInput) error {
			if actor.UserID != 77 || input.UserID != 15 || input.ExpectedVersion != 2 {
				t.Fatalf("actor=%+v input=%+v", actor, input)
			}
			if len(input.Roles) != 2 || input.Roles[0] != "admin" || input.Roles[1] != "customer" {
				t.Fatalf("roles = %#v", input.Roles)
			}
			return nil
		},
	})

	router := gin.New()
	router.PUT("/api/v1/admin/users/:id/roles", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionUsersRoles), handler.ReplaceRoles)

	rec := performJSON(t, router, http.MethodPut, "/api/v1/admin/users/15/roles", `{"roles":["admin","customer"],"version":2}`)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeOK))
}

func TestUserHandlerPermissions(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewUserHandler(fakeUserHandlerService{
		permissionsFn: func(_ context.Context, userID uint64) ([]string, error) {
			if userID != 15 {
				t.Fatalf("userID = %d", userID)
			}
			return []string{user.PermissionUsersRead, user.PermissionProductsWrite}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/users/:id/permissions", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionUsersRead), handler.Permissions)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/users/15/permissions", nil, nil)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.permissions.0", "users:read")
}

func TestUserHandlerAdminRoutesFailClosedWithoutPermissionEvidence(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	t.Run("list", func(t *testing.T) {
		called := false
		handler := NewUserHandler(fakeUserHandlerService{
			listFn: func(_ context.Context, _ user.Actor, _ user.ListFilter) (user.Page, error) {
				called = true
				return user.Page{}, nil
			},
		})

		router := gin.New()
		router.GET("/api/v1/admin/users", injectIdentity(user.Identity{UserID: 77}), handler.List)

		rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/users", nil, nil)

		assertStatusCode(t, rec, http.StatusForbidden)
		assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodePermission))
		if called {
			t.Fatal("List must not run without permission evidence")
		}
	})

	t.Run("set_status", func(t *testing.T) {
		called := false
		handler := NewUserHandler(fakeUserHandlerService{
			setStatusFn: func(_ context.Context, _ user.Actor, _ user.SetStatusInput) error {
				called = true
				return nil
			},
		})

		router := gin.New()
		router.PATCH("/api/v1/admin/users/:id/status", injectIdentity(user.Identity{UserID: 77}), handler.SetStatus)

		rec := performJSON(t, router, http.MethodPatch, "/api/v1/admin/users/15/status", `{"status":"disabled","version":9}`)

		assertStatusCode(t, rec, http.StatusForbidden)
		assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodePermission))
		if called {
			t.Fatal("SetStatus must not run without permission evidence")
		}
	})

	t.Run("admin_by_id", func(t *testing.T) {
		called := false
		handler := NewUserHandler(fakeUserHandlerService{
			adminByIDFn: func(_ context.Context, _ user.Actor, _ uint64) (*user.User, error) {
				called = true
				return &user.User{}, nil
			},
		})

		router := gin.New()
		router.GET("/api/v1/admin/users/:id", injectIdentity(user.Identity{UserID: 77}), handler.GetAdmin)

		rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/users/15", nil, nil)

		assertStatusCode(t, rec, http.StatusForbidden)
		assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodePermission))
		if called {
			t.Fatal("AdminByID must not run without permission evidence")
		}
	})

	t.Run("replace_roles", func(t *testing.T) {
		called := false
		handler := NewUserHandler(fakeUserHandlerService{
			replaceRolesFn: func(_ context.Context, _ user.Actor, _ user.ReplaceRolesInput) error {
				called = true
				return nil
			},
		})

		router := gin.New()
		router.PUT("/api/v1/admin/users/:id/roles", injectIdentity(user.Identity{UserID: 77}), handler.ReplaceRoles)

		rec := performJSON(t, router, http.MethodPut, "/api/v1/admin/users/15/roles", `{"roles":["admin"],"version":2}`)

		assertStatusCode(t, rec, http.StatusForbidden)
		assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodePermission))
		if called {
			t.Fatal("ReplaceRoles must not run without permission evidence")
		}
	})

	t.Run("permissions", func(t *testing.T) {
		called := false
		handler := NewUserHandler(fakeUserHandlerService{
			permissionsFn: func(_ context.Context, _ uint64) ([]string, error) {
				called = true
				return nil, nil
			},
		})

		router := gin.New()
		router.GET("/api/v1/admin/users/:id/permissions", injectIdentity(user.Identity{UserID: 77}), handler.Permissions)

		rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/users/15/permissions", nil, nil)

		assertStatusCode(t, rec, http.StatusForbidden)
		assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodePermission))
		if called {
			t.Fatal("Permissions must not run without permission evidence")
		}
	})
}

func injectIdentity(identity user.Identity) gin.HandlerFunc {
	return func(c *gin.Context) {
		setIdentity(c, identity)
		c.Next()
	}
}

func injectPermissions(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(string(identityPermissionsContextKey), permissions)
		c.Next()
	}
}

package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type userService interface {
	Me(context.Context, uint64) (*user.User, error)
	UpdateMe(context.Context, uint64, user.UpdateMeInput) (*user.User, error)
	List(context.Context, user.Actor, user.ListFilter) (user.Page, error)
	SetStatus(context.Context, user.Actor, user.SetStatusInput) error
	ReplaceRoles(context.Context, user.Actor, user.ReplaceRolesInput) error
	Permissions(context.Context, uint64) ([]string, error)
}

type userHandler struct {
	service userService
}

type updateMeRequest struct {
	Name    string `json:"name"`
	Version uint64 `json:"version"`
}

type setStatusRequest struct {
	Status  string `json:"status"`
	Version uint64 `json:"version"`
}

type replaceRolesRequest struct {
	Roles   []string `json:"roles"`
	Version uint64   `json:"version"`
}

type userDTO struct {
	ID        uint64    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Version   uint64    `json:"version"`
	Roles     []roleDTO `json:"roles,omitempty"`
	CreatedAt string    `json:"created_at,omitempty"`
	UpdatedAt string    `json:"updated_at,omitempty"`
}

type roleDTO struct {
	ID   uint64 `json:"id,omitempty"`
	Name string `json:"name"`
}

func NewUserHandler(service userService) *userHandler {
	return &userHandler{service: service}
}

func (h *userHandler) Me(c *gin.Context) {
	identity, ok := identityFromContext(c)
	if !ok {
		writeFailure(c, http.StatusUnauthorized, Problem{Code: CodeAuthentication, Message: "authentication failed"})
		return
	}
	current, err := h.service.Me(c.Request.Context(), identity.UserID)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, userToDTO(current))
}

func (h *userHandler) UpdateMe(c *gin.Context) {
	identity, ok := identityFromContext(c)
	if !ok {
		writeFailure(c, http.StatusUnauthorized, Problem{Code: CodeAuthentication, Message: "authentication failed"})
		return
	}

	var req updateMeRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	updated, err := h.service.UpdateMe(c.Request.Context(), identity.UserID, user.UpdateMeInput{
		Name:            req.Name,
		ExpectedVersion: req.Version,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, userToDTO(updated))
}

func (h *userHandler) List(c *gin.Context) {
	actor, ok := requireActorPermission(c, user.PermissionUsersRead)
	if !ok {
		return
	}

	query, problem := DecodePaginationQuery(c.Request.URL.Query(), QueryOptions{
		DefaultLimit: 20,
		AllowedSorts: []string{"id", "email", "name", "status", "created_at", "updated_at"},
	})
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	email, problem := optionalSingleton(c, "email")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}
	name, problem := optionalSingleton(c, "name")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}
	status, problem := optionalSingleton(c, "status")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}
	role, problem := optionalSingleton(c, "role")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	page, err := h.service.List(c.Request.Context(), actor, user.ListFilter{
		Email:      email,
		Name:       name,
		Status:     user.Status(status),
		Role:       role,
		Sort:       defaultSort(query.Sort),
		Descending: query.Direction == "desc",
		Limit:      query.Pagination.Limit,
		Offset:     query.Pagination.Offset,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}

	items := make([]userDTO, 0, len(page.Items))
	for i := range page.Items {
		items = append(items, userToDTO(&page.Items[i]))
	}
	writeSuccess(c, http.StatusOK, NewPageData(items, Pagination{Limit: page.Limit, Offset: page.Offset}))
}

func (h *userHandler) SetStatus(c *gin.Context) {
	actor, ok := requireActorPermission(c, user.PermissionUsersWrite)
	if !ok {
		return
	}
	id, problem := parseUintParam(c, "id")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	var req setStatusRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	if err := h.service.SetStatus(c.Request.Context(), actor, user.SetStatusInput{
		UserID:          id,
		Status:          user.Status(req.Status),
		ExpectedVersion: req.Version,
	}); err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, nil)
}

func (h *userHandler) ReplaceRoles(c *gin.Context) {
	actor, ok := requireActorPermission(c, user.PermissionUsersRoles)
	if !ok {
		return
	}
	id, problem := parseUintParam(c, "id")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	var req replaceRolesRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	if err := h.service.ReplaceRoles(c.Request.Context(), actor, user.ReplaceRolesInput{
		UserID:          id,
		Roles:           req.Roles,
		ExpectedVersion: req.Version,
	}); err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, nil)
}

func (h *userHandler) Permissions(c *gin.Context) {
	if _, _, ok := requireIdentityAndPermission(c, user.PermissionUsersRead); !ok {
		return
	}
	id, problem := parseUintParam(c, "id")
	if problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	permissions, err := h.service.Permissions(c.Request.Context(), id)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, gin.H{"permissions": permissions})
}

func userToDTO(value *user.User) userDTO {
	if value == nil {
		return userDTO{}
	}
	roles := make([]roleDTO, 0, len(value.Roles))
	for _, role := range value.Roles {
		// Only stable public role metadata is exposed here. Permission sets stay on
		// the dedicated endpoint so profile and list responses cannot over-share auth state.
		roles = append(roles, roleDTO{ID: role.ID, Name: role.Name})
	}
	return userDTO{
		ID:        value.ID,
		Email:     value.Email,
		Name:      value.Name,
		Status:    string(value.Status),
		Version:   value.Version,
		Roles:     roles,
		CreatedAt: formatUTCTime(value.CreatedAt),
		UpdatedAt: formatUTCTime(value.UpdatedAt),
	}
}

func formatUTCTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func optionalSingleton(c *gin.Context, key string) (string, *Problem) {
	value, ok, problem := singletonString(c.Request.URL.Query(), key)
	if problem != nil {
		return "", problem
	}
	if !ok {
		return "", nil
	}
	return strings.TrimSpace(value), nil
}

func defaultSort(value string) string {
	if strings.TrimSpace(value) == "" {
		return "created_at"
	}
	return value
}

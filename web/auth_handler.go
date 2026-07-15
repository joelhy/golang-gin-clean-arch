package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type webClock interface {
	Now() time.Time
}

type authRegistrationService interface {
	Register(context.Context, user.RegisterInput) (*user.User, error)
}

type authService interface {
	Login(context.Context, user.LoginInput) (user.Tokens, error)
	Refresh(context.Context, string) (user.Tokens, error)
	Logout(context.Context, user.Identity) error
	Authenticate(context.Context, string) (user.Identity, error)
	Authorize(context.Context, user.Identity, string) error
}

type authHandler struct {
	registration authRegistrationService
	auth         authService
	clock        webClock
}

type identityContextValue struct {
	Identity user.Identity
}

const (
	identityContextKey            contextKey = "auth_identity"
	identityPermissionsContextKey contextKey = "auth_permissions"
)

type registerRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenDTO struct {
	TokenType        string `json:"token_type"`
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
}

func NewAuthHandler(registration authRegistrationService, auth authService, clock webClock) *authHandler {
	if clock == nil {
		clock = systemWebClock{}
	}
	return &authHandler{registration: registration, auth: auth, clock: clock}
}

func (h *authHandler) Register(c *gin.Context) {
	var req registerRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	registered, err := h.registration.Register(c.Request.Context(), user.RegisterInput{
		Email:    req.Email,
		Name:     req.Name,
		Password: req.Password,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusCreated, userToDTO(registered))
}

func (h *authHandler) Login(c *gin.Context) {
	var req loginRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	tokens, err := h.auth.Login(c.Request.Context(), user.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, h.tokensToDTO(tokens))
}

func (h *authHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if problem := decodeJSON(c, &req, 0); problem != nil {
		writeFailure(c, http.StatusBadRequest, *problem)
		return
	}

	tokens, err := h.auth.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, h.tokensToDTO(tokens))
}

func (h *authHandler) Logout(c *gin.Context) {
	identity, ok := identityFromContext(c)
	if !ok {
		writeFailure(c, http.StatusUnauthorized, Problem{
			Code:    CodeAuthentication,
			Message: "authentication failed",
		})
		return
	}
	if err := h.auth.Logout(c.Request.Context(), identity); err != nil {
		writeProblem(c, err)
		return
	}
	writeSuccess(c, http.StatusOK, nil)
}

func (h *authHandler) Authenticated() gin.HandlerFunc {
	return func(c *gin.Context) {
		rawAccess, ok := bearerToken(c.Request.Header.Values("Authorization"))
		if !ok {
			writeFailure(c, http.StatusUnauthorized, Problem{
				Code:    CodeAuthentication,
				Message: "authentication failed",
			})
			return
		}

		identity, err := h.auth.Authenticate(c.Request.Context(), rawAccess)
		if err != nil {
			writeProblem(c, err)
			return
		}
		setIdentity(c, identity)
		c.Next()
	}
}

func (h *authHandler) RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := identityFromContext(c)
		if !ok {
			writeFailure(c, http.StatusUnauthorized, Problem{
				Code:    CodeAuthentication,
				Message: "authentication failed",
			})
			return
		}
		if err := h.auth.Authorize(c.Request.Context(), identity, permission); err != nil {
			writeProblem(c, err)
			return
		}
		// Store the granted permissions explicitly so downstream handlers can build
		// the narrow actor DTO expected by the domain without re-querying authority.
		appendPermission(c, permission)
		c.Next()
	}
}

func (h *authHandler) tokensToDTO(tokens user.Tokens) tokenDTO {
	now := h.clock.Now().UTC()
	return tokenDTO{
		TokenType:        "Bearer",
		AccessToken:      tokens.AccessToken,
		RefreshToken:     tokens.RefreshToken,
		ExpiresIn:        expiresIn(now, tokens.AccessExpiresAt),
		RefreshExpiresIn: expiresIn(now, tokens.RefreshExpiresAt),
	}
}

func expiresIn(now time.Time, expiry time.Time) int64 {
	seconds := int64(expiry.UTC().Sub(now).Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

func bearerToken(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}

	parts := strings.Fields(values[0])
	if len(parts) != 2 || parts[0] != "Bearer" || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

func setIdentity(c *gin.Context, identity user.Identity) {
	c.Set(string(identityContextKey), identityContextValue{Identity: identity})
}

func identityFromContext(c *gin.Context) (user.Identity, bool) {
	value, ok := c.Get(string(identityContextKey))
	if !ok {
		return user.Identity{}, false
	}
	typed, ok := value.(identityContextValue)
	if !ok {
		return user.Identity{}, false
	}
	return typed.Identity, true
}

func appendPermission(c *gin.Context, permission string) {
	if permission == "" {
		return
	}
	current := permissionsFromContext(c)
	for _, granted := range current {
		if granted == permission {
			return
		}
	}
	c.Set(string(identityPermissionsContextKey), append(current, permission))
}

func permissionsFromContext(c *gin.Context) []string {
	value, ok := c.Get(string(identityPermissionsContextKey))
	if !ok {
		return nil
	}
	permissions, ok := value.([]string)
	if !ok {
		return nil
	}
	return append([]string(nil), permissions...)
}

func actorFromContext(c *gin.Context) (user.Actor, bool) {
	identity, ok := identityFromContext(c)
	if !ok {
		return user.Actor{}, false
	}
	return user.Actor{UserID: identity.UserID, Permissions: permissionsFromContext(c)}, true
}

func parseUintParam(c *gin.Context, key string) (uint64, *Problem) {
	raw := strings.TrimSpace(c.Param(key))
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		problem := validationProblem(key, "invalid_integer")
		return 0, &problem
	}
	return value, nil
}

func writeProblem(c *gin.Context, err error) {
	status, problem := problemFromError(err)
	writeFailure(c, status, problem)
}

type systemWebClock struct{}

func (systemWebClock) Now() time.Time { return time.Now() }

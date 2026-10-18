package tenantv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Scope contains only URL parameters. Nil means absent; a pointer to an empty
// string means supplied but invalid and must never fall back to identity-only.
type Scope struct {
	TenantID  *string
	ProjectID *string
}

// AuthorizationConfig declares the requirements of a server route.
type AuthorizationConfig struct {
	Client        *Client
	SessionCookie string

	// Scope must extract only URL parameters, never headers or request bodies.
	// The net/http adapter requires this callback, even for identity-only routes.
	Scope func(*http.Request) (Scope, error)

	// RequireTenant rejects requests without a tenant URL parameter.
	RequireTenant bool

	// Action is the permission this route requires. A nonempty action requires
	// both tenant and project parameters. Project scopes require an action.
	Action AccessAction
}

// AuthorizationContext records the identity, validated scope and permission
// checked for this request. Action is empty for identity-only or tenant routes.
type AuthorizationContext struct {
	Principal *Principal
	TenantID  *string
	ProjectID *string
	Action    AccessAction
}

type authorizationKey struct{}

// AuthorizationFromContext returns the result of a successful Authorization.
func AuthorizationFromContext(ctx context.Context) (*AuthorizationContext, bool) {
	result, _ := ctx.Value(authorizationKey{}).(*AuthorizationContext)
	return result, result != nil
}

// Authorization authenticates every request and validates its URL scope before
// calling next. It does not check ownership of business resources loaded by next.
// Invalid route configuration fails closed with a sanitized 503 response.
func Authorization(config AuthorizationConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, err := config.authorize(r)
			if err != nil {
				writeAuthorizationError(w, err)
				return
			}
			ctx := WithPrincipal(r.Context(), result.Principal)
			ctx = context.WithValue(ctx, authorizationKey{}, result)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (config AuthorizationConfig) authorize(r *http.Request) (*AuthorizationContext, error) {
	if config.Client == nil || config.Client.transport == nil || config.Scope == nil ||
		(config.Action != "" && config.Action != AccessRead && config.Action != AccessWrite && config.Action != AccessManage) {
		return nil, errors.New("invalid authorization middleware configuration")
	}
	scope, err := config.Scope(r)
	if err != nil {
		// Extractors may include input values in errors; never expose them.
		return nil, &Error{Status: http.StatusBadRequest, Code: "INVALID_SCOPE", Message: "invalid URL scope"}
	}
	for _, id := range []*string{scope.TenantID, scope.ProjectID} {
		if id != nil && (*id == "" || *id != strings.TrimSpace(*id)) {
			return nil, &Error{
				Status:  http.StatusBadRequest,
				Code:    "INVALID_SCOPE",
				Message: "invalid URL scope",
			}
		}
	}
	if (scope.ProjectID != nil && scope.TenantID == nil) ||
		(config.RequireTenant && scope.TenantID == nil) ||
		(config.Action != "" && (scope.TenantID == nil || scope.ProjectID == nil)) {
		return nil, &Error{
			Status:  http.StatusBadRequest,
			Code:    "SCOPE_REQUIRED",
			Message: "required URL scope is missing",
		}
	}
	if scope.ProjectID != nil && config.Action == "" {
		return nil, errors.New("project authorization requires a configured action")
	}
	cookie := config.SessionCookie
	if cookie == "" {
		cookie = DefaultSessionCookie
	}
	proof, err := RequestProof(r, cookie)
	if err != nil {
		return nil, err
	}
	result := &AuthorizationContext{}
	if scope.TenantID == nil {
		result.Principal, err = config.Client.ResolveIdentity(r.Context(), proof)
		return result, err
	}
	resolved, err := config.Client.ResolveScope(r.Context(), &ResolveScopeRequest{
		Type: proof.Type, APIKey: proof.APIKey, Session: proof.Session,
		Method: r.Method, Origin: proof.Origin, CSRF: proof.CSRF,
		TenantID: *scope.TenantID, ProjectID: scope.ProjectID,
	})
	if err != nil {
		return nil, err
	}
	if resolved.Principal == nil || resolved.Principal.ID != resolved.UserID {
		return nil, errors.New("scope response has invalid principal")
	}
	result.Principal = resolved.Principal
	result.TenantID = &resolved.TenantID
	result.ProjectID = resolved.ProjectID
	if result.ProjectID == nil {
		return result, nil
	}
	access, err := config.Client.CheckAccess(r.Context(), &CheckAccessRequest{
		UserID: result.Principal.ID, TenantID: *result.TenantID,
		ProjectID: *result.ProjectID, Action: config.Action,
	})
	if err != nil {
		return nil, err
	}
	if !access.Allowed {
		return nil, &Error{
			Status:  http.StatusForbidden,
			Code:    "ACCESS_DENIED",
			Message: "project permission denied",
		}
	}
	if access.TenantID != *result.TenantID || access.ProjectID != *result.ProjectID {
		return nil, errors.New("access response has invalid scope")
	}
	result.Action = config.Action
	return result, nil
}

func writeAuthorizationError(w http.ResponseWriter, err error) {
	var detail *Error
	if errors.As(err, &detail) &&
		((detail.Status == http.StatusNotFound && detail.Code == "SCOPE_NOT_FOUND") ||
			(detail.Status == http.StatusUnprocessableEntity && detail.Code == "INVALID_SCOPE")) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(detail.Status)
		_ = json.NewEncoder(w).
			Encode(map[string]any{"error": map[string]any{"code": detail.Code, "message": detail.Message}})
		return
	}
	WriteIdentityError(w, err)
}

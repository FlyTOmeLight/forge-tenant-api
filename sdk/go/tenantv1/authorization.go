package tenantv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/FlyTOmeLight/server-go/constants"
)

// Scope is the scope a request selects. Nil means absent; a pointer to an
// empty string means supplied but invalid and must never fall back to
// identity-only. Neither source is trusted until ResolveScope verifies it.
type Scope struct {
	TenantID  *string
	ProjectID *string
}

// AuthorizationConfig declares the requirements of a server route.
type AuthorizationConfig struct {
	Client *Client

	// Scope extracts the URL selection. It may return an empty Scope for routes
	// without URL parameters; headers still apply. The net/http adapter requires
	// this callback, even for identity-only routes.
	Scope func(*http.Request) (Scope, error)

	// RequireTenant rejects requests without a tenant selection.
	RequireTenant bool

	// Action is the project permission this route requires. A nonempty action
	// requires a project selection. An empty action validates tenant membership
	// and project ownership only, without consulting project grants.
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

// Authorization authenticates every request and validates its selected scope
// before calling next. It does not check ownership of business resources loaded
// by next. Invalid route configuration fails closed with a sanitized 503 response.
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
	urlScope, err := config.Scope(r)
	if err != nil {
		// Extractors may include input values in errors; never expose them.
		return nil, &Error{Status: http.StatusBadRequest, Code: "INVALID_SCOPE", Message: "invalid scope"}
	}
	scope, err := mergeScope(urlScope, r)
	if err != nil {
		return nil, err
	}
	for _, id := range []*string{scope.TenantID, scope.ProjectID} {
		if id != nil && (*id == "" || *id != strings.TrimSpace(*id)) {
			return nil, &Error{
				Status:  http.StatusBadRequest,
				Code:    "INVALID_SCOPE",
				Message: "invalid scope",
			}
		}
	}
	if (scope.ProjectID != nil && scope.TenantID == nil) ||
		(config.RequireTenant && scope.TenantID == nil) ||
		(config.Action != "" && scope.ProjectID == nil) {
		return nil, &Error{
			Status:  http.StatusBadRequest,
			Code:    "SCOPE_REQUIRED",
			Message: "required scope is missing",
		}
	}
	proof, err := RequestProof(r, DefaultSessionCookie)
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
	if result.ProjectID == nil || config.Action == "" {
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

// mergeScope combines the URL selection with the scope headers. A dimension
// supplied by both sources must agree; otherwise the request is ambiguous and
// is rejected rather than resolved by an arbitrary precedence rule.
func mergeScope(scope Scope, r *http.Request) (Scope, error) {
	for _, candidate := range []struct {
		header string
		target **string
	}{
		{header: constants.HeaderTenantID, target: &scope.TenantID},
		{header: constants.HeaderProjectID, target: &scope.ProjectID},
	} {
		values := r.Header.Values(candidate.header)
		switch len(values) {
		case 0:
			continue
		case 1:
		default:
			return Scope{}, &Error{
				Status: http.StatusBadRequest, Code: "INVALID_SCOPE", Message: "invalid scope",
			}
		}
		value := values[0]
		if *candidate.target != nil && **candidate.target != value {
			return Scope{}, &Error{
				Status: http.StatusBadRequest, Code: "INVALID_SCOPE", Message: "invalid scope",
			}
		}
		*candidate.target = &value
	}
	return scope, nil
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

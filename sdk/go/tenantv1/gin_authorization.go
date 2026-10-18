package tenantv1

import (
	"net/http"

	"github.com/FlyTOmeLight/server-go/constants"
	"github.com/FlyTOmeLight/server-go/middleware"
	core "github.com/FlyTOmeLight/server-go/pkg"
)

// PublishAuthorizationContext copies verified identity and optional scope from the
// request context onto Gin keys. Tenant and project IDs come only from successful
// authorization, never from URL parameters or request headers.
func PublishAuthorizationContext(c *core.Context, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		return
	}
	c.Set(constants.KeyUserID, principal.ID)
	auth, ok := AuthorizationFromContext(r.Context())
	if !ok || auth == nil {
		return
	}
	if auth.TenantID != nil {
		// KeyOrgID remains the framework's verified tenant scope for idempotence
		// and impersonation; KeyTenantID exposes the same value by domain name.
		c.Set(constants.KeyOrgID, *auth.TenantID)
		c.Set(constants.KeyTenantID, *auth.TenantID)
	}
	if auth.ProjectID != nil {
		c.Set(constants.KeyProjectID, *auth.ProjectID)
	}
}

// GinAuthorization validates identity and URL scope before running later handlers.
// Without a Scope callback it reads the router's tid and pid path parameters.
func GinAuthorization(config AuthorizationConfig) core.HandlerFunc {
	return func(c *core.Context) {
		requestConfig := config
		if requestConfig.Scope == nil {
			requestConfig.Scope = func(*http.Request) (Scope, error) {
				var scope Scope
				if tid, ok := c.Params.Get("tid"); ok {
					scope.TenantID = &tid
				}
				if pid, ok := c.Params.Get("pid"); ok {
					scope.ProjectID = &pid
				}
				return scope, nil
			}
		}
		middleware.HTTPAuthorization(
			Authorization(requestConfig),
			PublishAuthorizationContext,
		)(c)
	}
}

// GinIdentity resolves the caller through forge-tenant and publishes the principal
// on both Gin keys and the request context.
func GinIdentity(config MiddlewareConfig) core.HandlerFunc {
	cookie := config.SessionCookie
	if cookie == "" {
		cookie = DefaultSessionCookie
	}
	return func(c *core.Context) {
		proof, err := RequestProof(c.Request, cookie)
		if err == nil {
			var principal *Principal
			if principal, err = config.Client.ResolveIdentity(c.Request.Context(), proof); err == nil {
				ctx := WithPrincipal(c.Request.Context(), principal)
				c.Request = c.Request.WithContext(ctx)
				c.Set(constants.KeyUserID, principal.ID)
				c.Next()
				return
			}
		}
		status, code := IdentityFailure(err)
		c.AbortWithStatusJSON(
			status,
			map[string]any{
				"error": map[string]any{
					"code":    code,
					"message": IdentityErrorMessage(err),
				},
			},
		)
	}
}

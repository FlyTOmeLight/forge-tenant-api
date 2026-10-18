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
		c.Set(constants.KeyTenantID, *auth.TenantID)
	}
	if auth.ProjectID != nil {
		c.Set(constants.KeyProjectID, *auth.ProjectID)
	}
}

// MiddlewareAuthorization validates identity and URL scope before running later handlers.
// Without a Scope callback it reads the router's tid and pid path parameters.
func MiddlewareAuthorization(config AuthorizationConfig) core.HandlerFunc {
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

// MiddlewareIdentity resolves the caller through forge-tenant and publishes the principal
// on both Gin keys and the request context. It is the Gin form of Identity; both share
// RequestProof and the error mapping, so cookie, bearer and failure behaviour match.
func MiddlewareIdentity(client *Client) core.HandlerFunc {
	handler := Identity(client)
	return func(c *core.Context) {
		done := false
		handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			done = true
			principal, _ := PrincipalFromContext(r.Context())
			c.Request = r
			c.Set(constants.KeyUserID, principal.ID)
			c.Next()
		})).ServeHTTP(c.Writer, c.Request)
		if !done {
			c.Abort()
		}
	}
}

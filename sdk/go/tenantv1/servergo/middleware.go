// Package servergo adapts the tenant identity middleware to server-go.
package servergo

import (
	"github.com/FlyTOmeLight/server-go/constants"
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

// Identity resolves the caller through forge-tenant and publishes the
// principal on both the gin keys and the request context, so handlers and
// service code read the same identity either way.
func Identity(config tenantv1.MiddlewareConfig) core.HandlerFunc {
	cookie := config.SessionCookie
	if cookie == "" {
		cookie = tenantv1.DefaultSessionCookie
	}
	return func(c *core.Context) {
		proof, err := tenantv1.RequestProof(c.Request, cookie)
		if err == nil {
			var principal *tenantv1.Principal
			if principal, err = config.Client.ResolveIdentity(c.Request.Context(), proof); err == nil {
				ctx := tenantv1.WithPrincipal(c.Request.Context(), principal)
				c.Request = c.Request.WithContext(ctx)
				c.Set(constants.KeyUserID, principal.ID)
				c.Next()
				return
			}
		}
		status, code := tenantv1.IdentityFailure(err)
		c.AbortWithStatusJSON(
			status,
			map[string]any{"error": map[string]any{"code": code, "message": err.Error()}},
		)
	}
}

package servergo

import (
	"net/http"

	"github.com/FlyTOmeLight/server-go/constants"
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

// Authorization validates identity and URL scope before running later handlers.
// Without a Scope callback it reads the router's tid and pid path parameters.
// Principal and AuthorizationContext are published on the request context;
// the framework user key contains the same verified principal ID.
func Authorization(config tenantv1.AuthorizationConfig) core.HandlerFunc {
	return func(c *core.Context) {
		requestConfig := config
		if requestConfig.Scope == nil {
			requestConfig.Scope = func(*http.Request) (tenantv1.Scope, error) {
				var scope tenantv1.Scope
				if tid, ok := c.Params.Get("tid"); ok {
					scope.TenantID = &tid
				}
				if pid, ok := c.Params.Get("pid"); ok {
					scope.ProjectID = &pid
				}
				return scope, nil
			}
		}
		passed := false
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			passed = true
			c.Request = r
			principal, _ := tenantv1.PrincipalFromContext(r.Context())
			c.Set(constants.KeyUserID, principal.ID)
			c.Next()
		})
		tenantv1.Authorization(requestConfig)(next).ServeHTTP(c.Writer, c.Request)
		if !passed {
			c.Abort()
		}
	}
}

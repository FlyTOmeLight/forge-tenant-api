package tenantv1

import (
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/FlyTOmeLight/server-go/auth"
	"github.com/FlyTOmeLight/server-go/constants"
	serrors "github.com/FlyTOmeLight/server-go/errors"
	core "github.com/FlyTOmeLight/server-go/pkg"
)

// InternalPropagationConfig configures the internal identity propagation
// middleware for services that receive calls from other internal services.
type InternalPropagationConfig struct {
	// ServiceTokens maps an internal caller's Bearer API key to its service
	// name. Requests without one of these keys are rejected before any
	// propagated identity header is read. Keys must follow the same minimum
	// length rules as auth.BearerTokens.
	ServiceTokens map[string]string

	// Authorize optionally rejects propagation for specific callers, such as
	// services that may not act on behalf of a user. It runs after the caller
	// is authenticated; auth.UserID returns the caller's service name.
	Authorize func(c *core.Context) error
}

// MiddlewareInternalPropagation adopts the identity an upstream internal service
// forwarded through x-impersonate-target-* headers. Install it on internal
// routes: it first authenticates the caller against ServiceTokens, then hands
// the verified tenant, user and project to auth.InternalPropagatedIdentity,
// which writes KeyTenantID, KeyUserID, KeyProjectID and KeyImpersonateOrigin*
// with KeyAuthMode "Internal". Handlers can treat an origin key as proof the
// identity was verified upstream, so no tenant resolution runs on this path.
//
// Requests that carry a service key but no propagation headers keep the
// service account as the identity. Services needing custom caller checks
// rather than a static token map can use auth.InternalPropagatedIdentity
// directly after installing their own caller authentication.
func MiddlewareInternalPropagation(config InternalPropagationConfig) (core.HandlerFunc, error) {
	if len(config.ServiceTokens) == 0 {
		return nil, errors.New("internal propagation requires at least one service token")
	}
	credentials := make(map[string]string, len(config.ServiceTokens))
	for token, service := range config.ServiceTokens {
		if token != "" && service != "" {
			credentials[token] = service
		}
	}
	if len(credentials) == 0 {
		return nil, errors.New("internal propagation requires nonempty service tokens")
	}
	authorize := config.Authorize
	if authorize == nil {
		authorize = func(*core.Context) error { return nil }
	}
	propagate, err := auth.InternalPropagatedIdentity(authorize)
	if err != nil {
		return nil, err
	}
	return func(c *core.Context) {
		token, hasBearer := bearerCredential(c.GetHeader("Authorization"))
		service := ""
		if hasBearer {
			for candidate, name := range credentials {
				if subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1 {
					service = name
					break
				}
			}
		}
		if service == "" {
			serrors.Abort(c, http.StatusUnauthorized, "Unauthorized", "unknown internal service key")
			return
		}
		c.Set(constants.KeyUserID, service)
		propagate(c)
	}, nil
}

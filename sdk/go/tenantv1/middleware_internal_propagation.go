package tenantv1

import (
	"context"
	"errors"
	"net/http"

	"github.com/FlyTOmeLight/server-go/auth"
	"github.com/FlyTOmeLight/server-go/constants"
	serrors "github.com/FlyTOmeLight/server-go/errors"
	core "github.com/FlyTOmeLight/server-go/pkg"
)

// ServiceAuthConfig configures how a downstream service checks that an inbound
// call comes from an internal service.
type ServiceAuthConfig struct {
	// Authenticate resolves the caller's Bearer service API key to its service
	// name, for example by checking it against forge-tenant.
	Authenticate func(context.Context, string) (string, error)

	// Authorize optionally rejects propagation for specific callers, such as
	// services that may not act on behalf of a user. It runs after the caller
	// is authenticated; auth.UserID returns the caller's service name. Only
	// MiddlewareInternalPropagation uses it.
	Authorize func(c *core.Context) error
}

func (config ServiceAuthConfig) authenticate(c *core.Context) (string, error) {
	if config.Authenticate == nil {
		return "", &Error{
			Status:  http.StatusUnauthorized,
			Code:    "SERVICE_AUTH_REQUIRED",
			Message: "missing service credential",
		}
	}
	token, hasBearer := bearerCredential(c.GetHeader("Authorization"))
	if !hasBearer || token == "" {
		return "", &Error{
			Status:  http.StatusUnauthorized,
			Code:    "SERVICE_AUTH_REQUIRED",
			Message: "missing service credential",
		}
	}
	service, err := config.Authenticate(c.Request.Context(), token)
	if err != nil {
		return "", err
	}
	if service == "" {
		return "", &Error{
			Status:  http.StatusUnauthorized,
			Code:    "SERVICE_AUTH_REQUIRED",
			Message: "unknown service credential",
		}
	}
	return service, nil
}

// abortServiceAuth rejects the caller. A credential rejection reported as
// *Error keeps its status, so a bad service key stays 401. Every other error,
// including a forge-tenant timeout or outage, becomes 503: the caller must be
// able to tell an infrastructure failure from a rejected credential.
func abortServiceAuth(c *core.Context, err error) {
	var detail *Error
	if errors.As(err, &detail) && detail.Status == http.StatusUnauthorized {
		serrors.Abort(c, http.StatusUnauthorized, "Unauthorized", "unknown internal service key")
		return
	}
	serrors.Abort(c, http.StatusServiceUnavailable, "ServiceUnavailable", identityUnavailableMessage)
}

// MiddlewareServiceAuth authenticates an internal caller by its service API key
// and publishes the service name as constants.KeyUserID. It does not read any
// user identity; install MiddlewareInternalPropagation after it when the caller
// forwards an end-user identity.
func MiddlewareServiceAuth(config ServiceAuthConfig) (core.HandlerFunc, error) {
	if config.Authenticate == nil {
		return nil, errors.New("service auth requires an Authenticate function")
	}
	return func(c *core.Context) {
		service, err := config.authenticate(c)
		if err != nil {
			abortServiceAuth(c, err)
			return
		}
		c.Set(constants.KeyUserID, service)
		c.Next()
	}, nil
}

// MiddlewareInternalPropagation adopts the identity an upstream internal service
// forwarded through x-impersonate-target-* headers. Install it after
// MiddlewareServiceAuth on the same route group: it authenticates the caller
// with the same config, publishes the service name as constants.KeyUserID, then
// hands the verified tenant, user and project to auth.InternalPropagatedIdentity.
// That writes KeyTenantID, KeyUserID, KeyProjectID and KeyImpersonateOrigin* with
// KeyAuthMode "Internal". Handlers can treat an origin key as proof the identity
// was verified upstream, so no tenant resolution runs on this path.
//
// Requests without propagation headers keep the service account as the
// identity. Authorize, when set, rejects propagation for specific callers.
func MiddlewareInternalPropagation(config ServiceAuthConfig) (core.HandlerFunc, error) {
	if config.Authenticate == nil {
		return nil, errors.New("internal propagation requires an Authenticate function")
	}
	propagate, err := auth.InternalPropagatedIdentity(func(c *core.Context) error {
		if config.Authorize == nil {
			return nil
		}
		return config.Authorize(c)
	})
	if err != nil {
		return nil, err
	}
	return func(c *core.Context) {
		service, err := config.authenticate(c)
		if err != nil {
			abortServiceAuth(c, err)
			return
		}
		c.Set(constants.KeyUserID, service)
		propagate(c)
	}, nil
}

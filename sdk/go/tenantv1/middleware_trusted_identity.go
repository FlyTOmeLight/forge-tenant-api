package tenantv1

import (
	"errors"
	"net/http"

	"github.com/FlyTOmeLight/server-go/auth"
	core "github.com/FlyTOmeLight/server-go/pkg"
)

// GatewayConfig authenticates the gateway that injects x-forge-* identity
// headers. The check must prove the request came from the gateway; it must not
// look at the identity headers.
type GatewayConfig struct {
	// Verify reports whether the request comes from the trusted gateway, for
	// example by checking a signed request, source network or mutual TLS. It
	// must not read the identity headers.
	Verify func(*http.Request) error
}

// MiddlewareTrustedIdentity adopts the identity a trusted gateway injected
// through x-forge-tenant-id, x-forge-user-id and the optional
// x-forge-project-id. Install it on routes that only the gateway can reach.
// It writes KeyTenantID, KeyUserID and KeyProjectID and does not call
// forge-tenant.
//
// It refuses a request that already carries an authenticated identity, so it
// must run before MiddlewareIdentity and MiddlewareAuthorization. A request
// that fails the gateway check is rejected even when identity headers are
// present, so a caller cannot forge them.
func MiddlewareTrustedIdentity(config GatewayConfig) (core.HandlerFunc, error) {
	if config.Verify == nil {
		return nil, errors.New("trusted identity headers require a Verify function")
	}
	return auth.TrustedIdentityHeaders(config.Verify)
}

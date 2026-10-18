package tenantv1

import "context"

type principalKey struct{}

// WithPrincipal attaches a resolved identity to a request context.
func WithPrincipal(ctx context.Context, principal *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// PrincipalFromContext reports the identity the middleware resolved for this
// request. It reports false instead of panicking when no identity is present.
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	principal, _ := ctx.Value(principalKey{}).(*Principal)
	return principal, principal != nil
}

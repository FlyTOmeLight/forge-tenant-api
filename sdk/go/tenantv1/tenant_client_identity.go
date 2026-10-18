package tenantv1

import (
	"context"
	"errors"
	"net/http"
)

// ResolveIdentity asks forge-tenant who the presented proof belongs to. The
// caller's own service API key authenticates the request; the body carries the
// end user's proof and never a self-reported user id.
func (c *Client) ResolveIdentity(ctx context.Context, request *ResolveIdentityRequest) (*Principal, error) {
	if request == nil {
		return nil, errors.New("ResolveIdentity request is required")
	}
	var result Principal
	if err := c.call(ctx, http.MethodPost, "/internal/v1/identity/resolve", request, &result); err != nil {
		return nil, err
	}
	if result.ID == "" {
		return nil, errors.New("tenant API identity response is missing the user id")
	}
	return &result, nil
}

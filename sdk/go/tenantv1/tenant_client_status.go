package tenantv1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// TenantStatus is the tenant-service view of one tenant's existence and state.
type TenantStatus struct {
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
}

// GetTenantStatus returns one tenant's current state. The response is only
// accepted when it names the requested tenant and carries a known state.
func (c *Client) GetTenantStatus(ctx context.Context, tenantID string) (*TenantStatus, error) {
	if c == nil || c.transport == nil {
		return nil, errors.New("tenant SDK client is not initialized")
	}
	if tenantID == "" || tenantID != strings.TrimSpace(tenantID) || len(tenantID) > 128 ||
		strings.ContainsAny(tenantID, "/?#") {
		return nil, &Error{
			Status:  http.StatusBadRequest,
			Code:    "INVALID_TENANT_ID",
			Message: "invalid tenant ID",
		}
	}
	var result TenantStatus
	path := "/internal/v1/tenants/" + url.PathEscape(tenantID) + "/status"
	if err := c.call(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	if result.TenantID != tenantID {
		return nil, fmt.Errorf("tenant API returned tenant %q for request %q", result.TenantID, tenantID)
	}
	switch result.Status {
	case "active", "suspended":
		return &result, nil
	default:
		return nil, fmt.Errorf("tenant API returned unknown tenant status %q", result.Status)
	}
}

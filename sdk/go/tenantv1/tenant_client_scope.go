package tenantv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// ResolveScope validates the original user proof and active tenant membership.
// Project selection checks ownership only, without consulting project grants.
func (c *Client) ResolveScope(ctx context.Context, request *ResolveScopeRequest) (*ResolveScopeResponse, error) {
	if request == nil {
		return nil, errors.New("ResolveScope request is required")
	}
	var wire struct {
		ResolveScopeResponse
		ProjectID json.RawMessage `json:"project_id"`
	}
	if err := c.call(ctx, http.MethodPost, "/internal/v1/scope/resolve", request, &wire); err != nil {
		return nil, err
	}
	// RawMessage distinguishes an omitted project_id from an explicit tenant scope null.
	if len(wire.ProjectID) == 0 {
		return nil, errors.New("tenant scope response is missing project_id")
	}
	result := wire.ResolveScopeResponse
	if err := json.Unmarshal(wire.ProjectID, &result.ProjectID); err != nil {
		return nil, errors.New("tenant scope response has invalid project_id")
	}
	if result.UserID != strings.TrimSpace(result.UserID) || strings.TrimSpace(result.UserID) == "" ||
		result.TenantID == "" ||
		result.TenantID != request.TenantID ||
		result.OwnershipState != "active" ||
		(result.ProjectID == nil) != (request.ProjectID == nil) ||
		(result.ProjectID != nil && (*result.ProjectID == "" || *result.ProjectID != *request.ProjectID)) {
		return nil, errors.New("tenant scope response has invalid identity, scope or ownership state")
	}
	return &result, nil
}

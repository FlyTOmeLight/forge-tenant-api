package tenantv1

import (
	"context"
	"errors"
	"net/http"
)

func (c *Client) CheckWorkspace(ctx context.Context, request *CheckWorkspaceRequest) (*CheckWorkspaceResponse, error) {
	if request == nil {
		return nil, errors.New("CheckWorkspace request is required")
	}
	var result CheckWorkspaceResponse
	if err := c.call(ctx, http.MethodPost, "/internal/v1/workspace/check", request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

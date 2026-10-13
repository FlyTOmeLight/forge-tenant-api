package tenantv1

import (
	"context"
	"errors"
	"net/http"
)

func (c *Client) ResolveProject(ctx context.Context, request *ResolveProjectRequest) (*ResolveProjectResponse, error) {
	if request == nil {
		return nil, errors.New("ResolveProject request is required")
	}
	var result ResolveProjectResponse
	if err := c.call(ctx, http.MethodPost, "/internal/v1/projects/resolve", request, &result); err != nil {
		return nil, err
	}
	if result.ID == "" {
		return nil, errors.New("tenant project response is missing the project ID")
	}
	return &result, nil
}

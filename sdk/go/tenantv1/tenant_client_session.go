package tenantv1

import (
	"context"
	"errors"
	"net/http"
)

func (c *Client) CheckSession(ctx context.Context, request *CheckSessionRequest) (*CheckSessionResponse, error) {
	if request == nil {
		return nil, errors.New("CheckSession request is required")
	}
	var result CheckSessionResponse
	if err := c.call(ctx, http.MethodPost, "/internal/v1/session/check", request, &result); err != nil {
		return nil, err
	}
	if result.ID == "" {
		return nil, errors.New("tenant session response is missing the user ID")
	}
	return &result, nil
}

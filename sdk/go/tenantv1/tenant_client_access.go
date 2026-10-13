package tenantv1

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

func (c *Client) CheckAccess(ctx context.Context, request *CheckAccessRequest) (*CheckAccessResponse, error) {
	if request == nil {
		return nil, errors.New("CheckAccess request is required")
	}
	var result CheckAccessResponse
	if err := c.call(ctx, http.MethodPost, "/internal/v1/access/check", request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListAccessEvents(
	ctx context.Context,
	request *ListAccessEventsRequest,
) (*ListAccessEventsResponse, error) {
	if request == nil {
		return nil, errors.New("ListAccessEvents request is required")
	}
	if request.After < 0 {
		return nil, errors.New("access event cursor must not be negative")
	}
	var result ListAccessEventsResponse
	path := "/internal/v1/access/events?after=" + strconv.FormatInt(request.After, 10)
	if err := c.call(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

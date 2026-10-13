package client

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

type LoginCookie struct {
	Value   string
	MaxAge  int
	Expires time.Time
}

type LoginResponse struct {
	Status  int
	Body    []byte
	Cookies []LoginCookie
}

// ProxyLogin forwards only the Casdoor identity cookie, never Forge sessions.
func (c *Casdoor) ProxyLogin(
	ctx context.Context,
	issuer, method, uri, identity string,
	body io.Reader,
) (LoginResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.URL+uri, body)
	if err != nil {
		return LoginResponse{}, err
	}
	target, err := url.Parse(issuer)
	if err != nil {
		return LoginResponse{}, err
	}
	req.Host = target.Host
	req.Header.Set("X-Forwarded-Proto", target.Scheme)
	req.Header.Set("Content-Type", "application/json")
	if identity != "" {
		req.AddCookie(&http.Cookie{Name: "casdoor_session_id", Value: identity})
	}
	response, err := c.HTTP.Do(req)
	if err != nil {
		return LoginResponse{}, err
	}
	defer response.Body.Close()
	result := LoginResponse{Status: response.StatusCode}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "casdoor_session_id" {
			result.Cookies = append(
				result.Cookies,
				LoginCookie{Value: cookie.Value, MaxAge: cookie.MaxAge, Expires: cookie.Expires},
			)
		}
	}
	result.Body, err = io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		result.Body = []byte(`{"status":"error","msg":"身份响应读取失败"}`)
	}
	return result, nil
}

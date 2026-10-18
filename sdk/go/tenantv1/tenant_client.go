// Package tenantv1 provides the trusted service API for Forge tenant authorization.
package tenantv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	frameworksdk "github.com/FlyTOmeLight/server-go/sdk"
)

type Config struct {
	Endpoint string

	// ServiceAPIKey is this service's own forge-tenant API key.
	ServiceAPIKey string

	// Timeout defaults to 15 seconds. Calls also respect their context deadline.
	Timeout time.Duration
}

type Client struct{ transport *frameworksdk.Client }

func New(config Config) (*Client, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Host == "" || !allowedEndpointScheme(endpoint) ||
		endpoint.User != nil ||
		endpoint.RawQuery != "" ||
		endpoint.Fragment != "" {
		return nil, errors.New(
			"tenant SDK endpoint must be an absolute HTTPS URL, or HTTP for a loopback host, without credentials, query or fragment",
		)
	}
	if len(config.ServiceAPIKey) < 32 || strings.ContainsAny(config.ServiceAPIKey, "\r\n") {
		return nil, errors.New(
			"tenant SDK service API key must contain at least 32 characters and no line breaks",
		)
	}
	if config.Timeout < 0 {
		return nil, errors.New("tenant SDK timeout must not be negative")
	}
	if config.Timeout == 0 {
		config.Timeout = 15 * time.Second
	}
	transport, err := frameworksdk.NewClientWithConfig(
		&frameworksdk.Config{
			Endpoint: strings.TrimRight(config.Endpoint, "/"),
			Credentials: &frameworksdk.Credentials{
				AuthType: frameworksdk.AuthTypeAPIKey,
				APIKey:   config.ServiceAPIKey,
			},
		},
	)
	if err != nil {
		return nil, err
	}
	transport.SetTimeout(config.Timeout)
	transport.Client.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{transport: transport}, nil
}

// Close releases the client's connections after its callers have finished.
func (c *Client) Close() error { return c.transport.Close() }

// allowedEndpointScheme accepts HTTPS to any host. Plain HTTP is accepted only
// for a loopback host, so service API keys never travel in cleartext to a
// remote peer.
func allowedEndpointScheme(endpoint *url.URL) bool {
	switch endpoint.Scheme {
	case "https":
		return true
	case "http":
		host := endpoint.Hostname()
		return host == "localhost" || net.ParseIP(host).IsLoopback()
	default:
		return false
	}
}

// Error is an HTTP failure. Network and context errors keep their original identity.
type Error struct {
	Status                   int
	Code, Message, RequestID string
}

func (e *Error) Error() string {
	return fmt.Sprintf("tenant API: %s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

func (c *Client) call(ctx context.Context, method, path string, body, result any) error {
	request := c.transport.R().SetContext(ctx).SetHeader("Accept", "application/json")
	if body != nil {
		request.SetBody(body).SetContentType("application/json")
	}
	response, err := request.Execute(method, path)
	if err != nil {
		return err
	}
	data := response.Bytes()
	if response.StatusCode() != http.StatusOK {
		// Forge's existing API wraps the framework error in an error property.
		var envelope struct {
			Error json.RawMessage `json:"error"`
		}
		payload := data
		if json.Unmarshal(data, &envelope) == nil && len(envelope.Error) > 0 {
			payload = envelope.Error
		}
		detail := frameworksdk.DecodeError(response.StatusCode(), bytes.NewReader(payload))
		id := detail.RequestID
		if id == "" {
			id = response.Header().Get("Request-ID")
		}
		return &Error{Status: detail.Status, Code: detail.Code, Message: detail.Message, RequestID: id}
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("decode tenant API response: %w", err)
	}
	// A JSON null is not a successful service response.
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("tenant API returned a null response")
	}
	return nil
}

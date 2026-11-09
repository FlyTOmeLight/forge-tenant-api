package tenantv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultServiceAuthenticatorTimeout = 3 * time.Second
	maxServiceAuthenticatorResponse    = 64 << 10
)

// ServiceAuthenticatorConfig configures the inbound service credential check.
type ServiceAuthenticatorConfig struct {
	Endpoint string
	Timeout  time.Duration
}

// ServiceAuthenticator verifies inbound service API keys against
// tenant-service. It owns one reusable transport but never stores a caller's
// key on the client.
type ServiceAuthenticator struct {
	endpoint string
	client   *http.Client
}

func NewServiceAuthenticator(config ServiceAuthenticatorConfig) (*ServiceAuthenticator, error) {
	endpoint, err := normalizeEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	if config.Timeout < 0 {
		return nil, errors.New("tenant SDK service authenticator timeout must not be negative")
	}
	if config.Timeout == 0 {
		config.Timeout = defaultServiceAuthenticatorTimeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &ServiceAuthenticator{
		endpoint: endpoint,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Close releases idle connections owned by the authenticator.
func (a *ServiceAuthenticator) Close() error {
	if a == nil || a.client == nil {
		return nil
	}
	if transport, ok := a.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}

// Authenticate exchanges a caller's service API key for its tenant-service
// service name. A rejected key keeps the SDK's 401 error so callers can avoid
// reporting a credential failure as an outage.
func (a *ServiceAuthenticator) Authenticate(ctx context.Context, serviceAPIKey string) (string, error) {
	if a == nil || a.client == nil {
		return "", errors.New("tenant SDK service authenticator is not initialized")
	}
	if serviceAPIKey == "" {
		return "", &Error{
			Status:  http.StatusUnauthorized,
			Code:    "SERVICE_AUTH_REQUIRED",
			Message: "missing service credential",
		}
	}
	if strings.ContainsAny(serviceAPIKey, "\r\n") {
		return "", &Error{
			Status:  http.StatusUnauthorized,
			Code:    "SERVICE_AUTH_REQUIRED",
			Message: "invalid service credential",
		}
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		a.endpoint+"/internal/v1/service/self",
		nil,
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+serviceAPIKey)
	request.Header.Set("Accept", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxServiceAuthenticatorResponse+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxServiceAuthenticatorResponse {
		return "", errors.New("tenant API service response is too large")
	}
	if response.StatusCode != http.StatusOK {
		detail := decodeErrorResponse(data, response.StatusCode, response.Header)
		if response.StatusCode == http.StatusUnauthorized {
			return "", detail
		}
		return "", fmt.Errorf("tenant API: %s (HTTP %d): %s", detail.Code, detail.Status, detail.Message)
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return "", errors.New("tenant API returned a null service response")
	}
	var result struct {
		Service string `json:"service"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decode tenant API service response: %w", err)
	}
	if strings.TrimSpace(result.Service) == "" {
		return "", errors.New("tenant API returned an empty service name")
	}
	return result.Service, nil
}

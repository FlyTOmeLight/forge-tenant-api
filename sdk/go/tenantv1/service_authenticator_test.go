package tenantv1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceAuthenticatorUsesCallerCredential(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/v1/service/self" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"service":"compute-control-plane"}`))
	}))
	defer server.Close()

	authenticator, err := NewServiceAuthenticator(ServiceAuthenticatorConfig{
		Endpoint: server.URL,
		Timeout:  250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer authenticator.Close()
	for _, key := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32)} {
		service, err := authenticator.Authenticate(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		if service != "compute-control-plane" {
			t.Fatalf("service = %q", service)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "Bearer "+strings.Repeat("a", 32) ||
		seen[1] != "Bearer "+strings.Repeat("b", 32) {
		t.Fatalf("authorization headers = %v", seen)
	}
}

func TestServiceAuthenticatorFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		is401  bool
	}{
		{
			name: "unknown key", status: http.StatusUnauthorized,
			body:  `{"error":{"code":"SERVICE_AUTH_REQUIRED","message":"rejected"}}`,
			is401: true,
		},
		{name: "server error", status: http.StatusInternalServerError, body: `{"error":{"code":"INTERNAL_ERROR"}}`},
		{name: "invalid JSON", status: http.StatusOK, body: `{`},
		{name: "empty service", status: http.StatusOK, body: `{"service":""}`},
		{name: "null body", status: http.StatusOK, body: `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			authenticator, err := NewServiceAuthenticator(ServiceAuthenticatorConfig{Endpoint: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			defer authenticator.Close()
			_, err = authenticator.Authenticate(t.Context(), strings.Repeat("k", 32))
			if err == nil {
				t.Fatal("failure accepted")
			}
			var detail *Error
			if tc.is401 {
				if !errors.As(err, &detail) || detail.Status != http.StatusUnauthorized {
					t.Fatalf("401 error = %#v", err)
				}
				return
			}
			if errors.As(err, &detail) {
				t.Fatalf("infrastructure failure mapped to typed rejection: %#v", err)
			}
		})
	}
}

func TestServiceAuthenticatorContextAndRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.test/other", http.StatusFound)
	}))
	defer server.Close()
	authenticator, err := NewServiceAuthenticator(ServiceAuthenticatorConfig{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer authenticator.Close()
	if _, err := authenticator.Authenticate(t.Context(), strings.Repeat("k", 32)); err == nil {
		t.Fatal("redirect was followed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authenticator.Authenticate(ctx, strings.Repeat("k", 32)); err == nil {
		t.Fatal("canceled context accepted")
	}
}

func TestServiceAuthenticatorConfiguration(t *testing.T) {
	for _, endpoint := range []string{"http://tenant.example.test", "ftp://tenant.example.test", "https://"} {
		if _, err := NewServiceAuthenticator(ServiceAuthenticatorConfig{Endpoint: endpoint}); err == nil {
			t.Fatalf("endpoint %q accepted", endpoint)
		}
	}
	authenticator, err := NewServiceAuthenticator(
		ServiceAuthenticatorConfig{Endpoint: "https://tenant.example.test"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer authenticator.Close()
	if authenticator.client.Timeout != defaultServiceAuthenticatorTimeout {
		t.Fatalf("timeout = %v", authenticator.client.Timeout)
	}
	if _, err := NewServiceAuthenticator(ServiceAuthenticatorConfig{
		Endpoint: "https://tenant.example.test",
		Timeout:  -time.Second,
	}); err == nil {
		t.Fatal("negative timeout accepted")
	}
}

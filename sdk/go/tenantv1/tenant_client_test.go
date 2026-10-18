package tenantv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{Endpoint: endpoint, ServiceAPIKey: strings.Repeat("s", 32)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return client
}

func TestRequestsAndResponses(t *testing.T) {
	for _, tc := range []struct {
		name, path, method, body, response string
		invoke                             func(context.Context, *Client) error
	}{
		{name: "identity", path: "/internal/v1/identity/resolve", method: "POST", body: `{"type":"session","session":"cookie","method":"POST","origin":"https://forge.test","csrf":"csrf"}`, response: `{"id":"user","name":"User","email":"user@example.test","operator":false}`, invoke: func(ctx context.Context, c *Client) error {
			v, e := c.ResolveIdentity(ctx, &ResolveIdentityRequest{Type: IdentityProofSession, Session: "cookie", Method: "POST", Origin: "https://forge.test", CSRF: "csrf"})
			if e == nil && v.ID != "user" {
				return errors.New("missing session identity")
			}
			return e
		}},
		{name: "project", path: "/internal/v1/projects/resolve", method: "POST", body: `{"tenant_id":"tenant","project_id":"project"}`, response: `{"id":"project","tenant_id":"tenant","path":"tenant/repo","gitlab_id":77}`, invoke: func(ctx context.Context, c *Client) error {
			v, e := c.ResolveProject(ctx, &ResolveProjectRequest{TenantID: "tenant", ProjectID: "project"})
			if e == nil && v.GitLabID != 77 {
				return errors.New("missing GitLab ID")
			}
			return e
		}},
		{name: "workspace", path: "/internal/v1/workspace/check", method: "POST", body: `{"user_id":"user","tenant_id":"tenant"}`, response: `{"allowed":true,"canEdit":true,"projects":[]}`, invoke: func(ctx context.Context, c *Client) error {
			v, e := c.CheckWorkspace(ctx, &CheckWorkspaceRequest{UserID: "user", TenantID: "tenant"})
			if e == nil && (!v.CanEdit || v.Projects == nil) {
				return errors.New("invalid workspace response")
			}
			return e
		}},
		{name: "access", path: "/internal/v1/access/check", method: "POST", body: `{"user_id":"user","tenant_id":"tenant","project_id":"project","action":"write"}`, response: `{"allowed":true,"level":30,"read":true,"write":true,"manage":false,"authorization_version":9,"tenant_id":"tenant","project_id":"project"}`, invoke: func(ctx context.Context, c *Client) error {
			v, e := c.CheckAccess(ctx, &CheckAccessRequest{UserID: "user", TenantID: "tenant", ProjectID: "project", Action: AccessWrite})
			if e == nil && (!v.Write || v.AuthorizationVersion != 9) {
				return errors.New("missing authorization facts")
			}
			return e
		}},
		{name: "events", path: "/internal/v1/access/events?after=7", method: "GET", body: "", response: `{"items":[{"id":8,"tenant_id":"tenant","version":9,"reason":"remove_member","created_at":"2026-10-13T12:00:00Z"}]}`, invoke: func(ctx context.Context, c *Client) error {
			v, e := c.ListAccessEvents(ctx, &ListAccessEventsRequest{After: 7})
			if e == nil && (len(v.Items) != 1 || v.Items[0].ID != 8 || v.Items[0].CreatedAt.IsZero()) {
				return errors.New("invalid event page")
			}
			return e
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.RequestURI() != tc.path {
					t.Errorf("request %s %s", r.Method, r.URL.RequestURI())
				}
				if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 32) {
					t.Error("missing service bearer")
				}
				if tc.body != "" {
					var actual, expected map[string]any
					if err := json.NewDecoder(r.Body).Decode(&actual); err != nil {
						t.Error(err)
					}
					_ = json.Unmarshal([]byte(tc.body), &expected)
					a, _ := json.Marshal(actual)
					b, _ := json.Marshal(expected)
					if string(a) != string(b) {
						t.Errorf("request body %s", a)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			if err := tc.invoke(t.Context(), newTestClient(t, server.URL)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFailures(t *testing.T) {
	if _, err := newTestClient(t, "https://example.test").ResolveIdentity(t.Context(), nil); err == nil || err.Error() != "ResolveIdentity request is required" {
		t.Fatalf("unexpected nil request error: %v", err)
	}
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{name: "service error", body: `{"error":{"code":"CSRF_REJECTED","message":"origin rejected"}}`, code: "CSRF_REJECTED", status: 403},
		{name: "framework error", body: `{"code":"Unauthorized","message":"invalid token"}`, code: "Unauthorized", status: 401},
		{name: "proxy HTML", body: "<html>unavailable</html>", code: "HTTPError", status: 502},
		{name: "empty error", body: "{}", code: "HTTPError", status: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Request-ID", "trace-123")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := newTestClient(t, server.URL).ResolveIdentity(t.Context(), &ResolveIdentityRequest{})
			var detail *Error
			if !errors.As(err, &detail) || detail.Status != tc.status || detail.Code != tc.code ||
				detail.RequestID != "trace-123" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
	for _, tc := range []struct{ name, body string }{
		{name: "not JSON", body: "not JSON"},
		{name: "null", body: "null"},
		{name: "empty object", body: "{}"},
		{name: "missing user id", body: `{"name":"User","email":"user@example.test","operator":false}`},
		{name: "multiple JSON values", body: "{} {}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) },
				),
			)
			defer server.Close()
			if _, err := newTestClient(
				t,
				server.URL,
			).ResolveIdentity(t.Context(), &ResolveIdentityRequest{}); err == nil {
				t.Fatal("invalid successful response accepted")
			}
		})
	}
}

func TestCancellationTimeoutAndConcurrency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in ResolveIdentityRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Session == "wait" {
			<-r.Context().Done()
			return
		}
		_ = json.NewEncoder(w).Encode(Principal{ID: in.Session})
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.ResolveIdentity(ctx, &ResolveIdentityRequest{Type: IdentityProofSession, Session: "wait"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	timeout, err := New(
		Config{Endpoint: server.URL, ServiceAPIKey: strings.Repeat("s", 32), Timeout: 20 * time.Millisecond},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer timeout.Close()
	if _, err = timeout.ResolveIdentity(
		t.Context(),
		&ResolveIdentityRequest{Type: IdentityProofSession, Session: "wait"},
	); !errors.Is(
		err,
		context.DeadlineExceeded,
	) {
		t.Fatalf("lost timeout: %v", err)
	}
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			session := fmt.Sprintf("session-%d", i)
			response, err := c.ResolveIdentity(t.Context(), &ResolveIdentityRequest{Type: IdentityProofSession, Session: session})
			if err != nil || response.ID != session {
				t.Errorf("shared request identity: %v", err)
			}
		})
	}
	group.Wait()
}

func TestEndpointSchemes(t *testing.T) {
	key := strings.Repeat("s", 32)
	for _, tc := range []struct {
		endpoint string
		allowed  bool
	}{
		{endpoint: "https://forge.example.test", allowed: true},
		{endpoint: "https://10.0.0.5:8443", allowed: true},
		{endpoint: "http://127.0.0.1:8080", allowed: true},
		{endpoint: "http://localhost:8080", allowed: true},
		{endpoint: "http://[::1]:8080", allowed: true},
		{endpoint: "http://forge.example.test", allowed: false},
		{endpoint: "http://10.0.0.5:8080", allowed: false},
		{endpoint: "http://[2001:db8::1]:8080", allowed: false},
		{endpoint: "ftp://127.0.0.1", allowed: false},
		{endpoint: "127.0.0.1:8080", allowed: false},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			client, err := New(Config{Endpoint: tc.endpoint, ServiceAPIKey: key})
			if tc.allowed {
				if err != nil {
					t.Fatalf("endpoint rejected: %v", err)
				}
				_ = client.Close()
				return
			}
			if err == nil {
				_ = client.Close()
				t.Fatal("endpoint accepted")
			}
		})
	}
}

func TestConstructorAndRedirect(t *testing.T) {
	for _, cfg := range []Config{{Endpoint: "relative", ServiceAPIKey: strings.Repeat("s", 32)}, {Endpoint: "https://user:password@example.test", ServiceAPIKey: strings.Repeat("s", 32)}, {Endpoint: "http://example.test?query=x", ServiceAPIKey: strings.Repeat("s", 32)}, {Endpoint: "http://example.test", ServiceAPIKey: "short"}, {Endpoint: "http://example.test", ServiceAPIKey: strings.Repeat("s", 32), Timeout: -1}} {
		if c, err := New(cfg); err == nil {
			_ = c.Close()
			t.Fatal("invalid SDK configuration accepted")
		}
	}
	target := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed service redirect") }),
	)
	defer target.Close()
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }),
	)
	defer server.Close()
	_, err := newTestClient(t, server.URL).ResolveIdentity(t.Context(), &ResolveIdentityRequest{})
	var failure *Error
	if !errors.As(err, &failure) || failure.Status != 302 {
		t.Fatalf("lost redirect status: %v", err)
	}
}

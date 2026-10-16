package tenantv1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveScope(t *testing.T) {
	for _, project := range []*string{nil, scopePointer("p")} {
		name := "tenant"
		if project != nil {
			name = "project"
		}
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/internal/v1/scope/resolve" ||
					r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 32) {
					t.Error("scope request path or service identity changed")
				}
				var in map[string]any
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				if len(in) != 6 || in["session"] != "session" || in["method"] != "POST" ||
					in["origin"] != "https://forge.test" ||
					in["csrf"] != "csrf" ||
					in["tenant_id"] != "t" {
					t.Errorf("scope wire fields: %#v", in)
				}
				v, ok := in["project_id"]
				if !ok || (project == nil && v != nil) || (project != nil && v != *project) {
					t.Errorf("project_id not exact: %#v", in)
				}
				_ = json.NewEncoder(w).
					Encode(ResolveScopeResponse{UserID: "u", TenantID: "t", ProjectID: project, OwnershipState: "active"})
			}))
			defer server.Close()
			out, err := newTestClient(
				t,
				server.URL,
			).ResolveScope(t.Context(), &ResolveScopeRequest{Session: "session", Method: "POST", Origin: "https://forge.test", CSRF: "csrf", TenantID: "t", ProjectID: project})
			if err != nil || out.UserID != "u" {
				t.Fatalf("scope: %+v %v", out, err)
			}
		})
	}
}

func TestResolveScopeInvalidResponse(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`,
		`{"user_id":"u","tenant_id":"t","ownership_state":"active"}`,
		`{"user_id":" u ","tenant_id":"t","project_id":null,"ownership_state":"active"}`,
		`{"user_id":"u","tenant_id":"other","project_id":null,"ownership_state":"active"}`,
		`{"user_id":"u","tenant_id":"t","project_id":null,"ownership_state":"deleted"}`,
		`{"user_id":"u","tenant_id":"t","project_id":"p","ownership_state":"active"}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) },
				),
			)
			defer server.Close()
			_, err := newTestClient(
				t,
				server.URL,
			).ResolveScope(t.Context(), &ResolveScopeRequest{TenantID: "t"})
			if err == nil {
				t.Fatal("invalid scope accepted")
			}
		})
	}
	c := newTestClient(t, "http://localhost:1")
	if _, err := c.ResolveScope(t.Context(), nil); err == nil {
		t.Fatal("nil request accepted")
	}
}

func scopePointer(v string) *string { return &v }

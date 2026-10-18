package servergo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/FlyTOmeLight/server-go/constants"
	core "github.com/FlyTOmeLight/server-go/pkg"
	"github.com/gin-gonic/gin"

	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func TestAuthorizationPublishesScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, route, path, action string
		cookie, custom            bool
	}{
		{name: "identity bearer", route: "/me", path: "/me"},
		{name: "tenant cookie", route: "/tenants/:tid", path: "/tenants/tenant", cookie: true},
		{name: "project bearer", route: "/tenants/:tid/projects/:pid", path: "/tenants/tenant/projects/project", action: "write"},
		{name: "custom URL extractor", route: "/orgs/:org/repos/:repo", path: "/orgs/tenant/repos/project", action: "read", custom: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var identityCalls, scopeCalls, accessCalls atomic.Int32
			tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/internal/v1/identity/resolve":
					identityCalls.Add(1)
					_ = json.NewEncoder(w).Encode(tenantv1.Principal{ID: "user"})
				case "/internal/v1/scope/resolve":
					scopeCalls.Add(1)
					var in tenantv1.ResolveScopeRequest
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					if in.TenantID != "tenant" || in.Method != http.MethodPost {
						t.Errorf("scope = %+v", in)
					}
					if tc.cookie {
						if in.Type != tenantv1.IdentityProofSession ||
							in.Session != "session" ||
							in.Origin != "https://service.test" ||
							in.CSRF != "csrf" {
							t.Errorf("cookie proof = %+v", in)
						}
					} else if in.Type != tenantv1.IdentityProofAPIKey || in.APIKey != "user-key" {
						t.Errorf("API key proof = %+v", in)
					}
					_ = json.NewEncoder(w).Encode(tenantv1.ResolveScopeResponse{
						UserID:         "user",
						Principal:      &tenantv1.Principal{ID: "user"},
						TenantID:       in.TenantID,
						ProjectID:      in.ProjectID,
						OwnershipState: "active",
					})
				case "/internal/v1/access/check":
					accessCalls.Add(1)
					var in tenantv1.CheckAccessRequest
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					if in.UserID != "user" || in.TenantID != "tenant" ||
						in.ProjectID != "project" ||
						string(in.Action) != tc.action {
						t.Errorf("access = %+v", in)
					}
					_ = json.NewEncoder(w).
						Encode(tenantv1.CheckAccessResponse{Allowed: true, TenantID: "tenant", ProjectID: "project"})
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer tenant.Close()
			config := tenantv1.AuthorizationConfig{
				Client: newTenantClient(t, tenant.URL),
				Action: tenantv1.AccessAction(tc.action),
			}
			engine := gin.New()
			var extract core.HandlerFunc = func(c *core.Context) { c.Next() }
			if tc.custom {
				extract = func(c *core.Context) {
					c.Request.SetPathValue("org", c.Param("org"))
					c.Request.SetPathValue("repo", c.Param("repo"))
					c.Next()
				}
				config.Scope = func(r *http.Request) (tenantv1.Scope, error) {
					tid, pid := r.PathValue("org"), r.PathValue("repo")
					return tenantv1.Scope{TenantID: &tid, ProjectID: &pid}, nil
				}
			}
			handled := false
			engine.POST(tc.route, extract, Authorization(config), func(c *core.Context) {
				handled = true
				auth, ok := tenantv1.AuthorizationFromContext(c.Request.Context())
				principal, identityOK := tenantv1.PrincipalFromContext(c.Request.Context())
				if !ok || !identityOK || auth.Principal.ID != "user" || principal.ID != "user" ||
					c.GetString(constants.KeyUserID) != "user" {
					t.Fatalf("missing verified identity: %+v", auth)
				}
				if string(auth.Action) != tc.action {
					t.Errorf("action=%s", auth.Action)
				}
				if tc.route == "/me" {
					if auth.TenantID != nil || auth.ProjectID != nil {
						t.Errorf("unexpected scope: %+v", auth)
					}
				} else if auth.TenantID == nil || *auth.TenantID != "tenant" {
					t.Errorf("tenant scope: %+v", auth)
				}
				if tc.action != "" && (auth.ProjectID == nil || *auth.ProjectID != "project") {
					t.Errorf("project scope: %+v", auth)
				}
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(
				http.MethodPost,
				"https://service.test"+tc.path+"?tid=forged&pid=forged",
				nil,
			)
			request.Header.Set("X-User-ID", "forged")
			request.Header.Set("X-Tenant-ID", "forged")
			request.Header.Set("X-Project-ID", "forged")
			if tc.cookie {
				request.AddCookie(&http.Cookie{Name: tenantv1.DefaultSessionCookie, Value: "session"})
				request.Header.Set("Origin", "https://service.test")
				request.Header.Set("X-CSRF-Token", "csrf")
			} else {
				request.Header.Set("Authorization", "Bearer user-key")
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if !handled || response.Code != http.StatusNoContent {
				t.Fatalf("handled=%v status=%d body=%s", handled, response.Code, response.Body.String())
			}
			if tc.route == "/me" {
				if identityCalls.Load() != 1 || scopeCalls.Load() != 0 {
					t.Error("incorrect identity calls")
				}
			} else if identityCalls.Load() != 0 || scopeCalls.Load() != 1 {
				t.Error("scoped request repeated identity authentication")
			}
			if (accessCalls.Load() == 1) != (tc.action != "") {
				t.Error("incorrect permission calls")
			}
		})
	}
}

func TestAuthorizationAbortsRejectedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, route, path, action, response string
		requireTenant, nilClient            bool
		upstreamStatus, wantStatus          int
	}{
		{name: "tenant missing", route: "/me", path: "/me", requireTenant: true, wantStatus: 400},
		{name: "project missing", route: "/tenants/:tid", path: "/tenants/tenant", action: "read", wantStatus: 400},
		{name: "tenant absent with project", route: "/projects/:pid", path: "/projects/project", action: "read", wantStatus: 400},
		{name: "empty supplied scope", route: "/tenants/:tid", path: "/tenants/%20", wantStatus: 400},
		{name: "nil client", route: "/me", path: "/me", nilClient: true, wantStatus: 503},
		{name: "missing project action", route: "/tenants/:tid/projects/:pid", path: "/tenants/tenant/projects/project", wantStatus: 503},
		{name: "wrong project", route: "/tenants/:tid/projects/:pid", path: "/tenants/tenant/projects/project", action: "read", upstreamStatus: 404, response: `{"error":{"code":"SCOPE_NOT_FOUND","message":"scope missing"}}`, wantStatus: 404},
		{name: "permission denied", route: "/tenants/:tid/projects/:pid", path: "/tenants/tenant/projects/project", action: "manage", response: `{"allowed":false}`, wantStatus: 403},
		{name: "service unavailable", route: "/tenants/:tid", path: "/tenants/tenant", upstreamStatus: 500, response: `{"error":{"code":"INTERNAL","message":"secret"}}`, wantStatus: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.upstreamStatus != 0 {
					w.WriteHeader(tc.upstreamStatus)
					_, _ = w.Write([]byte(tc.response))
					return
				}
				if r.URL.Path == "/internal/v1/scope/resolve" {
					_, _ = w.Write(
						[]byte(
							`{"user_id":"user","principal":{"id":"user"},"tenant_id":"tenant","project_id":"project","ownership_state":"active"}`,
						),
					)
					return
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer tenant.Close()
			config := tenantv1.AuthorizationConfig{
				Client:        newTenantClient(t, tenant.URL),
				RequireTenant: tc.requireTenant,
				Action:        tenantv1.AccessAction(tc.action),
			}
			if tc.nilClient {
				config.Client = nil
			}
			engine := gin.New()
			engine.GET(
				tc.route,
				Authorization(config),
				func(c *core.Context) { t.Error("rejected request reached handler") },
			)
			request := httptest.NewRequest(http.MethodGet, "http://service.test"+tc.path, nil)
			request.Header.Set("Authorization", "Bearer user-key")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != tc.wantStatus || strings.Contains(response.Body.String(), "secret") {
				t.Errorf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

package tenantv1

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
)

func TestMiddlewareAuthorizationPublishesScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, route, path, action string
		cookie, custom            bool
	}{
		{name: "identity bearer", route: "/me", path: "/me"},
		{name: "tenant cookie", route: "/tenants/:tid", path: "/tenants/tenant", cookie: true},
		{name: "project bearer", route: "/tenants/:tid/projects/:pid", path: "/tenants/tenant/projects/project", action: "write"},
		{name: "project without action", route: "/tenants/:tid/projects/:pid", path: "/tenants/tenant/projects/project"},
		{name: "custom URL extractor", route: "/orgs/:org/repos/:repo", path: "/orgs/tenant/repos/project", action: "read", custom: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var identityCalls, scopeCalls, accessCalls atomic.Int32
			tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/internal/v1/identity/resolve":
					identityCalls.Add(1)
					_ = json.NewEncoder(w).Encode(Principal{ID: "user"})
				case "/internal/v1/scope/resolve":
					scopeCalls.Add(1)
					var in ResolveScopeRequest
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					if in.TenantID != "tenant" || in.Method != http.MethodPost {
						t.Errorf("scope = %+v", in)
					}
					if tc.cookie {
						if in.Type != IdentityProofSession ||
							in.Session != "session" ||
							in.Origin != "https://service.test" ||
							in.CSRF != "csrf" {
							t.Errorf("cookie proof = %+v", in)
						}
					} else if in.Type != IdentityProofAPIKey || in.APIKey != "user-key" {
						t.Errorf("API key proof = %+v", in)
					}
					_ = json.NewEncoder(w).Encode(ResolveScopeResponse{
						UserID:         "user",
						Principal:      &Principal{ID: "user"},
						TenantID:       in.TenantID,
						ProjectID:      in.ProjectID,
						OwnershipState: "active",
					})
				case "/internal/v1/access/check":
					accessCalls.Add(1)
					var in CheckAccessRequest
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					if in.UserID != "user" || in.TenantID != "tenant" ||
						in.ProjectID != "project" ||
						string(in.Action) != tc.action {
						t.Errorf("access = %+v", in)
					}
					_ = json.NewEncoder(w).
						Encode(CheckAccessResponse{Allowed: true, TenantID: "tenant", ProjectID: "project"})
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer tenant.Close()
			config := AuthorizationConfig{
				Client: newGinTestClient(t, tenant.URL),
				Action: AccessAction(tc.action),
			}
			engine := gin.New()
			var extract core.HandlerFunc = func(c *core.Context) { c.Next() }
			if tc.custom {
				extract = func(c *core.Context) {
					c.Request.SetPathValue("org", c.Param("org"))
					c.Request.SetPathValue("repo", c.Param("repo"))
					c.Next()
				}
				config.Scope = func(r *http.Request) (Scope, error) {
					tid, pid := r.PathValue("org"), r.PathValue("repo")
					return Scope{TenantID: &tid, ProjectID: &pid}, nil
				}
			}
			handled := false
			engine.POST(tc.route, extract, MiddlewareAuthorization(config), func(c *core.Context) {
				handled = true
				auth, ok := AuthorizationFromContext(c.Request.Context())
				principal, identityOK := PrincipalFromContext(c.Request.Context())
				if !ok || !identityOK || auth.Principal.ID != "user" || principal.ID != "user" ||
					c.GetString(constants.KeyUserID) != "user" {
					t.Fatalf("missing verified identity: %+v", auth)
				}
				if string(auth.Action) != tc.action {
					t.Errorf("action=%s", auth.Action)
				}
				if tc.route == "/me" {
					if auth.TenantID != nil || auth.ProjectID != nil ||
						c.GetString(constants.KeyTenantID) != "" ||
						c.GetString(constants.KeyProjectID) != "" {
						t.Errorf(
							"unexpected scope: %+v keys tenant=%q project=%q",
							auth,
							c.GetString(constants.KeyTenantID),
							c.GetString(constants.KeyProjectID),
						)
					}
				} else if auth.TenantID == nil || *auth.TenantID != "tenant" ||
					c.GetString(constants.KeyTenantID) != "tenant" {
					t.Errorf("tenant scope: %+v key=%q", auth, c.GetString(constants.KeyTenantID))
				}
				if strings.Contains(tc.route, "/projects/") {
					if auth.ProjectID == nil || *auth.ProjectID != "project" ||
						c.GetString(constants.KeyProjectID) != "project" {
						t.Errorf(
							"project scope: %+v key=%q",
							auth,
							c.GetString(constants.KeyProjectID),
						)
					}
				}
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(
				http.MethodPost,
				"https://service.test"+tc.path+"?tid=forged&pid=forged",
				nil,
			)
			request.Header.Set(constants.HeaderUserID, "forged")
			// Scope headers agree with the route parameters; agreement must pass.
			if tc.route != "/me" {
				request.Header.Set(constants.HeaderTenantID, "tenant")
			}
			if strings.Contains(tc.route, "/projects/") {
				request.Header.Set(constants.HeaderProjectID, "project")
			}
			if tc.cookie {
				request.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "session"})
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

func TestMiddlewareAuthorizationAbortsRejectedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, route, path, action, response string
		headerTenant, headerProject         string
		requireTenant, nilClient            bool
		upstreamStatus, wantStatus          int
	}{
		{name: "tenant missing", route: "/me", path: "/me", requireTenant: true, wantStatus: 400},
		{name: "scope header conflict", route: "/tenants/:tid", path: "/tenants/tenant", headerTenant: "other", wantStatus: 400},
		{name: "project missing", route: "/tenants/:tid", path: "/tenants/tenant", action: "read", wantStatus: 400},
		{name: "tenant absent with project", route: "/projects/:pid", path: "/projects/project", action: "read", wantStatus: 400},
		{name: "empty supplied scope", route: "/tenants/:tid", path: "/tenants/%20", wantStatus: 400},
		{name: "nil client", route: "/me", path: "/me", nilClient: true, wantStatus: 503},
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
			config := AuthorizationConfig{
				Client:        newGinTestClient(t, tenant.URL),
				RequireTenant: tc.requireTenant,
				Action:        AccessAction(tc.action),
			}
			if tc.nilClient {
				config.Client = nil
			}
			engine := gin.New()
			engine.GET(
				tc.route,
				MiddlewareAuthorization(config),
				func(c *core.Context) { t.Error("rejected request reached handler") },
			)
			request := httptest.NewRequest(http.MethodGet, "http://service.test"+tc.path, nil)
			request.Header.Set("Authorization", "Bearer user-key")
			if tc.headerTenant != "" {
				request.Header.Set(constants.HeaderTenantID, tc.headerTenant)
			}
			if tc.headerProject != "" {
				request.Header.Set(constants.HeaderProjectID, tc.headerProject)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != tc.wantStatus || strings.Contains(response.Body.String(), "secret") {
				t.Errorf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMiddlewareIdentityPublishesSameUserOnGinAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in ResolveIdentityRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if in.Type != IdentityProofAPIKey || in.APIKey != "user-api-key" {
			t.Errorf("unexpected identity proof: %+v", in)
		}
		_ = json.NewEncoder(w).
			Encode(Principal{ID: "user-1", Name: "User", Email: "user@example.test"})
	}))
	defer tenant.Close()
	client := newGinTestClient(t, tenant.URL)

	handled := 0
	engine := gin.New()
	engine.GET("/resource", MiddlewareIdentity(client), func(c *core.Context) {
		handled++
		ginUser := c.GetString(constants.KeyUserID)
		principal, ok := PrincipalFromContext(c.Request.Context())
		if !ok || principal.ID != "user-1" || ginUser != principal.ID {
			t.Errorf("identity mismatch: gin=%q context=%+v present=%v", ginUser, principal, ok)
		}
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "http://service.test/resource", nil)
	request.Header.Set("Authorization", "Bearer user-api-key")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || handled != 1 {
		t.Fatalf("status %d, handler runs %d", response.Code, handled)
	}
}

func newGinTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{Endpoint: endpoint, ServiceAPIKey: strings.Repeat("s", 32)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

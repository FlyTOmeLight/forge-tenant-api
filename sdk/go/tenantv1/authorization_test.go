package tenantv1

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAuthorizationIdentityAndScope(t *testing.T) {
	for _, credential := range []string{"cookie", "bearer"} {
		for _, action := range []string{"identity", "tenant", "read", "write", "manage"} {
			t.Run(credential+"/"+action, func(t *testing.T) {
				var identityCalls, scopeCalls, accessCalls atomic.Int32
				principal := Principal{ID: "user", Name: "User", Operator: true}
				proof := ResolveIdentityRequest{Type: IdentityProofAPIKey, APIKey: "user-key"}
				if credential == "cookie" {
					proof = ResolveIdentityRequest{
						Type: IdentityProofSession, Session: "session", Method: http.MethodPost,
						Origin: "https://service.test", CSRF: "csrf",
					}
				}
				scope := Scope{}
				if action != "identity" {
					scope.TenantID = stringPointer("tenant")
				}
				var permission AccessAction
				if action != "identity" && action != "tenant" {
					scope.ProjectID = stringPointer("project")
					permission = AccessAction(action)
				}
				server := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodPost {
							t.Errorf("method = %s", r.Method)
						}
						switch r.URL.Path {
						case "/internal/v1/identity/resolve":
							identityCalls.Add(1)
							var in ResolveIdentityRequest
							if err := json.NewDecoder(r.Body).
								Decode(&in); err != nil ||
								in != proof {
								t.Errorf("identity proof = %+v, err = %v", in, err)
							}
							_ = json.NewEncoder(w).Encode(principal)
						case "/internal/v1/scope/resolve":
							scopeCalls.Add(1)
							var in ResolveScopeRequest
							want := ResolveScopeRequest{
								Type:      proof.Type,
								APIKey:    proof.APIKey,
								Session:   proof.Session,
								Method:    http.MethodPost,
								Origin:    proof.Origin,
								CSRF:      proof.CSRF,
								TenantID:  "tenant",
								ProjectID: scope.ProjectID,
							}
							if err := json.NewDecoder(r.Body).
								Decode(&in); err != nil ||
								!reflect.DeepEqual(in, want) {
								t.Errorf(
									"scope proof = %+v, want %+v, err = %v",
									in,
									want,
									err,
								)
							}
							_ = json.NewEncoder(w).Encode(ResolveScopeResponse{
								UserID:         principal.ID,
								Principal:      &principal,
								TenantID:       "tenant",
								ProjectID:      scope.ProjectID,
								OwnershipState: "active",
							})
						case "/internal/v1/access/check":
							accessCalls.Add(1)
							var in CheckAccessRequest
							want := CheckAccessRequest{
								UserID:    "user",
								TenantID:  "tenant",
								ProjectID: "project",
								Action:    permission,
							}
							if err := json.NewDecoder(r.Body).
								Decode(&in); err != nil ||
								in != want {
								t.Errorf(
									"access request = %+v, want %+v, err = %v",
									in,
									want,
									err,
								)
							}
							_ = json.NewEncoder(w).
								Encode(CheckAccessResponse{Allowed: true, TenantID: "tenant", ProjectID: "project"})
						default:
							t.Errorf("unexpected path %s", r.URL.Path)
							w.WriteHeader(http.StatusNotFound)
						}
					}),
				)
				defer server.Close()
				config := AuthorizationConfig{
					Client:        newTestClient(t, server.URL),
					Scope:         func(*http.Request) (Scope, error) { return scope, nil },
					RequireTenant: action == "tenant", Action: permission,
				}
				handled := false
				handler := Authorization(
					config,
				)(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						handled = true
						got, ok := AuthorizationFromContext(r.Context())
						want := &AuthorizationContext{
							Principal: &principal,
							TenantID:  scope.TenantID,
							ProjectID: scope.ProjectID,
							Action:    permission,
						}
						if !ok || !reflect.DeepEqual(got, want) {
							t.Errorf("authorization = %+v, want %+v", got, want)
						}
						identity, ok := PrincipalFromContext(r.Context())
						if !ok || *identity != principal {
							t.Errorf("principal = %+v", identity)
						}
						w.WriteHeader(http.StatusNoContent)
					}),
				)
				request := httptest.NewRequest(
					http.MethodPost,
					"https://service.test/resources?tenant_id=forged&project_id=forged",
					nil,
				)
				request.Header.Set("X-User-ID", "forged")
				request.Header.Set("X-Tenant-ID", "forged")
				request.Header.Set("X-Project-ID", "forged")
				request.Header.Set("X-Access-Action", "manage")
				if credential == "cookie" {
					request.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "session"})
					request.Header.Set("Origin", proof.Origin)
					request.Header.Set("X-CSRF-Token", proof.CSRF)
				} else {
					request.Header.Set("Authorization", "Bearer user-key")
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if !handled || response.Code != http.StatusNoContent {
					t.Fatalf(
						"handler=%v status=%d body=%s",
						handled,
						response.Code,
						response.Body.String(),
					)
				}
				wantIdentity, wantScope, wantAccess := int32(0), int32(0), int32(0)
				if action == "identity" {
					wantIdentity = 1
				} else {
					wantScope = 1
				}
				if permission != "" {
					wantAccess = 1
				}
				if identityCalls.Load() != wantIdentity || scopeCalls.Load() != wantScope ||
					accessCalls.Load() != wantAccess {
					t.Errorf(
						"calls identity=%d scope=%d access=%d",
						identityCalls.Load(),
						scopeCalls.Load(),
						accessCalls.Load(),
					)
				}
			})
		}
	}
}

func TestAuthorizationRejectsLocally(t *testing.T) {
	for _, tc := range []struct {
		name, tenant, project, action, credential, wantCode               string
		requireTenant, noExtractor, nilClient, zeroClient, extractorError bool
		wantStatus                                                        int
	}{
		{name: "tenant required", requireTenant: true, wantStatus: 400, wantCode: "SCOPE_REQUIRED"},
		{name: "project required", tenant: "tenant", action: "read", wantStatus: 400, wantCode: "SCOPE_REQUIRED"},
		{name: "project action requires both", action: "write", wantStatus: 400, wantCode: "SCOPE_REQUIRED"},
		{name: "project without tenant", project: "project", action: "read", wantStatus: 400, wantCode: "SCOPE_REQUIRED"},
		{name: "blank tenant cannot downgrade", tenant: " ", wantStatus: 400, wantCode: "INVALID_SCOPE"},
		{name: "empty tenant cannot downgrade", tenant: "<empty>", wantStatus: 400, wantCode: "INVALID_SCOPE"},
		{name: "empty project cannot downgrade", tenant: "tenant", project: "<empty>", action: "read", wantStatus: 400, wantCode: "INVALID_SCOPE"},
		{name: "padded project", tenant: "tenant", project: " project ", action: "read", wantStatus: 400, wantCode: "INVALID_SCOPE"},
		{name: "project without configured action", tenant: "tenant", project: "project", wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "invalid action", action: "delete", wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "missing extractor", noExtractor: true, wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "nil client", nilClient: true, wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "zero client", zeroClient: true, wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "extractor error", extractorError: true, wantStatus: 400, wantCode: "INVALID_SCOPE"},
		{name: "missing credential", credential: "missing", wantStatus: 401, wantCode: "AUTH_REQUIRED"},
		{name: "ambiguous credential", credential: "both", wantStatus: 400, wantCode: "AMBIGUOUS_IDENTITY_PROOF"},
		{name: "empty bearer", credential: "empty", wantStatus: 401, wantCode: "AUTH_REQUIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }),
			)
			defer server.Close()
			scope := Scope{TenantID: scopeTestID(tc.tenant), ProjectID: scopeTestID(tc.project)}
			config := AuthorizationConfig{
				Client: newTestClient(
					t,
					server.URL,
				),
				RequireTenant: tc.requireTenant,
				Action:        AccessAction(tc.action),
				Scope: func(*http.Request) (Scope, error) {
					if tc.extractorError {
						return Scope{}, errors.New("secret input")
					}
					return scope, nil
				},
			}
			if tc.nilClient {
				config.Client = nil
			}
			if tc.zeroClient {
				config.Client = &Client{}
			}
			if tc.noExtractor {
				config.Scope = nil
			}
			request := httptest.NewRequest(http.MethodGet, "http://service.test/resource", nil)
			if tc.credential != "missing" {
				request.Header.Set("Authorization", "Bearer user-key")
			}
			if tc.credential == "empty" {
				request.Header.Set("Authorization", "Bearer ")
			}
			if tc.credential == "both" {
				request.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "session"})
			}
			response := httptest.NewRecorder()
			Authorization(config)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("rejected request reached handler")
			})).ServeHTTP(response, request)
			assertIdentityError(t, response, tc.wantStatus, tc.wantCode, "")
			if calls.Load() != 0 || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("calls=%d body=%s", calls.Load(), response.Body.String())
			}
		})
	}
}

func TestAuthorizationRemoteFailures(t *testing.T) {
	validScope := `{"user_id":"user","principal":{"id":"user"},"tenant_id":"tenant","project_id":"project","ownership_state":"active"}`
	for _, tc := range []struct {
		name, scopeBody, accessBody, wantCode string
		scopeStatus, accessStatus, wantStatus int
		closeServer                           bool
	}{
		{name: "scope missing", scopeStatus: 404, scopeBody: `{"error":{"code":"SCOPE_NOT_FOUND","message":"scope missing"}}`, wantStatus: 404, wantCode: "SCOPE_NOT_FOUND"},
		{name: "inactive membership", scopeStatus: 403, scopeBody: `{"error":{"code":"SCOPE_ACCESS_DENIED","message":"membership required"}}`, wantStatus: 403, wantCode: "SCOPE_ACCESS_DENIED"},
		{name: "invalid scope", scopeStatus: 422, scopeBody: `{"error":{"code":"INVALID_SCOPE","message":"invalid scope"}}`, wantStatus: 422, wantCode: "INVALID_SCOPE"},
		{name: "bad user credential", scopeStatus: 401, scopeBody: `{"error":{"code":"AUTH_REQUIRED","message":"invalid credential"}}`, wantStatus: 401, wantCode: "AUTH_REQUIRED"},
		{name: "CSRF rejected", scopeStatus: 403, scopeBody: `{"error":{"code":"CSRF_REJECTED","message":"origin rejected"}}`, wantStatus: 403, wantCode: "CSRF_REJECTED"},
		{name: "wrong tenant response", scopeBody: strings.Replace(validScope, `"tenant"`, `"another"`, 1), wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "wrong project response", scopeBody: strings.Replace(validScope, `"project"`, `"another"`, 1), wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "missing principal", scopeBody: strings.Replace(validScope, `"principal":{"id":"user"},`, "", 1), wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "mismatched principal", scopeBody: strings.Replace(validScope, `"id":"user"`, `"id":"another"`, 1), wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "permission denied despite read flag", scopeBody: validScope, accessBody: `{"allowed":false,"read":true,"tenant_id":"tenant","project_id":"project"}`, wantStatus: 403, wantCode: "ACCESS_DENIED"},
		{name: "wrong access scope", scopeBody: validScope, accessBody: `{"allowed":true,"tenant_id":"another","project_id":"project"}`, wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "access service failure", scopeBody: validScope, accessStatus: 500, accessBody: `{"error":{"code":"INTERNAL","message":"secret boom"}}`, wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "access service key rejected", scopeBody: validScope, accessStatus: 401, accessBody: `{"error":{"code":"SERVICE_AUTH_REQUIRED","message":"secret credential"}}`, wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "scope service failure", scopeStatus: 500, scopeBody: `{"error":{"code":"INTERNAL","message":"secret boom"}}`, wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "proxy route missing", scopeStatus: 404, scopeBody: `<html>secret proxy</html>`, wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
		{name: "network unavailable", closeServer: true, wantStatus: 503, wantCode: "IDENTITY_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var accessCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status, body := tc.scopeStatus, tc.scopeBody
				if r.URL.Path == "/internal/v1/access/check" {
					accessCalls.Add(1)
					status, body = tc.accessStatus, tc.accessBody
				} else if r.URL.Path != "/internal/v1/scope/resolve" {
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
				if status == 0 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			if tc.closeServer {
				server.Close()
			}
			config := AuthorizationConfig{
				Client: newTestClient(t, server.URL), Action: AccessRead,
				Scope: func(*http.Request) (Scope, error) {
					return Scope{
						TenantID:  stringPointer("tenant"),
						ProjectID: stringPointer("project"),
					}, nil
				},
			}
			request := httptest.NewRequest(http.MethodGet, "http://service.test/resource", nil)
			request.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "session"})
			response := httptest.NewRecorder()
			Authorization(config)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("rejected request reached handler")
			})).ServeHTTP(response, request)
			assertIdentityError(t, response, tc.wantStatus, tc.wantCode, "")
			if strings.Contains(response.Body.String(), "secret") {
				t.Errorf("leaked error: %s", response.Body.String())
			}
			if tc.scopeBody != validScope && accessCalls.Load() != 0 {
				t.Error("access checked after invalid scope")
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func scopeTestID(value string) *string {
	if value == "" {
		return nil
	}
	if value == "<empty>" {
		value = ""
	}
	return &value
}

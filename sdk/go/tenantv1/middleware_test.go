package tenantv1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestIdentityMiddlewarePrincipal(t *testing.T) {
	wantPrincipal := Principal{ID: "user", Name: "User", Email: "user@example.test", Operator: true}
	for _, tc := range []struct {
		name, method, origin, csrf, authorization, session string
		want                                               ResolveIdentityRequest
	}{
		{
			name: "session cookie", method: http.MethodPost, session: "cookie",
			origin: "https://forge.test", csrf: "csrf",
			want: ResolveIdentityRequest{
				Type: IdentityProofSession, Session: "cookie", Method: http.MethodPost,
				Origin: "https://forge.test", CSRF: "csrf",
			},
		},
		{
			name: "bearer API key", method: http.MethodGet, authorization: "Bearer user-api-key",
			want: ResolveIdentityRequest{Type: IdentityProofAPIKey, APIKey: "user-api-key"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var got ResolveIdentityRequest
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if got != tc.want {
					t.Errorf("unexpected identity proof: %+v", got)
				}
				_ = json.NewEncoder(w).Encode(wantPrincipal)
			}))
			defer server.Close()
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				principal, ok := PrincipalFromContext(r.Context())
				if !ok || principal == nil || *principal != wantPrincipal {
					t.Errorf("unexpected principal: %+v, %v", principal, ok)
				}
			})
			handler := Identity(MiddlewareConfig{Client: newTestClient(t, server.URL)})(next)
			request := httptest.NewRequest(tc.method, "http://service.test/resource", nil)
			if tc.session != "" {
				request.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: tc.session})
			}
			if tc.authorization != "" {
				request.Header.Set("Authorization", tc.authorization)
			}
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			if tc.csrf != "" {
				request.Header.Set("X-CSRF-Token", tc.csrf)
			}
			handler.ServeHTTP(httptest.NewRecorder(), request)
			if !nextCalled {
				t.Fatal("next handler was not called")
			}
		})
	}
}

func TestIdentityMiddlewareFailures(t *testing.T) {
	for _, tc := range []struct {
		name, session, authorization, tenantBody, wantCode string
		tenantStatus, wantStatus                           int
		closeTenant, noTenantCall                          bool
	}{
		{name: "missing credentials", wantStatus: http.StatusUnauthorized, wantCode: "AUTH_REQUIRED", noTenantCall: true},
		{
			name: "ambiguous credentials", session: "cookie", authorization: "Bearer user-api-key",
			wantStatus: http.StatusBadRequest, wantCode: "AMBIGUOUS_IDENTITY_PROOF", noTenantCall: true,
		},
		{
			name: "bare bearer with cookie", session: "cookie", authorization: "Bearer",
			wantStatus: http.StatusBadRequest, wantCode: "AMBIGUOUS_IDENTITY_PROOF", noTenantCall: true,
		},
		{
			name: "empty bearer does not fall back to cookie", session: "cookie", authorization: "Bearer ",
			wantStatus: http.StatusBadRequest, wantCode: "AMBIGUOUS_IDENTITY_PROOF", noTenantCall: true,
		},
		{
			name: "empty bearer alone", authorization: "Bearer ",
			wantStatus: http.StatusUnauthorized, wantCode: "AUTH_REQUIRED", noTenantCall: true,
		},
		{
			name: "tenant session rejection", session: "cookie", tenantStatus: http.StatusForbidden,
			tenantBody: `{"error":{"code":"CSRF_REJECTED","message":"origin rejected"}}`,
			wantStatus: http.StatusForbidden, wantCode: "CSRF_REJECTED",
		},
		{
			name: "user API key rejection", authorization: "Bearer user-api-key", tenantStatus: http.StatusUnauthorized,
			tenantBody: `{"error":{"code":"AUTH_REQUIRED","message":"API key invalid"}}`,
			wantStatus: http.StatusUnauthorized, wantCode: "AUTH_REQUIRED",
		},
		{
			name: "service key rejection is not a user failure", authorization: "Bearer user-api-key",
			tenantStatus: http.StatusUnauthorized,
			tenantBody:   `{"error":{"code":"SERVICE_AUTH_REQUIRED","message":"service identity invalid"}}`,
			wantStatus:   http.StatusServiceUnavailable, wantCode: "IDENTITY_UNAVAILABLE",
		},
		{
			name: "tenant internal error", session: "cookie", tenantStatus: http.StatusInternalServerError,
			tenantBody: `{"error":{"code":"INTERNAL","message":"boom"}}`,
			wantStatus: http.StatusServiceUnavailable, wantCode: "IDENTITY_UNAVAILABLE",
		},
		{
			name: "tenant proxy HTML", authorization: "Bearer user-api-key", tenantStatus: http.StatusBadGateway,
			tenantBody: "<html>unavailable</html>",
			wantStatus: http.StatusServiceUnavailable, wantCode: "IDENTITY_UNAVAILABLE",
		},
		{
			name: "tenant route missing", session: "cookie", tenantStatus: http.StatusNotFound,
			tenantBody: `{}`,
			wantStatus: http.StatusServiceUnavailable, wantCode: "IDENTITY_UNAVAILABLE",
		},
		{
			name: "tenant unavailable", authorization: "Bearer user-api-key", closeTenant: true,
			wantStatus: http.StatusServiceUnavailable, wantCode: "IDENTITY_UNAVAILABLE",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if tc.tenantStatus != 0 {
					w.WriteHeader(tc.tenantStatus)
					_, _ = w.Write([]byte(tc.tenantBody))
					return
				}
				_ = json.NewEncoder(w).Encode(Principal{ID: "user"})
			}))
			if tc.closeTenant {
				server.Close()
			} else {
				defer server.Close()
			}
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true })
			handler := Identity(MiddlewareConfig{Client: newTestClient(t, server.URL)})(next)
			request := httptest.NewRequest(http.MethodPost, "http://service.test/resource", nil)
			if tc.session != "" {
				request.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: tc.session})
			}
			if tc.authorization != "" {
				request.Header.Set("Authorization", tc.authorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertIdentityError(t, response, tc.wantStatus, tc.wantCode)
			if nextCalled {
				t.Fatal("next handler was called")
			}
			if tc.noTenantCall && calls.Load() != 0 {
				t.Fatalf("tenant was called %d times for a request rejected locally", calls.Load())
			}
		})
	}
}

func assertIdentityError(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Errorf("unexpected status: %d", response.Code)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != wantCode {
		t.Errorf("unexpected error code: %s", body.Error.Code)
	}
}

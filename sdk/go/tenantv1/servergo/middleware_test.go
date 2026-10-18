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

func TestIdentityPublishesSameUserOnGinAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in tenantv1.ResolveIdentityRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if in.Type != tenantv1.IdentityProofAPIKey || in.APIKey != "user-api-key" {
			t.Errorf("unexpected identity proof: %+v", in)
		}
		_ = json.NewEncoder(w).Encode(tenantv1.Principal{ID: "user-1", Name: "User", Email: "user@example.test"})
	}))
	defer tenant.Close()
	client := newTenantClient(t, tenant.URL)

	handled := 0
	engine := gin.New()
	engine.GET("/resource", Identity(tenantv1.MiddlewareConfig{Client: client}), func(c *core.Context) {
		handled++
		ginUser := c.GetString(constants.KeyUserID)
		principal, ok := tenantv1.PrincipalFromContext(c.Request.Context())
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

func TestIdentityRejectionSkipsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, authorization, tenantBody, wantCode string
		tenantStatus, wantStatus, wantTenantCalls int
	}{
		{name: "missing credentials", wantStatus: http.StatusUnauthorized, wantCode: "AUTH_REQUIRED"},
		{
			name: "user API key rejected", authorization: "Bearer bad", tenantStatus: http.StatusUnauthorized,
			tenantBody: `{"error":{"code":"AUTH_REQUIRED","message":"API key invalid"}}`,
			wantStatus: http.StatusUnauthorized, wantCode: "AUTH_REQUIRED", wantTenantCalls: 1,
		},
		{
			name: "service key rejected", authorization: "Bearer user-api-key", tenantStatus: http.StatusUnauthorized,
			tenantBody: `{"error":{"code":"SERVICE_AUTH_REQUIRED","message":"service identity invalid"}}`,
			wantStatus: http.StatusServiceUnavailable, wantCode: "IDENTITY_UNAVAILABLE", wantTenantCalls: 1,
		},
		{
			name: "tenant internal error", authorization: "Bearer user-api-key", tenantStatus: http.StatusInternalServerError,
			tenantBody: `{"error":{"code":"INTERNAL","message":"boom"}}`,
			wantStatus: http.StatusServiceUnavailable, wantCode: "IDENTITY_UNAVAILABLE", wantTenantCalls: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			tenant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.tenantStatus)
				_, _ = w.Write([]byte(tc.tenantBody))
			}))
			defer tenant.Close()
			client := newTenantClient(t, tenant.URL)

			handled := false
			engine := gin.New()
			engine.GET("/resource", Identity(tenantv1.MiddlewareConfig{Client: client}), func(c *core.Context) {
				handled = true
			})
			request := httptest.NewRequest(http.MethodGet, "http://service.test/resource", nil)
			if tc.authorization != "" {
				request.Header.Set("Authorization", tc.authorization)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if handled {
				t.Fatal("handler ran for a rejected request")
			}
			if response.Code != tc.wantStatus {
				t.Errorf("status %d, want %d", response.Code, tc.wantStatus)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != tc.wantCode {
				t.Errorf("error code %q, want %q", body.Error.Code, tc.wantCode)
			}
			if got := int(calls.Load()); got != tc.wantTenantCalls {
				t.Errorf("tenant calls %d, want %d", got, tc.wantTenantCalls)
			}
		})
	}
}

func newTenantClient(t *testing.T, endpoint string) *tenantv1.Client {
	t.Helper()
	client, err := tenantv1.New(tenantv1.Config{Endpoint: endpoint, ServiceAPIKey: strings.Repeat("s", 32)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

package tenantv1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FlyTOmeLight/server-go/constants"
	core "github.com/FlyTOmeLight/server-go/pkg"
	"github.com/gin-gonic/gin"
)

const gatewayToken = "trusted-gateway-token-000001"

func TestMiddlewareTrustedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if _, err := MiddlewareTrustedIdentity(GatewayConfig{}); err == nil {
		t.Fatal("empty gateway config accepted")
	}
	for _, tc := range []struct {
		name, token, tenant, user, project string
		prior                              bool
		status                             int
	}{
		{name: "trusted gateway", token: gatewayToken, tenant: "tenant-1", user: "alice", project: "project-1", status: http.StatusOK},
		{name: "forged headers", tenant: "tenant-1", user: "alice", status: http.StatusUnauthorized},
		{name: "unknown gateway", token: "other-gateway-token-00000001", tenant: "tenant-1", user: "alice", status: http.StatusUnauthorized},
		{name: "incomplete identity", token: gatewayToken, user: "alice", status: http.StatusUnauthorized},
		{name: "already authenticated", token: gatewayToken, tenant: "tenant-1", user: "alice", prior: true, status: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := MiddlewareTrustedIdentity(GatewayConfig{
				Verify: func(r *http.Request) error {
					if r.Header.Get("Authorization") == "Bearer "+gatewayToken {
						return nil
					}
					return errors.New("untrusted gateway")
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			engine := gin.New()
			if tc.prior {
				engine.Use(func(c *core.Context) {
					c.Set(constants.KeyUserID, "existing")
					c.Next()
				})
			}
			engine.Use(handler)
			engine.GET("/", func(c *core.Context) {
				if c.GetString(constants.KeyUserID) != tc.user ||
					c.GetString(constants.KeyTenantID) != tc.tenant ||
					c.GetString(constants.KeyProjectID) != tc.project {
					t.Fatal("gateway identity was not installed")
				}
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			req.Header.Set(constants.HeaderTenantID, tc.tenant)
			req.Header.Set(constants.HeaderUserID, tc.user)
			req.Header.Set(constants.HeaderProjectID, tc.project)
			writer := httptest.NewRecorder()
			engine.ServeHTTP(writer, req)
			if writer.Code != tc.status {
				t.Fatalf("status = %d, want %d", writer.Code, tc.status)
			}
		})
	}
}

func TestMiddlewareServiceAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if _, err := MiddlewareServiceAuth(ServiceAuthConfig{}); err == nil {
		t.Fatal("empty service auth accepted")
	}
	for _, tc := range []struct {
		name, token string
		dynamic     bool
		status      int
		service     string
	}{
		{name: "static token", token: propagationServiceToken, status: http.StatusOK, service: "agentic-kit-api"},
		{name: "dynamic token", token: propagationServiceToken, dynamic: true, status: http.StatusOK, service: "coding"},
		{name: "rejected dynamically", token: propagationServiceToken, dynamic: true, status: http.StatusUnauthorized},
		{name: "missing token", status: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := ServiceAuthConfig{Authenticate: func(context.Context, string) (string, error) {
				return "agentic-kit-api", nil
			}}
			if tc.dynamic {
				config = ServiceAuthConfig{Authenticate: func(context.Context, string) (string, error) {
					if tc.service == "" {
						return "", errors.New("rejected")
					}
					return tc.service, nil
				}}
			}
			handler, err := MiddlewareServiceAuth(config)
			if err != nil {
				t.Fatal(err)
			}
			engine := gin.New()
			engine.Use(handler)
			engine.GET("/", func(c *core.Context) {
				if c.GetString(constants.KeyUserID) != tc.service {
					t.Fatalf("service = %q", c.GetString(constants.KeyUserID))
				}
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			writer := httptest.NewRecorder()
			engine.ServeHTTP(writer, req)
			if writer.Code != tc.status {
				t.Fatalf("status = %d, want %d", writer.Code, tc.status)
			}
		})
	}
}

package tenantv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FlyTOmeLight/server-go/constants"
	core "github.com/FlyTOmeLight/server-go/pkg"
	"github.com/gin-gonic/gin"
)

const (
	propagationServiceToken = "agentic-kit-service-token-0001"
	propagationPersonToken  = "ordinary-person-token-000001"
)

func newPropagationEngine(t *testing.T, config ServiceAuthConfig) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := MiddlewareInternalPropagation(config)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(handler)
	engine.GET("/check", func(c *core.Context) {
		c.JSON(http.StatusOK, map[string]string{
			"tenant":        c.GetString(constants.KeyTenantID),
			"user":          c.GetString(constants.KeyUserID),
			"project":       c.GetString(constants.KeyProjectID),
			"originTenant":  c.GetString(constants.KeyImpersonateOriginTenantID),
			"originUser":    c.GetString(constants.KeyImpersonateOriginUserID),
			"originProject": c.GetString(constants.KeyImpersonateOriginProjectID),
			"authMode":      c.GetString(constants.KeyAuthMode),
		})
	})
	return engine
}

func propagationRequest(token, tenant, user, project string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/check", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set(constants.HeaderImpersonateTenantID, tenant)
	req.Header.Set(constants.HeaderImpersonateUserID, user)
	req.Header.Set(constants.HeaderImpersonateProjectID, project)
	return req
}

func TestMiddlewareInternalPropagation(t *testing.T) {
	if _, err := MiddlewareInternalPropagation(ServiceAuthConfig{}); err == nil {
		t.Fatal("empty service tokens accepted")
	}
	for _, tc := range []struct {
		name                       string
		authorize                  bool
		token, tenant, user, proj  string
		status                     int
		tenantID, userID, authMode string
	}{
		{
			name: "service forwards identity", token: propagationServiceToken,
			tenant: "tenant-1", user: "alice", proj: "project-1", status: http.StatusOK,
			tenantID: "tenant-1", userID: "alice", authMode: "Internal",
		},
		{
			name: "service account without headers", token: propagationServiceToken, status: http.StatusOK,
			userID: "agentic-kit-api",
		},
		{name: "missing service key", tenant: "t", user: "u", status: http.StatusUnauthorized},
		{name: "unknown service key", token: "unknown-caller-token-0000001", tenant: "t", user: "u", status: http.StatusUnauthorized},
		{name: "partial identity", token: propagationServiceToken, tenant: "tenant-1", status: http.StatusForbidden},
		{
			name: "caller not allowed to forward", authorize: true, token: propagationServiceToken,
			tenant: "tenant-1", user: "alice", status: http.StatusForbidden,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := ServiceAuthConfig{
				Authenticate: func(_ context.Context, token string) (string, error) {
					switch token {
					case propagationServiceToken:
						return "agentic-kit-api", nil
					case propagationPersonToken:
						return "person", nil
					default:
						return "", &Error{
							Status:  http.StatusUnauthorized,
							Code:    "SERVICE_AUTH_REQUIRED",
							Message: "unknown service token",
						}
					}
				},
			}
			if tc.authorize {
				config.Authorize = func(*core.Context) error { return errors.New("caller may not forward identity") }
			}
			engine := newPropagationEngine(t, config)
			writer := httptest.NewRecorder()
			engine.ServeHTTP(writer, propagationRequest(tc.token, tc.tenant, tc.user, tc.proj))
			if writer.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", writer.Code, tc.status, writer.Body)
			}
			if tc.status != http.StatusOK {
				return
			}
			response := map[string]any{}
			if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response["tenant"] != tc.tenantID || response["user"] != tc.userID ||
				response["authMode"] != tc.authMode {
				t.Fatalf(
					"identity = %v, want tenant %q user %q mode %q",
					response,
					tc.tenantID,
					tc.userID,
					tc.authMode,
				)
			}
			if tc.tenant != "" {
				for _, key := range []string{"originTenant", "originUser", "originProject"} {
					if response[key] == "" {
						t.Fatalf(
							"%s empty, propagated identity must also land in origin keys: %v",
							key,
							response,
						)
					}
				}
			}
		})
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
)

func TestIdentityCredentialSelectionAndCSRF(t *testing.T) {
	for _, tc := range []struct {
		name, method, session, bearer, origin, csrf string
		status                                      int
		code                                        string
	}{
		{name: "cookie read", method: "GET", session: "session", status: 200},
		{name: "cookie write", method: "POST", session: "session", origin: "https://forge.test", csrf: "csrf", status: 200},
		{name: "cookie write without csrf", method: "POST", session: "session", status: 403, code: "CSRF_REJECTED"},
		{name: "bearer write without csrf", method: "POST", bearer: "Bearer key", status: 200},
		{name: "missing credentials", method: "GET", status: 401, code: "AUTH_REQUIRED"},
		{name: "invalid bearer", method: "GET", bearer: "Bearer invalid", status: 401, code: "AUTH_REQUIRED"},
		{name: "empty bearer", method: "GET", bearer: "Bearer", status: 401, code: "AUTH_REQUIRED"},
		{name: "two credentials", method: "GET", session: "session", bearer: "Bearer key", status: 400, code: "AMBIGUOUS_IDENTITY_PROOF"},
		{name: "cookie with empty bearer", method: "GET", session: "session", bearer: "Bearer", status: 400, code: "AMBIGUOUS_IDENTITY_PROOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			Install(engine)
			engine.Use(Identity(
				func(_ context.Context, session string) (identity.User, string, error) {
					if session != "session" {
						return identity.User{}, "", apperror.New(
							401,
							"AUTH_REQUIRED",
							"invalid session",
						)
					}
					return identity.User{ID: "user"}, "csrf", nil
				},
				func(_ context.Context, token string) (identity.User, error) {
					if token != "key" {
						return identity.User{}, apperror.New(
							401,
							"AUTH_REQUIRED",
							"invalid key",
						)
					}
					return identity.User{ID: "user"}, nil
				},
				"https://forge.test",
			))
			reached := false
			engine.Any("/resource", func(c *gin.Context) {
				reached = true
				c.JSON(200, User(c))
			})
			request := httptest.NewRequest(tc.method, "/resource", nil)
			if tc.session != "" {
				request.AddCookie(&http.Cookie{Name: "forge_session", Value: tc.session})
			}
			request.Header.Set("Authorization", tc.bearer)
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("X-CSRF-Token", tc.csrf)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != tc.status || reached != (tc.status == 200) {
				t.Fatalf("status %d, reached %v: %s", response.Code, reached, response.Body.String())
			}
			var body struct {
				ID    string `json:"id"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.status == 200 && body.ID != "user" || body.Error.Code != tc.code {
				t.Fatalf("unexpected identity response: %s", response.Body.String())
			}
		})
	}
}

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FlyTOmeLight/server-go/conf"
	"github.com/FlyTOmeLight/server-go/host"
	"github.com/FlyTOmeLight/server-go/logger"
	"github.com/gin-gonic/gin"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authentication"
)

func TestAPIDoesNotServeFrontend(t *testing.T) {
	handler := apiHandler(t, &testApplication{})
	for _, path := range []string{"/", "/invite", "/login/oauth/authorize", "/app.js", "/tasks", "/api/unknown"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if w.Code != 404 || json.Unmarshal(w.Body.Bytes(), &body) != nil ||
				body.Error.Code != "NOT_FOUND" {
				t.Fatalf("expected JSON 404, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/v1/me", nil))
	if w.Code != 405 {
		t.Fatalf("method mismatch should be 405, got %d", w.Code)
	}
}

func apiHandler(t *testing.T, app *testApplication, extra ...host.Service) http.Handler {
	t.Helper()
	manager, err := conf.NewManagerFromConfig(&conf.Config{AppName: "forge-test", AppCode: 101, RunMode: "release"})
	if err != nil {
		t.Fatal(err)
	}
	log, err := logger.NewLogger(&logger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if app.Account == nil {
		app.Account = authentication.New(app.DB, app.Config, nil, nil)
	}
	application := newAPI(app.DB, app.Account.Config, app.Account, app.Writes)
	h, err := host.NewHTTPHost(
		manager,
		log,
		SetupHTTP,
		append([]host.Service{application}, extra...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(); log.Close(); _ = manager.Close(context.Background()) })
	return h.Handler()
}

func TestFrameworkBindingAndServiceAuthentication(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		name, credential, body string
		status                 int
		code                   string
	}{
		{name: "missing bearer", credential: "", body: `{}`, status: 401, code: "SERVICE_AUTH_REQUIRED"},
		{name: "raw token rejected", credential: f.a.ServiceKey, body: `{}`, status: 401, code: "SERVICE_AUTH_REQUIRED"},
		{name: "wrong bearer", credential: "Bearer wrong", body: `{}`, status: 401, code: "SERVICE_AUTH_REQUIRED"},
		{name: "user API key rejected", credential: "Bearer " + f.a.userKey, body: `{}`, status: 401, code: "SERVICE_AUTH_REQUIRED"},
		{name: "unknown field", credential: "Bearer " + f.a.ServiceKey, body: `{"user_id":"u","tenant_id":"t","extra":true}`, status: 400, code: "INVALID_JSON"},
		{name: "multiple documents", credential: "Bearer " + f.a.ServiceKey, body: `{} {}`, status: 400, code: "INVALID_JSON"},
		{name: "oversized", credential: "Bearer " + f.a.ServiceKey, body: `{"user_id":"` + strings.Repeat("x", 65536) + `"}`, status: 400, code: "INVALID_JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(
				"POST",
				"/internal/v1/workspace/check",
				strings.NewReader(tc.body),
			)
			request.Header.Set("Authorization", tc.credential)
			w := httptest.NewRecorder()
			apiHandler(t, f.a).ServeHTTP(w, request)
			var response struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if json.Unmarshal(w.Body.Bytes(), &response) != nil || w.Code != tc.status ||
				response.Error.Code != tc.code {
				t.Fatalf("response %d: %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Request-ID") == "" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing framework request ID or API cache policy")
			}
		})
	}
}

type panicService struct{}

func (*panicService) RegisterRoutes(group *gin.RouterGroup) error {
	group.GET("/test/panic", func(c *gin.Context) { panic("test") })
	group.GET("/test/committed", func(c *gin.Context) { c.JSON(200, Object{"ok": true}); panic("test") })
	return nil
}

func (*panicService) Start(context.Context) error { return nil }

func (*panicService) Stop(context.Context) error { return nil }

func TestFrameworkRecoveryPreservesResponseContract(t *testing.T) {
	handler := apiHandler(t, &testApplication{}, &panicService{})
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{path: "/test/panic", status: 500, body: `{"error":{"code":"INTERNAL_ERROR","message":"服务暂不可用"}}`},
		{path: "/test/committed", status: 200, body: `{"ok":true}`},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status || w.Body.String() != tc.body {
			t.Fatalf("response %d: %s", w.Code, w.Body.String())
		}
	}
}

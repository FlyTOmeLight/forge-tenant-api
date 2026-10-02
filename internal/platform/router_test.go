package platform

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestAPIDoesNotServeFrontend(t *testing.T) {
	handler := (&App{}).Handler()
	for _, path := range []string{"/", "/invite", "/login/oauth/authorize", "/app.js", "/tasks", "/api/unknown"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if w.Code != 404 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Error.Code != "NOT_FOUND" {
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

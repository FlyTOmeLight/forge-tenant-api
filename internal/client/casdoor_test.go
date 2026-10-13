package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

func TestCasdoorCreateUser(t *testing.T) {
	for _, tc := range []struct {
		name, owner, email string
		existing, conflict bool
	}{
		{name: "create", owner: "forge", email: "user@example.test", existing: false, conflict: false},
		{name: "reuse", owner: "forge", email: "user@example.test", existing: true, conflict: false},
		{name: "different email", owner: "forge", email: "someone@example.test", existing: true, conflict: true},
		{name: "different owner", owner: "other", email: "user@example.test", existing: true, conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created := false
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, secret, ok := r.BasicAuth()
				if !ok || id != "app" || secret != "secret" {
					t.Error("missing application authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/get-user":
					if r.URL.Query().Get("id") != "forge/user" {
						t.Errorf("unexpected id: %s", r.URL.RawQuery)
					}
					var user *casdoorsdk.User
					if tc.existing || created {
						user = &casdoorsdk.User{
							Id:    "subject",
							Owner: tc.owner,
							Name:  "user",
							Email: "user@example.test",
						}
					}
					_ = json.NewEncoder(w).Encode(casdoorsdk.Response{Status: "ok", Data: user})
				case "/api/add-user":
					if tc.existing {
						t.Error("must not modify an existing identity")
					}
					var user casdoorsdk.User
					if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
						t.Error(err)
					}
					if user.Owner != "forge" || user.Name != "user" ||
						user.Email != "user@example.test" ||
						user.Password != "password" ||
						user.Type != "normal-user" ||
						user.IsAdmin ||
						user.IsForbidden {
						t.Errorf("incorrect user: name=%s owner=%s", user.Name, user.Owner)
					}
					created = true
					_ = json.NewEncoder(w).
						Encode(casdoorsdk.Response{Status: "ok", Data: "Affected"})
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			c := &Casdoor{
				URL:          server.URL,
				ClientID:     "app",
				Secret:       "secret",
				Organization: "forge",
				HTTP:         server.Client(),
			}
			user, err := c.CreateUser(context.Background(), "user", "User", tc.email, "password")
			if tc.conflict {
				if !IsStatus(err, 409) {
					t.Fatalf("expected ownership conflict, got %v", err)
				}
				return
			}
			if err != nil || user == nil || user.ID != "subject" || created == tc.existing {
				t.Fatalf("user=%v created=%v error=%v", user, created, err)
			}
		})
	}
}

func TestCasdoorCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }),
	)
	defer server.Close()
	c := &Casdoor{URL: server.URL, Organization: "forge", HTTP: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := c.User(ctx, "user"); result <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not stop")
	}
}

func TestCasdoorFailureMapping(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status, expected int
	}{
		{name: "http", body: `{"status":"error"}`, code: "HTTP_ERROR", status: 403, expected: 403},
		{name: "rejected", body: `{"status":"error","msg":"private details"}`, code: "REQUEST_REJECTED", status: 200, expected: 422},
		{name: "invalid envelope", body: `broken`, code: "INVALID_RESPONSE", status: 200, expected: 502},
		{name: "invalid user", body: `{"status":"ok","data":42}`, code: "INVALID_RESPONSE", status: 200, expected: 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			c := &Casdoor{URL: server.URL, Organization: "forge", HTTP: server.Client()}
			_, err := c.User(context.Background(), "user")
			var detail *Error
			if !errors.As(err, &detail) || detail.Status != tc.expected || detail.Code != tc.code {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

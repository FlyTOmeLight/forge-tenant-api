package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSetAccessChecksEffectivePermission(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		direct, actual, target int
		wantConflict           bool
	}{{name: "revoke direct", direct: 30, actual: 0, target: 0, wantConflict: false}, {name: "inherited access survives revoke", direct: 30, actual: 20, target: 0, wantConflict: true}, {name: "downgrade", direct: 30, actual: 20, target: 20, wantConflict: false}, {name: "inherited access survives downgrade", direct: 30, actual: 40, target: 20, wantConflict: true}, {name: "already absent", direct: 0, actual: 0, target: 0, wantConflict: false}} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("PRIVATE-TOKEN") != "test" {
					t.Error("missing credential header")
				}
				if r.Method == "GET" {
					if r.URL.Path == "/api/v4/projects/1/members/all/2" {
						if tc.actual == 0 {
							w.WriteHeader(404)
						} else {
							fmt.Fprintf(w, `{"access_level":%d}`, tc.actual)
						}
					} else {
						if tc.direct == 0 {
							w.WriteHeader(404)
						} else {
							fmt.Fprintf(w, `{"access_level":%d}`, tc.direct)
						}
					}
					return
				}
				writes++
				if r.Method == "DELETE" {
					w.WriteHeader(204)
				} else {
					fmt.Fprintf(w, `{"id":2,"access_level":%d}`, tc.target)
				}
			}))
			defer s.Close()
			g := &GitLab{URL: s.URL, Token: "test", HTTP: s.Client()}
			actual, e := g.SetAccess(context.Background(), 1, 2, tc.target)
			if actual != tc.actual {
				t.Fatalf("actual=%d", actual)
			}
			var remote *Error
			conflict := errors.As(e, &remote) && remote.Code == "EFFECTIVE_PERMISSION_CONFLICT"
			if conflict != tc.wantConflict {
				t.Fatalf("unexpected result: %v", e)
			}
			if tc.direct == 0 && writes != 0 {
				t.Fatal("already absent access should not be deleted")
			}
		})
	}
}

func TestRejectsUnmanagedGroup(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(
			w,
			`{"id":12,"full_path":"example","visibility":"private","description":"another application"}`,
		)
	}))
	defer s.Close()
	g := &GitLab{URL: s.URL, HTTP: s.Client()}
	_, e := g.EnsureGroup(context.Background(), "tenant-a", "example", "Example", 0)
	if !IsStatus(e, 409) {
		t.Fatalf("must not adopt existing group: %v", e)
	}
}

func TestRetryAfterPreserved(t *testing.T) {
	s := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "17"); w.WriteHeader(429) },
		),
	)
	defer s.Close()
	g := &GitLab{URL: s.URL, HTTP: s.Client()}
	_, e := g.Version(context.Background())
	var r *Error
	if !errors.As(e, &r) || r.RetryAfter.Seconds() != 17 {
		t.Fatalf("missing retry-after: %v", e)
	}
}

func TestGitLabSDKDoesNotRetryWrites(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writes++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	g := GitLab{URL: server.URL, HTTP: server.Client()}
	_, err := g.EnsureGroup(context.Background(), "tenant", "tenant", "Tenant", 0)
	if !IsStatus(err, http.StatusServiceUnavailable) || writes != 1 {
		t.Fatalf("writes=%d err=%v", writes, err)
	}
}

func TestGitLabSDKPreservesOwnershipChecks(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		project        bool
	}{
		{name: "shared group", response: `{"id":12,"full_path":"tenant","visibility":"private","description":"managed-by:forge-tenant;tenant:t","shared_with_groups":[{"group_id":5}]}`, project: false},
		{name: "wrong namespace", response: `{"id":12,"path_with_namespace":"tenant/repo","visibility":"private","description":"managed-by:forge-tenant;project:p","namespace":{"id":99}}`, project: true},
		{name: "shared project", response: `{"id":12,"path_with_namespace":"tenant/repo","visibility":"private","description":"managed-by:forge-tenant;project:p","namespace":{"id":1},"shared_with_groups":[{"group_id":5}]}`, project: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.response) },
				),
			)
			defer server.Close()
			g := GitLab{URL: server.URL, HTTP: server.Client()}
			var err error
			if tc.project {
				_, err = g.EnsureProject(
					context.Background(),
					"p",
					"tenant/repo",
					"Repo",
					"create",
					1,
					0,
				)
			} else {
				_, err = g.EnsureGroup(context.Background(), "t", "tenant", "Tenant", 0)
			}
			if !IsStatus(err, http.StatusConflict) {
				t.Fatalf("unsafe ownership accepted: %v", err)
			}
		})
	}
}

func TestGitLabSDKCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }),
	)
	defer server.Close()
	g := GitLab{URL: server.URL, HTTP: server.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := g.Version(ctx); result <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled request succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SDK ignored cancellation")
	}
}

func TestGitLabSDKErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   int
		code   string
	}{
		{name: "missing", status: 404, body: `{"message":"missing"}`, want: 404, code: "HTTP_ERROR"},
		{name: "invalid JSON", status: 200, body: `{`, want: 502, code: "INVALID_RESPONSE"},
		{name: "wrong JSON type", status: 200, body: `[]`, want: 502, code: "INVALID_RESPONSE"},
		{name: "null JSON", status: 200, body: `null`, want: 502, code: "INVALID_RESPONSE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) },
				),
			)
			defer server.Close()
			g := GitLab{URL: server.URL, HTTP: server.Client()}
			_, err := g.Version(context.Background())
			var remote *Error
			if !errors.As(err, &remote) || remote.Status != tc.want || remote.Code != tc.code {
				t.Fatalf("wrong error mapping: %v", err)
			}
		})
	}
}

func TestGitLabSDKEncodesProjectPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v4/projects/tenant%2Frepo" {
			t.Errorf("path=%s", r.URL.EscapedPath())
		}
		fmt.Fprint(
			w,
			`{"id":12,"path_with_namespace":"tenant/repo","visibility":"private","namespace":{"id":1}}`,
		)
	}))
	defer server.Close()
	g := GitLab{URL: server.URL, HTTP: server.Client()}
	id, err := g.EnsureProject(context.Background(), "p", "tenant/repo", "Repo", "bind", 1, 0)
	if err != nil || id != 12 {
		t.Fatalf("id=%d err=%v", id, err)
	}
}

func TestGitLabSDKChecksFullUserIdentity(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{name: "bound active", body: `{"id":2,"state":"active","identities":[{"provider":"openid_connect","extern_uid":"subject"}]}`, code: ""},
		{name: "administrator", body: `{"id":2,"state":"active","is_admin":true}`, code: "USER_ADMIN_OR_INACTIVE"},
		{name: "blocked", body: `{"id":2,"state":"blocked"}`, code: "USER_ADMIN_OR_INACTIVE"},
		{name: "identity mismatch", body: `{"id":2,"state":"active","identities":[{"provider":"openid_connect","extern_uid":"other"}]}`, code: "IDENTITY_BINDING_MISMATCH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v4/users" {
					if r.URL.Query().Get("provider") != "openid_connect" ||
						r.URL.Query().Get("extern_uid") != "subject" {
						t.Error("missing identity filter")
					}
					fmt.Fprint(w, `[{"id":2}]`)
					return
				}
				if r.URL.Path != "/api/v4/users/2" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				reads++
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			g := GitLab{URL: server.URL, HTTP: server.Client()}
			id, err := g.EnsureUser(context.Background(), "subject", "user", "User", "user@example.test")
			if reads != 1 {
				t.Fatal("full user was not checked")
			}
			if tc.code == "" {
				if err != nil || id != 2 {
					t.Fatalf("id=%d err=%v", id, err)
				}
				return
			}
			var remote *Error
			if !errors.As(err, &remote) || remote.Code != tc.code {
				t.Fatalf("unsafe identity accepted: %v", err)
			}
		})
	}
}

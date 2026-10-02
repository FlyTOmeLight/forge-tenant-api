package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetAccessChecksEffectivePermission(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		direct, actual, target int
		wantConflict           bool
	}{{"revoke direct", 30, 0, 0, false}, {"inherited access survives revoke", 30, 20, 0, true}, {"downgrade", 30, 20, 20, false}, {"inherited access survives downgrade", 30, 40, 20, true}, {"already absent", 0, 0, 0, false}} {
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
				w.WriteHeader(204)
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
		fmt.Fprint(w, `{"id":12,"full_path":"example","visibility":"private","description":"another application"}`)
	}))
	defer s.Close()
	g := &GitLab{URL: s.URL, HTTP: s.Client()}
	_, e := g.EnsureGroup(context.Background(), "tenant-a", "example", "Example", 0)
	if !IsStatus(e, 409) {
		t.Fatalf("must not adopt existing group: %v", e)
	}
}
func TestRetryAfterPreserved(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "17"); w.WriteHeader(429) }))
	defer s.Close()
	g := &GitLab{URL: s.URL, HTTP: s.Client()}
	_, e := g.Version(context.Background())
	var r *Error
	if !errors.As(e, &r) || r.RetryAfter.Seconds() != 17 {
		t.Fatalf("missing retry-after: %v", e)
	}
}

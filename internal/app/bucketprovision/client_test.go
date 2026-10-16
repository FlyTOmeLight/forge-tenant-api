package bucketprovision

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStorageManagementWire(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		bad        bool
	}{
		{"created", `{"tenantID":"t_test","backendID":"jfs","bucket":"t-test"}`, 201, false},
		{"replay", `{"tenantID":"t_test","backendID":"jfs","bucket":"t-test"}`, 200, false},
		{"wrong tenant", `{"tenantID":"t_other","backendID":"jfs","bucket":"t-test"}`, 200, true},
		{"empty bucket", `{"tenantID":"t_test","backendID":"jfs","bucket":""}`, 200, true},
		{"trailing json", `{"tenantID":"t_test","backendID":"jfs","bucket":"x"}{}`, 200, true},
		{"server failure", `secret response`, 503, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v1/storage/tenant-buckets" ||
					r.Header.Get("Authorization") != "Bearer independent-token" {
					t.Error("wrong management request")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			c := Client{
				URL:     server.URL,
				Token:   "independent-token",
				HTTP:    server.Client(),
				Timeout: time.Second,
			}
			_, err := c.Ensure(context.Background(), "t_test")
			if (err != nil) != tc.bad {
				t.Fatal(err)
			}
			if err != nil &&
				(strings.Contains(err.Error(), "secret response") || strings.Contains(err.Error(), "independent-token")) {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestManagementRedirectRejected(t *testing.T) {
	var reached atomic.Bool
	target := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }),
	)
	defer target.Close()
	redirect := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }),
	)
	defer redirect.Close()
	h := redirect.Client()
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := Client{URL: redirect.URL, Token: "management-token", HTTP: h, Timeout: time.Second}
	if _, err := c.Ensure(context.Background(), "t_test"); err == nil || reached.Load() {
		t.Fatal("management redirect not rejected")
	}
}

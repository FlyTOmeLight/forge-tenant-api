package service

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
)

func TestLoginProxyPreservesIdentitySession(t *testing.T) {
	f := setup(t)
	a := f.a
	a.Account.Config.ClientID = "forge-client"
	a.Identity.OAuth = oauth2.Config{RedirectURL: a.Account.Config.PublicURL + "/auth/callback"}
	flow, state := identity.Token(), identity.Token()
	err := a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO login_flows(token_hash,state_hash,nonce,verifier,return_path,expires_at) VALUES(?,?,'nonce','verifier','/',now()+interval '10 minutes')`, identity.Hash(flow), identity.Hash(state)).
		Error
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("casdoor_session_id"); err != nil || c.Value != "existing-identity" {
			t.Error("identity session was not forwarded")
		}
		if _, err := r.Cookie("forge_session"); err == nil {
			t.Error("Forge session leaked to identity provider")
		}
		http.SetCookie(
			w,
			&http.Cookie{Name: "casdoor_session_id", Value: "renewed-identity", Path: "/", MaxAge: 3600},
		)
		http.SetCookie(w, &http.Cookie{Name: "unrelated", Value: "must-not-forward", Path: "/"})
		_, _ = w.Write([]byte(`{"status":"ok","data":"authorization-code"}`))
	}))
	defer upstream.Close()
	a.Casdoor.URL = upstream.URL
	q := url.Values{
		"clientId":     {a.Account.Config.ClientID},
		"redirectUri":  {a.Identity.OAuth.RedirectURL},
		"responseType": {"code"},
		"type":         {"code"},
		"state":        {state},
	}
	r := httptest.NewRequest("POST", "/api/login?"+q.Encode(), strings.NewReader(`{"type":"code"}`))
	r.Header.Set("Origin", a.Account.Config.PublicURL)
	r.AddCookie(&http.Cookie{Name: "forge_login", Value: flow})
	r.AddCookie(&http.Cookie{Name: "forge_session", Value: "private-forge-session"})
	r.AddCookie(&http.Cookie{Name: "forge_identity", Value: "existing-identity"})
	w := httptest.NewRecorder()
	apiHandler(t, a).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "forge_identity" || cookies[0].Value != "renewed-identity" ||
		!cookies[0].HttpOnly ||
		cookies[0].SameSite != http.SameSiteLaxMode ||
		cookies[0].Domain != "" {
		t.Fatalf("expected one protected identity cookie, got %d", len(cookies))
	}
}

func TestGitLabLoginClientBoundary(t *testing.T) {
	f := setup(t)
	a := f.a
	a.Account.Config.ClientID = "forge-client"
	a.Account.Config.GitLabClientID = "gitlab-client"
	a.Account.Config.GitLabPublicURL = "https://gitlab.example.test"
	a.Identity.OAuth = oauth2.Config{RedirectURL: a.Account.Config.PublicURL + "/auth/callback"}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"status":"ok","data":{"name":"gitlab"}}`))
	}))
	defer upstream.Close()
	a.Casdoor.URL = upstream.URL
	for _, tc := range []struct {
		name, client, redirect, state, origin string
		want                                  int
	}{
		{name: "registered GitLab", client: "gitlab-client", redirect: a.Account.Config.GitLabPublicURL + "/users/auth/openid_connect/callback", state: "state", origin: a.Account.Config.PublicURL, want: 200},
		{name: "unregistered client", client: "foreign-client", redirect: a.Account.Config.GitLabPublicURL + "/users/auth/openid_connect/callback", state: "state", origin: a.Account.Config.PublicURL, want: 400},
		{name: "foreign callback", client: "gitlab-client", redirect: "https://evil.example/callback", state: "state", origin: a.Account.Config.PublicURL, want: 400},
		{name: "missing state", client: "gitlab-client", redirect: a.Account.Config.GitLabPublicURL + "/users/auth/openid_connect/callback", state: "", origin: a.Account.Config.PublicURL, want: 400},
		{name: "foreign origin", client: "gitlab-client", redirect: a.Account.Config.GitLabPublicURL + "/users/auth/openid_connect/callback", state: "state", origin: "https://evil.example", want: 403},
		{name: "Forge without login flow", client: "forge-client", redirect: a.Identity.OAuth.RedirectURL, state: "state", origin: a.Account.Config.PublicURL, want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := calls
			q := url.Values{
				"clientId":     {tc.client},
				"redirectUri":  {tc.redirect},
				"state":        {tc.state},
				"type":         {"code"},
				"responseType": {"code"},
			}
			r := httptest.NewRequest("POST", "/api/login?"+q.Encode(), strings.NewReader(`{"type":"code"}`))
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			apiHandler(t, a).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
			if tc.want != 200 && calls != before {
				t.Fatal("invalid login reached identity provider")
			}
		})
	}
}

func TestLogoutClearsIdentitySession(t *testing.T) {
	f := setup(t)
	r := httptest.NewRequest("POST", "/auth/logout", nil)
	r.Header.Set("Origin", f.a.Account.Config.PublicURL)
	r.Header.Set("X-CSRF-Token", identity.Hash(f.operator+":csrf"))
	r.AddCookie(&http.Cookie{Name: "forge_session", Value: f.operator})
	r.AddCookie(&http.Cookie{Name: "forge_identity", Value: "identity"})
	w := httptest.NewRecorder()
	apiHandler(t, f.a).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	cleared := map[string]bool{}
	for _, c := range w.Result().Cookies() {
		cleared[c.Name] = c.MaxAge < 0
	}
	if !cleared["forge_session"] || !cleared["forge_identity"] {
		t.Fatal("logout must clear app and identity sessions")
	}
}

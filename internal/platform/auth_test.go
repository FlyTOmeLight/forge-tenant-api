package platform

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestLoginProxyPreservesIdentitySession(t *testing.T) {
	f := setup(t)
	a := f.a
	a.Config.ClientID = "forge-client"
	a.OAuth = oauth2.Config{RedirectURL: a.Config.PublicURL + "/auth/callback"}
	flow, state := Token(), Token()
	_, err := a.DB.Exec(f.ctx, `INSERT INTO login_flows(token_hash,state_hash,nonce,verifier,return_path,expires_at) VALUES($1,$2,'nonce','verifier','/',now()+interval '10 minutes')`, Hash(flow), Hash(state))
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
		http.SetCookie(w, &http.Cookie{Name: "casdoor_session_id", Value: "renewed-identity", Path: "/", MaxAge: 3600})
		http.SetCookie(w, &http.Cookie{Name: "unrelated", Value: "must-not-forward", Path: "/"})
		_, _ = w.Write([]byte(`{"status":"ok","data":"authorization-code"}`))
	}))
	defer upstream.Close()
	a.Config.CasdoorURL = upstream.URL
	q := url.Values{"clientId": {a.Config.ClientID}, "redirectUri": {a.OAuth.RedirectURL}, "responseType": {"code"}, "type": {"code"}, "state": {state}}
	r := httptest.NewRequest("POST", "/api/login?"+q.Encode(), strings.NewReader(`{"type":"code"}`))
	r.Header.Set("Origin", a.Config.PublicURL)
	r.AddCookie(&http.Cookie{Name: "forge_login", Value: flow})
	r.AddCookie(&http.Cookie{Name: "forge_session", Value: "private-forge-session"})
	r.AddCookie(&http.Cookie{Name: "forge_identity", Value: "existing-identity"})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "forge_identity" || cookies[0].Value != "renewed-identity" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Domain != "" {
		t.Fatalf("expected one protected identity cookie, got %d", len(cookies))
	}
}

func TestGitLabLoginClientBoundary(t *testing.T) {
	f := setup(t)
	a := f.a
	a.Config.ClientID = "forge-client"
	a.Config.GitLabClientID = "gitlab-client"
	a.Config.GitLabPublicURL = "https://gitlab.example.test"
	a.OAuth = oauth2.Config{RedirectURL: a.Config.PublicURL + "/auth/callback"}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"status":"ok","data":{"name":"gitlab"}}`))
	}))
	defer upstream.Close()
	a.Config.CasdoorURL = upstream.URL
	for _, tc := range []struct {
		name, client, redirect, state, origin string
		want                                  int
	}{
		{"registered GitLab", "gitlab-client", a.Config.GitLabPublicURL + "/users/auth/openid_connect/callback", "state", a.Config.PublicURL, 200},
		{"unregistered client", "foreign-client", a.Config.GitLabPublicURL + "/users/auth/openid_connect/callback", "state", a.Config.PublicURL, 400},
		{"foreign callback", "gitlab-client", "https://evil.example/callback", "state", a.Config.PublicURL, 400},
		{"missing state", "gitlab-client", a.Config.GitLabPublicURL + "/users/auth/openid_connect/callback", "", a.Config.PublicURL, 400},
		{"foreign origin", "gitlab-client", a.Config.GitLabPublicURL + "/users/auth/openid_connect/callback", "state", "https://evil.example", 403},
		{"Forge without login flow", "forge-client", a.OAuth.RedirectURL, "state", a.Config.PublicURL, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := calls
			q := url.Values{"clientId": {tc.client}, "redirectUri": {tc.redirect}, "state": {tc.state}, "type": {"code"}, "responseType": {"code"}}
			r := httptest.NewRequest("POST", "/api/login?"+q.Encode(), strings.NewReader(`{"type":"code"}`))
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
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
	r.Header.Set("Origin", f.a.Config.PublicURL)
	r.Header.Set("X-CSRF-Token", Hash(f.operator+":csrf"))
	r.AddCookie(&http.Cookie{Name: "forge_session", Value: f.operator})
	r.AddCookie(&http.Cookie{Name: "forge_identity", Value: "identity"})
	w := httptest.NewRecorder()
	f.a.Handler().ServeHTTP(w, r)
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

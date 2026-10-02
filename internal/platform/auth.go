package platform

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
)

func (a *App) InitAuth(ctx context.Context) error {
	provider, e := oidc.NewProvider(oidc.ClientContext(ctx, a.HTTP), a.Config.Issuer)
	if e != nil {
		return e
	}
	a.OAuth = oauth2.Config{ClientID: a.Config.ClientID, ClientSecret: a.Config.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: a.Config.PublicURL + "/auth/callback", Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}
	a.Verifier = provider.Verifier(&oidc.Config{ClientID: a.Config.ClientID})
	return nil
}
func (a *App) cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.Config.PublicURL, "https:"), SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	flow, state, nonce, verifier := Token(), Token(), Token(), oauth2.GenerateVerifier()
	back := r.URL.Query().Get("return")
	if back != "/invite" {
		back = "/"
	}
	_, e := a.DB.Exec(r.Context(), `INSERT INTO login_flows(token_hash,state_hash,nonce,verifier,return_path,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '10 minutes')`, Hash(flow), Hash(state), nonce, verifier, back)
	if e != nil {
		fail(w, e)
		return
	}
	a.cookie(w, "forge_login", flow, 600)
	target, e := url.Parse(a.OAuth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)))
	if e != nil {
		fail(w, e)
		return
	}
	// Serve our own authorization page; only the Casdoor JSON endpoints are proxied.
	own, _ := url.Parse(a.Config.PublicURL)
	target.Scheme = own.Scheme
	target.Host = own.Host
	target.Path = "/login/oauth/authorize"
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}
func (a *App) callback(w http.ResponseWriter, r *http.Request) {
	cookie, e := r.Cookie("forge_login")
	if e != nil {
		fail(w, problem(400, "INVALID_LOGIN", "登录请求已失效，请重新登录"))
		return
	}
	var expected, nonce, verifier, back string
	e = a.DB.QueryRow(r.Context(), `DELETE FROM login_flows WHERE token_hash=$1 AND expires_at>now() RETURNING state_hash,nonce,verifier,return_path`, Hash(cookie.Value)).Scan(&expected, &nonce, &verifier, &back)
	a.cookie(w, "forge_login", "", -1)
	if e != nil || subtle.ConstantTimeCompare([]byte(expected), []byte(Hash(r.URL.Query().Get("state")))) != 1 || r.URL.Query().Get("code") == "" {
		fail(w, problem(400, "INVALID_LOGIN", "登录请求已失效，请重新登录"))
		return
	}
	ctx := oidc.ClientContext(r.Context(), a.HTTP)
	token, e := a.OAuth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if e != nil {
		fail(w, problem(401, "TOKEN_EXCHANGE_FAILED", "身份服务未能完成登录，请重新登录"))
		return
	}
	raw, _ := token.Extra("id_token").(string)
	id, e := a.Verifier.Verify(ctx, raw)
	if e != nil || id == nil || id.Subject == "" || id.Nonce != nonce {
		fail(w, problem(401, "INVALID_IDENTITY", "身份校验失败"))
		return
	}
	var uid string
	e = a.DB.QueryRow(r.Context(), `SELECT id FROM users WHERE issuer=$1 AND subject=$2 AND status='active'`, a.Config.Issuer, id.Subject).Scan(&uid)
	if e != nil {
		fail(w, problem(403, "IDENTITY_NOT_ENROLLED", "此账号尚未在 Forge 中激活"))
		return
	}
	session := Token()
	_, e = a.DB.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_hash,expires_at) VALUES($1,$2,$3,now()+interval '8 hours')`, Hash(session), uid, Hash(Hash(session+":csrf")))
	if e != nil {
		fail(w, e)
		return
	}
	a.cookie(w, "forge_session", session, 8*3600)
	http.Redirect(w, r, back, http.StatusSeeOther)
}
func (a *App) authenticate(r *http.Request) (User, string, error) {
	var u User
	c, e := r.Cookie("forge_session")
	if e != nil {
		return u, "", problem(401, "AUTH_REQUIRED", "请先登录")
	}
	e = a.DB.QueryRow(r.Context(), `SELECT u.id,u.name,u.email,u.operator,coalesce(u.subject,'') FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.status='active'`, Hash(c.Value)).Scan(&u.ID, &u.Name, &u.Email, &u.Operator, &u.Subject)
	if errors.Is(e, pgx.ErrNoRows) {
		return u, "", problem(401, "AUTH_REQUIRED", "会话已过期，请重新登录")
	}
	return u, Hash(c.Value + ":csrf"), e
}
func (a *App) origin(r *http.Request) bool { return r.Header.Get("Origin") == a.Config.PublicURL }
func (a *App) authorized(fn func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, csrf, e := a.authenticate(r)
		if e != nil {
			fail(w, e)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !a.origin(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1 {
				fail(w, problem(403, "CSRF_REJECTED", "请求来源校验失败，请刷新页面"))
				return
			}
		}
		fn(w, r, u)
	}
}
func (a *App) identityProxy(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("clientId") != a.Config.ClientID || q.Get("redirectUri") != a.OAuth.RedirectURL || q.Get("type") != "code" {
		fail(w, problem(400, "INVALID_LOGIN_CLIENT", "登录请求无效"))
		return
	}
	c, e := r.Cookie("forge_login")
	if e != nil {
		fail(w, problem(400, "INVALID_LOGIN", "请重新发起登录"))
		return
	}
	var ok bool
	e = a.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM login_flows WHERE token_hash=$1 AND state_hash=$2 AND expires_at>now())`, Hash(c.Value), Hash(q.Get("state"))).Scan(&ok)
	if e != nil || !ok {
		fail(w, problem(400, "INVALID_LOGIN", "登录请求已过期"))
		return
	}
	if r.Method == "POST" && !a.origin(r) {
		fail(w, problem(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	req, e := http.NewRequestWithContext(r.Context(), r.Method, a.Config.CasdoorURL+r.URL.RequestURI(), http.MaxBytesReader(w, r.Body, 65536))
	if e != nil {
		fail(w, e)
		return
	}
	issuer, _ := url.Parse(a.Config.Issuer)
	req.Host = issuer.Host
	req.Header.Set("X-Forwarded-Proto", issuer.Scheme)
	req.Header.Set("Content-Type", "application/json")
	resp, e := a.HTTP.Do(req)
	if e != nil {
		fail(w, problem(502, "IDENTITY_UNAVAILABLE", "身份服务暂不可用"))
		return
	}
	defer resp.Body.Close()
	// Never forward management credentials or a third-party redirect to the browser.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(mustReadLimited(resp.Body))
}

package tenant

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

func (a *Controller) cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     name,
			Value:    value,
			Path:     "/",
			HttpOnly: true,
			Secure:   strings.HasPrefix(a.service.Config.PublicURL, "https:"),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   age,
		},
	)
}

func (a *Controller) origin(r *http.Request) bool {
	return r.Header.Get("Origin") == a.service.Config.PublicURL
}

func (a *Controller) authorized(fn func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, csrf, e := a.authenticate(r)
		if e != nil {
			fail(w, e)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !a.origin(r) ||
				subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1 {
				fail(w, problem(403, "CSRF_REJECTED", "请求来源校验失败，请刷新页面"))
				return
			}
		}
		fn(w, r, u)
	}
}

func (a *Controller) me(w http.ResponseWriter, r *http.Request, u User) {
	_, csrf, e := a.authenticate(r)
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, Object{"user": u, "csrf_token": csrf, "permissions": Object{"managePlatform": u.Operator}})
}

func (a *Controller) authenticate(r *http.Request) (User, string, error) {
	cookie, err := r.Cookie("forge_session")
	if err != nil {
		return User{}, "", problem(401, "AUTH_REQUIRED", "请先登录")
	}
	return a.service.authenticate(r.Context(), cookie.Value)
}

func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (a *Controller) login(w http.ResponseWriter, r *http.Request) {
	flow, target, err := a.service.beginLogin(r.Context(), r.URL.Query().Get("return"))
	if err != nil {
		fail(w, err)
		return
	}
	a.cookie(w, "forge_login", flow, 600)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *Controller) callback(w http.ResponseWriter, r *http.Request) {
	flow := cookieValue(r, "forge_login")
	if flow == "" {
		fail(w, problem(400, "INVALID_LOGIN", "登录请求已失效，请重新登录"))
		return
	}
	a.cookie(w, "forge_login", "", -1)
	session, back, err := a.service.completeLogin(
		r.Context(),
		flow,
		r.URL.Query().Get("state"),
		r.URL.Query().Get("code"),
	)
	if err != nil {
		fail(w, err)
		return
	}
	a.cookie(w, "forge_session", session, 8*3600)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (a *Controller) identityProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" && !a.origin(r) {
		fail(w, problem(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	q := r.URL.Query()
	result, err := a.service.identityLogin(
		r.Context(),
		q.Get("clientId"),
		q.Get("redirectUri"),
		q.Get("type"),
		q.Get("responseType"),
		q.Get("state"),
		cookieValue(r, "forge_login"),
		r.Method,
		r.URL.RequestURI(),
		cookieValue(r, "forge_identity"),
		http.MaxBytesReader(w, r.Body, 65536),
	)
	if err != nil {
		fail(w, err)
		return
	}
	for _, value := range result.Cookies {
		http.SetCookie(
			w,
			&http.Cookie{
				Name:     "forge_identity",
				Value:    value.Value,
				Path:     "/",
				MaxAge:   value.MaxAge,
				Expires:  value.Expires,
				HttpOnly: true,
				Secure:   strings.HasPrefix(a.service.Config.PublicURL, "https:"),
				SameSite: http.SameSiteLaxMode,
			},
		)
	}
	if r.URL.Path == "/api/get-app-login" && result.Status == 200 {
		var body Object
		if json.Unmarshal(result.Body, &body) != nil {
			fail(w, problem(502, "INVALID_IDENTITY_RESPONSE", "身份服务返回无效响应"))
			return
		}
		body["sessionAvailable"] = cookieValue(r, "forge_identity") != ""
		JSON(w, result.Status, body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.Status)
	_, _ = w.Write(result.Body)
}

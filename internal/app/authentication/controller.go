package authentication

import (
	"encoding/json"
	"net/http"
	"strings"

	server "github.com/FlyTOmeLight/server-go"
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) cookie(c *core.Context, name, value string, age int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, value, age, "/", "", strings.HasPrefix(h.service.Config.PublicURL, "https:"), true)
}

func (h *Controller) login(c *core.Context) {
	flow, target, err := h.service.BeginLogin(c.Request.Context(), c.Query("return"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	h.cookie(c, "forge_login", flow, 600)
	c.Redirect(http.StatusSeeOther, target)
}

func (h *Controller) callback(c *core.Context) {
	flow, _ := c.Cookie("forge_login")
	if flow == "" {
		httpapi.Fail(c, apperror.New(400, "INVALID_LOGIN", "登录请求已失效，请重新登录"))
		return
	}
	h.cookie(c, "forge_login", "", -1)
	session, back, err := h.service.CompleteLogin(c.Request.Context(), flow, c.Query("state"), c.Query("code"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	h.cookie(c, "forge_session", session, 8*3600)
	c.Redirect(http.StatusSeeOther, back)
}

func (h *Controller) identityProxy(c *core.Context) {
	if c.Request.Method == "POST" && c.GetHeader("Origin") != h.service.Config.PublicURL {
		httpapi.Fail(c, apperror.New(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	flow, _ := c.Cookie("forge_login")
	session, _ := c.Cookie("forge_identity")
	result, err := h.service.IdentityLogin(
		c.Request.Context(),
		c.Query("clientId"),
		c.Query("redirectUri"),
		c.Query("type"),
		c.Query("responseType"),
		c.Query("state"),
		flow,
		c.Request.Method,
		c.Request.URL.RequestURI(),
		session,
		http.MaxBytesReader(c.Writer, c.Request.Body, 65536),
	)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	for _, value := range result.Cookies {
		http.SetCookie(
			c.Writer,
			&http.Cookie{
				Name:     "forge_identity",
				Value:    value.Value,
				Path:     "/",
				MaxAge:   value.MaxAge,
				Expires:  value.Expires,
				HttpOnly: true,
				Secure:   strings.HasPrefix(h.service.Config.PublicURL, "https:"),
				SameSite: http.SameSiteLaxMode,
			},
		)
	}
	if c.Request.URL.Path == "/api/get-app-login" && result.Status == 200 {
		var body map[string]any
		if json.Unmarshal(result.Body, &body) != nil {
			httpapi.Fail(c, apperror.New(502, "INVALID_IDENTITY_RESPONSE", "身份服务返回无效响应"))
			return
		}
		body["sessionAvailable"] = session != ""
		c.JSON(result.Status, body)
		return
	}
	c.Data(result.Status, "application/json", result.Body)
}

func (h *Controller) logout(c *core.Context) {
	session, _ := c.Cookie("forge_session")
	if err := h.service.Logout(c.Request.Context(), session); err != nil {
		httpapi.Fail(c, err)
		return
	}
	h.cookie(c, "forge_session", "", -1)
	h.cookie(c, "forge_identity", "", -1)
	server.OK(c, map[string]any{"ok": true})
}

func (h *Controller) me(c *core.Context) {
	u := httpapi.User(c)
	server.OK(
		c,
		map[string]any{
			"user":        u,
			"csrf_token":  c.GetString("forge.csrf"),
			"permissions": map[string]any{"managePlatform": u.Operator},
		},
	)
}

func (h *Controller) resolveIdentity(c *core.Context) {
	var req tenantv1.ResolveIdentityRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.service.ResolveIdentity(c.Request.Context(), req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) resolveScope(c *core.Context) {
	var req tenantv1.ResolveScopeRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.service.ResolveScope(c.Request.Context(), req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

package authentication

import core "github.com/FlyTOmeLight/server-go/pkg"

func (h *Controller) RegisterRoutes(root, session, internal *core.RouterGroup) {
	root.GET("/auth/login", h.login)
	root.GET("/auth/callback", h.callback)
	root.GET("/api/get-app-login", h.identityProxy)
	root.POST("/api/login", h.identityProxy)
	session.POST("/auth/logout", h.logout)
	session.GET("/api/v1/me", h.me)
	internal.POST("/session/check", h.checkSession)
}

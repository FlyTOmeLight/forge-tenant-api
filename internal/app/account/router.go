package account

import "github.com/gin-gonic/gin"

func (h *Controller) RegisterRoutes(root, session, internal *gin.RouterGroup) {
	root.GET("/auth/login", h.login)
	root.GET("/auth/callback", h.callback)
	root.GET("/api/get-app-login", h.identityProxy)
	root.POST("/api/login", h.identityProxy)
	session.POST("/auth/logout", h.logout)
	session.GET("/api/v1/me", h.me)
	session.GET("/api/v1/platform/users", h.users)
	internal.POST("/session/check", h.checkSession)
}

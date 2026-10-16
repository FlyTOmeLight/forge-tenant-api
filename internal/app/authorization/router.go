package authorization

import core "github.com/FlyTOmeLight/server-go/pkg"

func (h *Controller) RegisterRoutes(session, internal *core.RouterGroup) {
	session.GET("/api/v1/roles", h.roles)
	tenant := session.Group("/api/v1/tenants/:tid")
	tenant.GET("/permissions/me", h.memberPermissions)
	tenant.GET("/members/:mid/permissions", h.memberPermissions)
	project := tenant.Group("/projects/:pid")
	project.GET("/access", h.selfAccess)
	internal.POST("/access/check", h.checkAccess)
	internal.GET("/access/events", h.accessEvents)
}

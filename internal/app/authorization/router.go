package authorization

import "github.com/gin-gonic/gin"

func (h *Controller) RegisterRoutes(session, internal *gin.RouterGroup) {
	session.GET("/api/v1/roles", h.roles)
	tenant := session.Group("/api/v1/tenants/:tid")
	tenant.GET("/permissions/me", h.memberPermissions)
	tenant.GET("/members/:mid/permissions", h.memberPermissions)
	project := tenant.Group("/projects/:pid")
	project.GET("/people", h.projectPeople)
	project.GET("/team-grants", h.projectTeamGrants)
	project.GET("/grant-candidates", h.grantCandidates)
	project.GET("/access", h.selfAccess)
	internal.POST("/projects/resolve", h.resolveProject)
	internal.POST("/access/check", h.checkAccess)
	internal.POST("/workspace/check", h.checkWorkspace)
	internal.GET("/access/events", h.accessEvents)
}

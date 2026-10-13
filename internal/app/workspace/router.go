package workspace

import "github.com/gin-gonic/gin"

func (h *Controller) RegisterRoutes(root, session *gin.RouterGroup) {
	invitations := root.Group("/api/v1/invitations")
	invitations.POST("/inspect", h.inspectInvitation)
	invitations.POST("/accept", h.acceptInvitation)
	platform := session.Group("/api/v1/platform/tenants")
	platform.GET("", h.listPlatformTenants)
	platform.POST("", h.createTenant)
	platform.PATCH("/:tid", h.updateTenant)
	tenants := session.Group("/api/v1/tenants")
	tenants.GET("", h.listTenants)
	tenant := tenants.Group("/:tid")
	tenant.GET("", h.snapshot)
	members := tenant.Group("/members")
	members.POST("", h.addMember)
	members.PATCH("/:mid", h.updateMember)
	members.DELETE("/:mid", h.removeMember)
	members.POST("/:mid/invitation", h.resendInvitation)
	teams := tenant.Group("/teams")
	teams.POST("", h.createTeam)
	teams.PUT("/:team/members/:mid", h.setTeamMember)
	teams.DELETE("/:team/members/:mid", h.setTeamMember)
	projects := tenant.Group("/projects")
	projects.POST("", h.addProject)
	project := projects.Group("/:pid")
	project.PUT("/grants/:mid", h.setGrant)
	project.DELETE("/grants/:mid", h.setGrant)
	project.PUT("/team-grants/:team", h.setTeamGrant)
	project.DELETE("/team-grants/:team", h.setTeamGrant)
	tenant.POST("/operations/:oid/retry", h.retryOperation)
}

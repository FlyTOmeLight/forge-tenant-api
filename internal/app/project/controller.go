package project

import (
	server "github.com/FlyTOmeLight/server-go"
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) addProject(c *core.Context) { tenantwrite.Execute(c, h.service.AddProject) }

func (h *Controller) setGrant(c *core.Context) { tenantwrite.Execute(c, h.service.SetGrant) }

func (h *Controller) setTeamGrant(c *core.Context) { tenantwrite.Execute(c, h.service.SetTeamGrant) }

func (h *Controller) RegisterRoutes(session, internal *core.RouterGroup) {
	projects := session.Group("/api/v1/tenants/:tid/projects")
	projects.POST("", h.addProject)
	internal.POST("/projects/resolve", h.resolveProject)
	project := projects.Group("/:pid")
	project.GET("/people", h.projectPeople)
	project.GET("/team-grants", h.projectTeamGrants)
	project.GET("/grant-candidates", h.grantCandidates)
	project.PUT("/grants/:mid", h.setGrant)
	project.DELETE("/grants/:mid", h.setGrant)
	project.PUT("/team-grants/:team", h.setTeamGrant)
	project.DELETE("/team-grants/:team", h.setTeamGrant)
}

func (h *Controller) projectPeople(c *core.Context) {
	result, err := h.service.ProjectPeople(c.Request.Context(), httpapi.User(c), c.Param("tid"), c.Param("pid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) projectTeamGrants(c *core.Context) {
	result, err := h.service.ProjectTeamGrants(c.Request.Context(), httpapi.User(c), c.Param("tid"), c.Param("pid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) grantCandidates(c *core.Context) {
	result, err := h.service.GrantCandidates(c.Request.Context(), httpapi.User(c), c.Param("tid"), c.Param("pid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) resolveProject(c *core.Context) {
	var req tenantv1.ResolveProjectRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.service.ResolveProject(c.Request.Context(), req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

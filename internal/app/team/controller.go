package team

import (
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) createTeam(c *core.Context) { tenantwrite.Execute(c, h.service.CreateTeam) }

func (h *Controller) setTeamMember(c *core.Context) { tenantwrite.Execute(c, h.service.SetTeamMember) }

func (h *Controller) RegisterRoutes(session *core.RouterGroup) {
	teams := session.Group("/api/v1/tenants/:tid/teams")
	teams.POST("", h.createTeam)
	teams.PUT("/:team/members/:mid", h.setTeamMember)
	teams.DELETE("/:team/members/:mid", h.setTeamMember)
}

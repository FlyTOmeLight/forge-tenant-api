package member

import (
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) addMember(c *core.Context) { tenantwrite.Execute(c, h.service.AddMember) }

func (h *Controller) updateMember(c *core.Context) { tenantwrite.Execute(c, h.service.UpdateMember) }

func (h *Controller) removeMember(c *core.Context) { tenantwrite.Execute(c, h.service.RemoveMember) }

func (h *Controller) resendInvitation(c *core.Context) {
	tenantwrite.Execute(c, h.service.ResendInvitation)
}

func (h *Controller) RegisterRoutes(session *core.RouterGroup) {
	members := session.Group("/api/v1/tenants/:tid/members")
	members.POST("", h.addMember)
	members.PATCH("/:mid", h.updateMember)
	members.DELETE("/:mid", h.removeMember)
	members.POST("/:mid/invitation", h.resendInvitation)
}

package invitation

import (
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) inspectInvitation(c *core.Context) {
	if c.GetHeader("Origin") != h.service.Config.PublicURL {
		httpapi.Fail(c, apperror.New(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	var req InvitationRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.service.InspectInvitation(c.Request.Context(), req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(200, result)
}

func (h *Controller) acceptInvitation(c *core.Context) {
	if c.GetHeader("Origin") != h.service.Config.PublicURL {
		httpapi.Fail(c, apperror.New(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	var req InvitationRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	session, _ := c.Cookie("forge_session")
	result, err := h.service.AcceptInvitation(c.Request.Context(), req, session)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(202, result)
}

func (h *Controller) RegisterRoutes(root *core.RouterGroup) {
	invitations := root.Group("/api/v1/invitations")
	invitations.POST("/inspect", h.inspectInvitation)
	invitations.POST("/accept", h.acceptInvitation)
}

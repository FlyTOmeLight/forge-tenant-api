package workspace

import (
	server "github.com/FlyTOmeLight/server-go"
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) snapshot(c *core.Context) {
	result, err := h.service.Snapshot(c.Request.Context(), httpapi.User(c), c.Param("tid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) RegisterRoutes(session, internal *core.RouterGroup) {
	session.GET("/api/v1/tenants/:tid", h.snapshot)
	internal.POST("/workspace/check", h.checkWorkspace)
}

func (h *Controller) checkWorkspace(c *core.Context) {
	var req tenantv1.CheckWorkspaceRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.service.CheckWorkspace(c.Request.Context(), req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

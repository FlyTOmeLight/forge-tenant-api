package tenant

import (
	server "github.com/FlyTOmeLight/server-go"
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) listTenants(c *core.Context) {
	result, err := h.service.ListTenants(c.Request.Context(), httpapi.User(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) listPlatformTenants(c *core.Context) {
	result, err := h.service.ListPlatformTenants(c.Request.Context(), httpapi.User(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) createTenant(c *core.Context) { tenantwrite.Execute(c, h.service.CreateTenant) }

func (h *Controller) updateTenant(c *core.Context) { tenantwrite.Execute(c, h.service.UpdateTenant) }

func (h *Controller) RegisterRoutes(session *core.RouterGroup) {
	platform := session.Group("/api/v1/platform/tenants")
	platform.GET("", h.listPlatformTenants)
	platform.POST("", h.createTenant)
	platform.PATCH("/:tid", h.updateTenant)
	session.GET("/api/v1/tenants", h.listTenants)
}

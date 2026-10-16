package synchronization

import (
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type Controller struct{ service *RetryService }

func NewController(service *RetryService) *Controller { return &Controller{service: service} }

func (h *Controller) retryOperation(c *core.Context) {
	tenantwrite.Execute(c, h.service.RetryOperation)
}

func (h *Controller) RegisterRoutes(session *core.RouterGroup) {
	session.POST("/api/v1/tenants/:tid/operations/:oid/retry", h.retryOperation)
}

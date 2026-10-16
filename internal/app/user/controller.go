package user

import (
	server "github.com/FlyTOmeLight/server-go"
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) users(c *core.Context) {
	result, err := h.service.ListUsers(c.Request.Context(), httpapi.User(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

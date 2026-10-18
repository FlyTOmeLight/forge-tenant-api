package credential

import (
	"time"

	server "github.com/FlyTOmeLight/server-go"
	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

type CreateUserRequest struct {
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at"`
}

func (h *Controller) RegisterRoutes(session *core.RouterGroup) {
	credentials := session.Group("/api/v1/credentials")
	credentials.POST("", h.create)
	credentials.GET("", h.list)
	credentials.DELETE("/:id", h.revoke)
}

func (h *Controller) create(c *core.Context) {
	var req CreateUserRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	expiresAt, err := Expiry(req.ExpiresAt, 90*24*time.Hour)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	issued, err := h.service.IssueUser(c.Request.Context(), httpapi.User(c).ID, req.Name, expiresAt)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(201, issued)
}

func (h *Controller) list(c *core.Context) {
	items, err := h.service.ListUser(c.Request.Context(), httpapi.User(c).ID)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, map[string]any{"items": items})
}

func (h *Controller) revoke(c *core.Context) {
	user := httpapi.User(c).ID
	id := c.Param("id")
	if err := h.service.RevokeUser(c.Request.Context(), user, id); err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, map[string]any{"id": id, "revoked": true})
}

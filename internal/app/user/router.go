package user

import core "github.com/FlyTOmeLight/server-go/pkg"

func (h *Controller) RegisterRoutes(session *core.RouterGroup) {
	session.GET("/api/v1/platform/users", h.users)
}

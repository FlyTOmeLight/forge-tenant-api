package service

import (
	"github.com/FlyTOmeLight/server-go/auth"
	"github.com/gin-gonic/gin"
)

func (s *API) RegisterRoutes(root *gin.RouterGroup) error {
	root.GET("/readyz", s.readiness)
	session := root.Group("", s.session)
	internal := root.Group(
		"/internal/v1",
		auth.BearerTokens(map[string]string{s.config.ServiceToken: "forge-service"}),
	)
	s.accounts.RegisterRoutes(root, session, internal)
	s.workspaces.RegisterRoutes(root, session)
	s.authorization.RegisterRoutes(session, internal)
	return nil
}

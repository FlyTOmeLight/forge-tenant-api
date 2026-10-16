package service

import (
	"github.com/FlyTOmeLight/server-go/auth"
	core "github.com/FlyTOmeLight/server-go/pkg"
)

func (s *API) RegisterRoutes(root *core.RouterGroup) error {
	root.GET("/readyz", s.readiness)
	session := root.Group("", s.session)
	internal := root.Group(
		"/internal/v1",
		auth.BearerTokens(map[string]string{s.config.ServiceToken: "forge-service"}),
	)
	s.accounts.RegisterRoutes(root, session, internal)
	s.users.RegisterRoutes(session)
	s.tenants.RegisterRoutes(session)
	s.members.RegisterRoutes(session)
	s.teams.RegisterRoutes(session)
	s.projects.RegisterRoutes(session, internal)
	s.invitations.RegisterRoutes(root)
	s.workspaces.RegisterRoutes(session, internal)
	s.synchronization.RegisterRoutes(session)
	s.authorization.RegisterRoutes(session, internal)
	return nil
}

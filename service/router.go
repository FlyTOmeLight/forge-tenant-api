package service

import (
	core "github.com/FlyTOmeLight/server-go/pkg"
)

func (s *API) RegisterRoutes(root *core.RouterGroup) error {
	root.GET("/readyz", s.readiness)
	cookie := root.Group("", s.session)
	identity := root.Group("", s.identity)
	internal := root.Group("/internal/v1", s.serviceAuth)
	s.accounts.RegisterRoutes(root, cookie, internal)
	s.credentials.RegisterRoutes(cookie)
	s.users.RegisterRoutes(identity)
	s.tenants.RegisterRoutes(identity)
	s.members.RegisterRoutes(identity)
	s.teams.RegisterRoutes(identity)
	s.projects.RegisterRoutes(identity, internal)
	s.invitations.RegisterRoutes(root)
	s.workspaces.RegisterRoutes(identity, internal)
	s.synchronization.RegisterRoutes(identity)
	s.authorization.RegisterRoutes(identity, internal)
	return nil
}

package tenant

import (
	"context"
	"net/http"

	"github.com/FlyTOmeLight/server-go/auth"
	"github.com/gin-gonic/gin"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (a *Controller) RegisterRoutes(m *gin.RouterGroup) error {
	route(m, "GET", "/readyz", a.readiness)
	internal := m.Group(
		"/internal/v1",
		auth.BearerTokens(map[string]string{a.service.Config.ServiceToken: "forge-service"}),
	)
	route(internal, "POST", "/session/check", a.checkSession)
	route(internal, "POST", "/projects/resolve", a.resolveProject)
	route(internal, "POST", "/access/check", a.checkAccess)
	route(internal, "POST", "/workspace/check", a.checkWorkspace)
	route(internal, "GET", "/access/events", a.accessEvents)

	route(m, "GET", "/auth/login", a.login)
	route(m, "GET", "/auth/callback", a.callback)
	route(m, "GET", "/api/get-app-login", a.identityProxy)
	route(m, "POST", "/api/login", a.identityProxy)
	route(m, "POST", "/auth/logout", a.authorized(func(w http.ResponseWriter, r *http.Request, u User) {
		c, _ := r.Cookie("forge_session")
		e := a.service.logout(r.Context(), c.Value)
		if e != nil {
			fail(w, e)
			return
		}
		a.cookie(w, "forge_session", "", -1)
		a.cookie(w, "forge_identity", "", -1)
		JSON(w, 200, Object{"ok": true})
	}))
	route(m, "GET", "/api/v1/me", a.authorized(a.me))
	route(m, "GET", "/api/v1/platform/users", a.authorized(a.listUsers))
	route(m, "GET", "/api/v1/platform/tenants", a.authorized(a.listPlatformTenants))
	route(m, "GET", "/api/v1/tenants/:tid/projects/:pid/people", a.authorized(a.projectPeople))
	route(m, "GET", "/api/v1/tenants", a.authorized(a.listTenants))
	route(m, "GET", "/api/v1/roles", a.authorized(a.roles))
	route(m, "GET", "/api/v1/tenants/:tid/permissions/me", a.authorized(a.memberPermissions))
	route(m, "GET", "/api/v1/tenants/:tid/members/:mid/permissions", a.authorized(a.memberPermissions))
	route(
		m,
		"PUT",
		"/api/v1/tenants/:tid/teams/:team/members/:mid",
		a.authorized(a.mutate("add_team_member", a.service.setTeamMember)),
	)
	route(
		m,
		"DELETE",
		"/api/v1/tenants/:tid/teams/:team/members/:mid",
		a.authorized(a.mutate("remove_team_member", a.service.setTeamMember)),
	)
	route(m, "GET", "/api/v1/tenants/:tid/projects/:pid/team-grants", a.authorized(a.projectTeamGrants))
	route(
		m,
		"PUT",
		"/api/v1/tenants/:tid/projects/:pid/team-grants/:team",
		a.authorized(a.mutate("set_team_grant", a.service.setTeamGrant)),
	)
	route(
		m,
		"DELETE",
		"/api/v1/tenants/:tid/projects/:pid/team-grants/:team",
		a.authorized(a.mutate("revoke_team_grant", a.service.setTeamGrant)),
	)
	route(m, "GET", "/api/v1/tenants/:tid", a.authorized(a.snapshot))
	route(m, "GET", "/api/v1/tenants/:tid/projects/:pid/grant-candidates", a.authorized(a.grantCandidates))
	route(m, "GET", "/api/v1/tenants/:tid/projects/:pid/access", a.authorized(a.selfAccess))
	route(m, "POST", "/api/v1/platform/tenants", a.authorized(a.mutate("create_tenant", a.service.createTenant)))
	route(
		m,
		"PATCH",
		"/api/v1/platform/tenants/:tid",
		a.authorized(a.mutate("update_tenant", a.service.updateTenant)),
	)
	route(m, "POST", "/api/v1/tenants/:tid/members", a.authorized(a.mutate("invite_member", a.service.addMember)))
	route(
		m,
		"PATCH",
		"/api/v1/tenants/:tid/members/:mid",
		a.authorized(a.mutate("update_member", a.service.updateMember)),
	)
	route(
		m,
		"DELETE",
		"/api/v1/tenants/:tid/members/:mid",
		a.authorized(a.mutate("remove_member", a.service.removeMember)),
	)
	route(
		m,
		"POST",
		"/api/v1/tenants/:tid/members/:mid/invitation",
		a.authorized(a.mutate("resend_invitation", a.service.resendInvitation)),
	)
	route(m, "POST", "/api/v1/tenants/:tid/teams", a.authorized(a.mutate("create_team", a.service.createTeam)))
	route(m, "POST", "/api/v1/tenants/:tid/projects", a.authorized(a.mutate("add_project", a.service.addProject)))
	route(
		m,
		"PUT",
		"/api/v1/tenants/:tid/projects/:pid/grants/:mid",
		a.authorized(a.mutate("set_grant", a.service.setGrant)),
	)
	route(
		m,
		"DELETE",
		"/api/v1/tenants/:tid/projects/:pid/grants/:mid",
		a.authorized(a.mutate("revoke_grant", a.service.setGrant)),
	)
	route(
		m,
		"POST",
		"/api/v1/tenants/:tid/operations/:oid/retry",
		a.authorized(a.mutate("retry_sync", a.service.retryOperation)),
	)
	route(m, "POST", "/api/v1/invitations/inspect", a.invitationInfo)
	route(m, "POST", "/api/v1/invitations/accept", a.acceptInvitation)
	return nil
}

func (a *Controller) Start(context.Context) error { return nil }

func (a *Controller) Stop(context.Context) error { return nil }

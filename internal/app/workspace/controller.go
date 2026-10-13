package workspace

import (
	"context"

	server "github.com/FlyTOmeLight/server-go"
	"github.com/gin-gonic/gin"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) listTenants(c *gin.Context) {
	result, err := h.service.ListTenants(c.Request.Context(), httpapi.User(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) listPlatformTenants(c *gin.Context) {
	result, err := h.service.ListPlatformTenants(c.Request.Context(), httpapi.User(c))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) snapshot(c *gin.Context) {
	result, err := h.service.Snapshot(c.Request.Context(), httpapi.User(c), c.Param("tid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func execute[T any](c *gin.Context, fn func(context.Context, identity.User, Change, T) (Receipt, error)) {
	var req T
	raw, err := httpapi.MutationBody(c, &req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	change := Change{
		TenantID:    c.Param("tid"),
		ProjectID:   c.Param("pid"),
		MemberID:    c.Param("mid"),
		TeamID:      c.Param("team"),
		OperationID: c.Param("oid"),
		Remove:      c.Request.Method == "DELETE",
		Key:         c.GetHeader("Idempotency-Key"),
		Version:     c.GetHeader("If-Match"),
		Route:       c.Request.Method + " " + c.Request.URL.Path,
		Target:      c.Request.URL.Path,
		Raw:         raw,
	}
	result, err := fn(c.Request.Context(), httpapi.User(c), change, req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Data(result.Status, "application/json", result.Data)
}

func (h *Controller) createTenant(c *gin.Context) { execute(c, h.service.CreateTenant) }

func (h *Controller) updateTenant(c *gin.Context) { execute(c, h.service.UpdateTenant) }

func (h *Controller) addMember(c *gin.Context) { execute(c, h.service.AddMember) }

func (h *Controller) updateMember(c *gin.Context) { execute(c, h.service.UpdateMember) }

func (h *Controller) removeMember(c *gin.Context) { execute(c, h.service.RemoveMember) }

func (h *Controller) resendInvitation(c *gin.Context) { execute(c, h.service.ResendInvitation) }

func (h *Controller) createTeam(c *gin.Context) { execute(c, h.service.CreateTeam) }

func (h *Controller) setTeamMember(c *gin.Context) { execute(c, h.service.SetTeamMember) }

func (h *Controller) addProject(c *gin.Context) { execute(c, h.service.AddProject) }

func (h *Controller) setGrant(c *gin.Context) { execute(c, h.service.SetGrant) }

func (h *Controller) setTeamGrant(c *gin.Context) { execute(c, h.service.SetTeamGrant) }

func (h *Controller) retryOperation(c *gin.Context) { execute(c, h.service.RetryOperation) }

func (h *Controller) inspectInvitation(c *gin.Context) {
	if c.GetHeader("Origin") != h.service.Config.PublicURL {
		httpapi.Fail(c, apperror.New(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	var req InvitationRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.service.InspectInvitation(c.Request.Context(), req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(200, result)
}

func (h *Controller) acceptInvitation(c *gin.Context) {
	if c.GetHeader("Origin") != h.service.Config.PublicURL {
		httpapi.Fail(c, apperror.New(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	var req InvitationRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	session, _ := c.Cookie("forge_session")
	result, err := h.service.AcceptInvitation(c.Request.Context(), req, session)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.JSON(202, result)
}

package authorization

import (
	"strconv"

	server "github.com/FlyTOmeLight/server-go"
	"github.com/gin-gonic/gin"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

type Controller struct{ service *Service }

func NewController(service *Service) *Controller { return &Controller{service: service} }

func (h *Controller) roles(c *gin.Context) { server.OK(c, map[string]any{"items": RoleDefinitions()}) }

func (h *Controller) memberPermissions(c *gin.Context) {
	result, err := h.service.MemberPermissions(c.Request.Context(), httpapi.User(c), c.Param("tid"), c.Param("mid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) projectPeople(c *gin.Context) {
	result, err := h.service.ProjectPeople(c.Request.Context(), httpapi.User(c), c.Param("tid"), c.Param("pid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) projectTeamGrants(c *gin.Context) {
	result, err := h.service.ProjectTeamGrants(c.Request.Context(), httpapi.User(c), c.Param("tid"), c.Param("pid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) grantCandidates(c *gin.Context) {
	result, err := h.service.GrantCandidates(c.Request.Context(), httpapi.User(c), c.Param("tid"), c.Param("pid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) selfAccess(c *gin.Context) {
	result, err := h.service.Access(c.Request.Context(), httpapi.User(c).ID, c.Param("tid"), c.Param("pid"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if result.Reason == "NO_GRANT" {
		server.OK(c, map[string]any{"allowed": false, "level": 0, "reason": result.Reason})
		return
	}
	server.OK(c, result)
}

func (h *Controller) resolveProject(c *gin.Context) {
	var req tenantv1.ResolveProjectRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.service.ResolveProject(c.Request.Context(), req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) checkWorkspace(c *gin.Context) {
	var req tenantv1.CheckWorkspaceRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.service.CheckWorkspace(c.Request.Context(), req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

func (h *Controller) checkAccess(c *gin.Context) {
	var req tenantv1.CheckAccessRequest
	if err := httpapi.Bind(c, &req); err != nil {
		httpapi.Fail(c, err)
		return
	}
	result, err := h.service.CheckAccess(c.Request.Context(), req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if result.Reason == "NO_GRANT" {
		server.OK(c, map[string]any{"allowed": result.Allowed, "level": result.Level, "reason": result.Reason})
		return
	}
	server.OK(c, result)
}

func (h *Controller) accessEvents(c *gin.Context) {
	after := int64(0)
	var err error
	if c.Query("after") != "" {
		after, err = strconv.ParseInt(c.Query("after"), 10, 64)
	}
	if err != nil || after < 0 {
		httpapi.Fail(c, apperror.New(422, "INVALID_CURSOR", "游标无效"))
		return
	}
	result, err := h.service.AccessEvents(c.Request.Context(), after)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	server.OK(c, result)
}

package tenant

import (
	"net/http"

	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func (a *Controller) checkSession(w http.ResponseWriter, r *http.Request) {
	var in tenantv1.CheckSessionRequest
	if err := readJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	result, err := a.service.checkSession(r.Context(), in)
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) resolveProject(w http.ResponseWriter, r *http.Request) {
	var in tenantv1.ResolveProjectRequest
	if err := readJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	result, err := a.service.resolveProject(r.Context(), in)
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) checkWorkspace(w http.ResponseWriter, r *http.Request) {
	var in tenantv1.CheckWorkspaceRequest
	if err := readJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	result, err := a.service.checkWorkspace(r.Context(), in)
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) checkAccess(w http.ResponseWriter, r *http.Request) {
	var in tenantv1.CheckAccessRequest
	if err := readJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	result, err := a.service.checkAccess(r.Context(), in)
	if err != nil {
		fail(w, err)
		return
	}
	if result.Reason == "NO_GRANT" {
		JSON(w, 200, Object{"allowed": result.Allowed, "level": result.Level, "reason": result.Reason})
		return
	}
	JSON(w, 200, result)
}

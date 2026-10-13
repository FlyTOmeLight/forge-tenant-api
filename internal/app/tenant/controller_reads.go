package tenant

import (
	"net/http"
)

func (a *Controller) listUsers(w http.ResponseWriter, r *http.Request, u User) {
	result, err := a.service.listUsers(r.Context(), u)
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) listTenants(w http.ResponseWriter, r *http.Request, u User) {
	result, err := a.service.listTenants(r.Context(), u)
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) listPlatformTenants(w http.ResponseWriter, r *http.Request, u User) {
	result, err := a.service.listPlatformTenants(r.Context(), u)
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) snapshot(w http.ResponseWriter, r *http.Request, u User) {
	result, err := a.service.snapshot(r.Context(), u, r.PathValue("tid"))
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) grantCandidates(w http.ResponseWriter, r *http.Request, u User) {
	result, err := a.service.grantCandidates(r.Context(), u, r.PathValue("tid"), r.PathValue("pid"))
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) projectPeople(w http.ResponseWriter, r *http.Request, u User) {
	result, err := a.service.projectPeople(r.Context(), u, r.PathValue("tid"), r.PathValue("pid"))
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) projectTeamGrants(w http.ResponseWriter, r *http.Request, u User) {
	result, err := a.service.projectTeamGrants(r.Context(), u, r.PathValue("tid"), r.PathValue("pid"))
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) memberPermissions(w http.ResponseWriter, r *http.Request, u User) {
	result, err := a.service.memberPermissions(r.Context(), u, r.PathValue("tid"), r.PathValue("mid"))
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

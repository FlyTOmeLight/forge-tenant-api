package tenant

import (
	"net/http"
)

func (a *Controller) invitationInfo(w http.ResponseWriter, r *http.Request) {
	if !a.origin(r) {
		fail(w, problem(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	var in invitationInput
	if err := readJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	result, err := a.service.invitationInfo(r.Context(), in)
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	if !a.origin(r) {
		fail(w, problem(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	var in invitationInput
	if err := readJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	result, err := a.service.acceptInvitation(r.Context(), in, cookieValue(r, "forge_session"))
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 202, result)
}

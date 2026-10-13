package tenant

import (
	"net/http"
	"strconv"
)

func (a *Controller) selfAccess(w http.ResponseWriter, r *http.Request, u User) {
	result, err := a.service.access(r.Context(), u.ID, r.PathValue("tid"), r.PathValue("pid"))
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

func (a *Controller) accessEvents(w http.ResponseWriter, r *http.Request) {
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if r.URL.Query().Get("after") == "" {
		after = 0
		err = nil
	}
	if err != nil || after < 0 {
		fail(w, problem(422, "INVALID_CURSOR", "游标无效"))
		return
	}
	result, err := a.service.accessEvents(r.Context(), after)
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, result)
}

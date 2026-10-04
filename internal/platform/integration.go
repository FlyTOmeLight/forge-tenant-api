package platform

import (
	"crypto/subtle"
	"net/http"
)

func (a *App) checkSession(w http.ResponseWriter, r *http.Request) {
	var in struct{ Session, Method, Origin, CSRF string }
	if e := readJSON(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	request := r.Clone(r.Context())
	request.Header = r.Header.Clone()
	request.Header.Del("Cookie")
	request.AddCookie(&http.Cookie{Name: "forge_session", Value: in.Session})
	u, csrf, e := a.authenticate(request)
	if e != nil {
		fail(w, e)
		return
	}
	if in.Method != "GET" && in.Method != "HEAD" {
		if in.Origin != a.Config.PublicURL || subtle.ConstantTimeCompare([]byte(in.CSRF), []byte(csrf)) != 1 {
			fail(w, problem(403, "CSRF_REJECTED", "请求来源校验失败"))
			return
		}
	}
	JSON(w, 200, u)
}
func (a *App) resolveProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TenantID  string `json:"tenant_id"`
		ProjectID string `json:"project_id"`
	}
	if e := readJSON(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	var id, path string
	var gitlab int64
	e := a.DB.QueryRow(r.Context(), `SELECT id,path,gitlab_id FROM projects WHERE tenant_id=$1 AND id=$2 AND gitlab_id IS NOT NULL`, in.TenantID, in.ProjectID).Scan(&id, &path, &gitlab)
	if e != nil {
		fail(w, problem(404, "NOT_FOUND", "项目不存在或尚未同步"))
		return
	}
	JSON(w, 200, Object{"id": id, "tenant_id": in.TenantID, "path": path, "gitlab_id": gitlab})
}

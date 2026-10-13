package tenant

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupProject(t *testing.T) *fixture {
	f := setup(t)
	e := f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO tenants(id,name,slug,status) VALUES('t','Tenant','task-test','active'); INSERT INTO projects(id,tenant_id,name,path,mode,gitlab_id) VALUES('p','t','Repo','task-test/repo','bind',77); INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('ma','t','operator','admin','active'),('mm','t','member','member','active'); INSERT INTO grants(tenant_id,project_id,membership_id,desired_level,observed_level,effective_level) VALUES('t','p','ma',40,40,40),('t','p','mm',30,30,30);INSERT INTO direct_grants SELECT tenant_id,project_id,membership_id,desired_level FROM grants WHERE desired_level>0`).
		Error
	if e != nil {
		t.Fatal(e)
	}
	return f
}

func TestServiceSessionRequiresCSRFAndServiceIdentity(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		token, method, origin, csrf string
		want                        int
	}{{token: "", method: "GET", origin: "", csrf: "", want: 401}, {token: f.a.Config.ServiceToken, method: "GET", origin: "", csrf: "", want: 200}, {token: f.a.Config.ServiceToken, method: "POST", origin: f.a.Config.PublicURL, csrf: "bad", want: 403}, {token: f.a.Config.ServiceToken, method: "POST", origin: "https://evil.test", csrf: Hash(f.operator + ":csrf"), want: 403}, {token: f.a.Config.ServiceToken, method: "POST", origin: f.a.Config.PublicURL, csrf: Hash(f.operator + ":csrf"), want: 200}} {
		b, _ := json.Marshal(
			Object{"session": f.operator, "method": tc.method, "origin": tc.origin, "csrf": tc.csrf},
		)
		r := httptest.NewRequest("POST", "/internal/v1/session/check", bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		apiHandler(t, f.a).ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(tc.want, w.Code)
		}
	}
	w := httptest.NewRecorder()
	apiHandler(t, f.a).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t/projects/p/tasks", nil))
	if w.Code != 404 {
		t.Fatal("tenant still owns tasks", w.Code)
	}
}

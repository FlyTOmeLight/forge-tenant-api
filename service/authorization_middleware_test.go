package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FlyTOmeLight/server-go/constants"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func TestAuthorizationMiddlewareWithPostgres(t *testing.T) {
	for _, tc := range []struct {
		name            string
		cookie          bool
		tenant, project string
		action          tenantv1.AccessAction
		headerTenant    string
		headerProject   string
		mutate          string
		status          int
	}{
		{name: "identity only", status: 204},
		{name: "tenant member", tenant: "t", status: 204},
		{name: "API key project write", tenant: "t", project: "p", action: tenantv1.AccessWrite, status: 204},
		{name: "Cookie project write", cookie: true, tenant: "t", project: "p", action: tenantv1.AccessWrite, status: 204},
		{name: "project without action", tenant: "t", project: "ungranted", status: 204},
		{name: "scope header agrees", tenant: "t", project: "p", action: tenantv1.AccessWrite, headerTenant: "t", headerProject: "p", status: 204},
		{name: "scope header contradicts", tenant: "t", project: "p", action: tenantv1.AccessWrite, headerTenant: "t", headerProject: "ungranted", status: 400},
		{name: "tenant header contradicts", tenant: "t", headerTenant: "other", status: 400},
		{name: "manage denied", tenant: "t", project: "p", action: tenantv1.AccessManage, status: 403},
		{name: "project without grant", tenant: "t", project: "ungranted", action: tenantv1.AccessRead, status: 403},
		{name: "foreign project", tenant: "t", project: "foreign", action: tenantv1.AccessRead, status: 404},
		{name: "foreign tenant", tenant: "other", project: "foreign", action: tenantv1.AccessRead, status: 403},
		{name: "removed member", tenant: "t", project: "p", action: tenantv1.AccessRead, mutate: `UPDATE memberships SET status='removed' WHERE id='mm'`, status: 403},
		{name: "suspended tenant", tenant: "t", project: "p", action: tenantv1.AccessRead, mutate: `UPDATE tenants SET status='suspended' WHERE id='t'`, status: 403},
		{name: "revoked key", tenant: "t", project: "p", action: tenantv1.AccessRead, mutate: `UPDATE credentials SET revoked_at=now() WHERE owner_kind='user'`, status: 401},
		{name: "disabled user", tenant: "t", project: "p", action: tenantv1.AccessRead, mutate: `UPDATE users SET status='disabled' WHERE id='member'`, status: 401},
		{name: "grant revoked", tenant: "t", project: "p", action: tenantv1.AccessRead, mutate: `UPDATE grants SET desired_level=0,effective_level=0 WHERE membership_id='mm'`, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupProject(t)
			if err := f.a.DB.Exec(
				`INSERT INTO tenants(id,name,slug,status) VALUES('other','Other','other-scope','active'); INSERT INTO projects(id,tenant_id,name,path,mode) VALUES('ungranted','t','Hidden','task-test/hidden','bind'),('foreign','other','Foreign','other/project','bind')`,
			).Error; err != nil {
				t.Fatal(err)
			}
			if tc.mutate != "" {
				if err := f.a.DB.Exec(tc.mutate).Error; err != nil {
					t.Fatal(err)
				}
			}
			upstream := httptest.NewServer(apiHandler(t, f.a))
			defer upstream.Close()
			client, err := tenantv1.New(
				tenantv1.Config{Endpoint: upstream.URL, ServiceAPIKey: f.a.ServiceKey},
			)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			called := false
			handler := tenantv1.Authorization(tenantv1.AuthorizationConfig{
				Client: client, Action: tc.action,
				Scope: func(r *http.Request) (tenantv1.Scope, error) {
					scope := tenantv1.Scope{}
					if id := r.PathValue("tid"); id != "" {
						scope.TenantID = &id
					}
					if id := r.PathValue("pid"); id != "" {
						scope.ProjectID = &id
					}
					return scope, nil
				},
			})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				auth, ok := tenantv1.AuthorizationFromContext(r.Context())
				if !ok || auth.Principal.ID != "member" {
					t.Fatal("missing verified user")
				}
				if (auth.TenantID == nil) != (tc.tenant == "") ||
					(auth.ProjectID == nil) != (tc.project == "") {
					t.Fatal("unexpected optional scope")
				}
				if auth.TenantID != nil && *auth.TenantID != tc.tenant {
					t.Fatal("wrong tenant")
				}
				if auth.ProjectID != nil && *auth.ProjectID != tc.project {
					t.Fatal("wrong project")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodPost, "http://service.test/resource", nil)
			request.SetPathValue("tid", tc.tenant)
			request.SetPathValue("pid", tc.project)
			request.Header.Set(constants.HeaderUserID, "operator")
			if tc.headerTenant != "" {
				request.Header.Set(constants.HeaderTenantID, tc.headerTenant)
			}
			if tc.headerProject != "" {
				request.Header.Set(constants.HeaderProjectID, tc.headerProject)
			}
			if tc.cookie {
				request.AddCookie(&http.Cookie{Name: tenantv1.DefaultSessionCookie, Value: f.member})
				request.Header.Set("Origin", f.a.Account.Config.PublicURL)
				request.Header.Set("X-CSRF-Token", identity.Hash(f.member+":csrf"))
			} else {
				request.Header.Set("Authorization", "Bearer "+f.a.userKey)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || called != (tc.status == 204) {
				t.Fatalf("status=%d called=%v body=%s", response.Code, called, response.Body.String())
			}
		})
	}
}

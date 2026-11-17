package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FlyTOmeLight/server-go/constants"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func TestAuthorizationMiddlewareTenantActionWithPostgres(t *testing.T) {
	for _, tc := range []struct {
		name          string
		admin         bool
		cookie        bool
		action        tenantv1.AccessAction
		project       string
		headerProject string
		mutate        string
		status        int
	}{
		{name: "API key member read", action: tenantv1.AccessRead, status: 204},
		{name: "Cookie member read", cookie: true, action: tenantv1.AccessRead, status: 204},
		{name: "member write denied", action: tenantv1.AccessWrite, status: 403},
		{name: "member manage denied", action: tenantv1.AccessManage, status: 403},
		{name: "admin read", admin: true, action: tenantv1.AccessRead, status: 204},
		{name: "admin write", admin: true, action: tenantv1.AccessWrite, status: 204},
		{name: "admin manage", admin: true, action: tenantv1.AccessManage, status: 204},
		{name: "non-member", action: tenantv1.AccessRead, mutate: `DELETE FROM memberships WHERE id='mm'`, status: 403},
		{name: "platform operator without membership", admin: true, action: tenantv1.AccessManage, mutate: `DELETE FROM memberships WHERE id='ma'`, status: 403},
		{name: "removed member", action: tenantv1.AccessRead, mutate: `UPDATE memberships SET status='removed' WHERE id='mm'`, status: 403},
		{name: "invited member", action: tenantv1.AccessRead, mutate: `UPDATE memberships SET status='invited' WHERE id='mm'`, status: 403},
		{name: "removed admin", admin: true, action: tenantv1.AccessManage, mutate: `UPDATE memberships SET status='removed' WHERE id='ma'`, status: 403},
		{name: "disabled API key user", action: tenantv1.AccessRead, mutate: `UPDATE users SET status='disabled' WHERE id='member'`, status: 401},
		{name: "disabled Cookie user", admin: true, action: tenantv1.AccessManage, mutate: `UPDATE users SET status='disabled' WHERE id='operator'`, status: 401},
		{name: "suspended tenant member", action: tenantv1.AccessRead, mutate: `UPDATE tenants SET status='suspended' WHERE id='t'`, status: 403},
		{name: "suspended tenant admin", admin: true, action: tenantv1.AccessManage, mutate: `UPDATE tenants SET status='suspended' WHERE id='t'`, status: 403},
		{name: "project URL rejected", action: tenantv1.AccessRead, project: "p", status: 400},
		{name: "project header rejected", action: tenantv1.AccessRead, headerProject: "p", status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupTenantAccess(t)
			if tc.mutate != "" {
				if err := f.a.DB.WithContext(f.ctx).Exec(tc.mutate).Error; err != nil {
					t.Fatal(err)
				}
			}
			upstream := httptest.NewServer(apiHandler(t, f.a))
			defer upstream.Close()
			client, err := tenantv1.New(tenantv1.Config{
				Endpoint: upstream.URL, ServiceAPIKey: f.a.ServiceKey,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			user := "member"
			if tc.admin {
				user = "operator"
			}
			called := false
			handler := tenantv1.Authorization(tenantv1.AuthorizationConfig{
				Client: client, RequireTenant: true, TenantAction: tc.action,
				Scope: func(r *http.Request) (tenantv1.Scope, error) {
					tenant := r.PathValue("tid")
					scope := tenantv1.Scope{TenantID: &tenant}
					if project := r.PathValue("pid"); project != "" {
						scope.ProjectID = &project
					}
					return scope, nil
				},
			})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				auth, ok := tenantv1.AuthorizationFromContext(r.Context())
				if !ok || auth.Principal == nil || auth.Principal.ID != user ||
					auth.TenantID == nil || *auth.TenantID != "t" ||
					auth.ProjectID != nil || auth.Action != tc.action {
					t.Fatalf("unexpected tenant authorization context: %+v", auth)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodPost, "http://service.test/resource", nil)
			request.SetPathValue("tid", "t")
			request.SetPathValue("pid", tc.project)
			if tc.headerProject != "" {
				request.Header.Set(constants.HeaderProjectID, tc.headerProject)
			}
			if tc.admin || tc.cookie {
				token := f.member
				if tc.admin {
					token = f.operator
				}
				request.AddCookie(&http.Cookie{Name: tenantv1.DefaultSessionCookie, Value: token})
				request.Header.Set("Origin", f.a.Account.Config.PublicURL)
				request.Header.Set("X-CSRF-Token", identity.Hash(token+":csrf"))
			} else {
				request.Header.Set("Authorization", "Bearer "+f.a.userKey)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || called != (tc.status == http.StatusNoContent) {
				t.Fatalf("status=%d called=%v body=%s", response.Code, called, response.Body.String())
			}
		})
	}
}

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

package service

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func TestScopeResolve(t *testing.T) {
	for _, tc := range []struct {
		name    string
		session string
		method  string
		origin  string
		csrf    string
		tenant  string
		project *string
		mutate  string
		status  int
		code    string
	}{
		{name: "tenant", method: "GET", tenant: "t", status: 200},
		{name: "project without grants or GitLab sync", method: "GET", tenant: "t", project: scopeString("unsynced"), status: 200},
		{name: "write", method: "POST", origin: "valid", csrf: "valid", tenant: "t", status: 200},
		{name: "wrong csrf", method: "POST", origin: "valid", csrf: "wrong", tenant: "t", status: 403, code: "CSRF_REJECTED"},
		{name: "wrong origin", method: "POST", origin: "https://evil.test", csrf: "valid", tenant: "t", status: 403, code: "CSRF_REJECTED"},
		{name: "missing method", tenant: "t", status: 422, code: "INVALID_SCOPE"},
		{name: "invalid method", method: "GET POST", tenant: "t", status: 422, code: "INVALID_SCOPE"},
		{name: "missing tenant", method: "GET", status: 422, code: "INVALID_SCOPE"},
		{name: "empty project", method: "GET", tenant: "t", project: scopeString(""), status: 422, code: "INVALID_SCOPE"},
		{name: "unknown tenant", method: "GET", tenant: "absent", status: 404, code: "SCOPE_NOT_FOUND"},
		{name: "cross tenant project", method: "GET", tenant: "t", project: scopeString("foreign"), status: 404, code: "SCOPE_NOT_FOUND"},
		{name: "unknown project", method: "GET", tenant: "t", project: scopeString("absent"), status: 404, code: "SCOPE_NOT_FOUND"},
		{name: "no membership operator", session: "operator", method: "GET", tenant: "t", status: 403, code: "SCOPE_ACCESS_DENIED"},
		{name: "removed member", method: "GET", tenant: "t", mutate: `UPDATE memberships SET status='removed' WHERE id='mm'`, status: 403, code: "SCOPE_ACCESS_DENIED"},
		{name: "invited member", method: "GET", tenant: "t", mutate: `UPDATE memberships SET status='invited' WHERE id='mm'`, status: 403, code: "SCOPE_ACCESS_DENIED"},
		{name: "suspended tenant", method: "GET", tenant: "t", mutate: `UPDATE tenants SET status='suspended' WHERE id='t'`, status: 403, code: "SCOPE_INACTIVE"},
		{name: "disabled user", method: "GET", tenant: "t", mutate: `UPDATE users SET status='disabled' WHERE id='member'`, status: 401, code: "AUTH_REQUIRED"},
		{name: "expired session", method: "GET", tenant: "t", mutate: `UPDATE sessions SET expires_at=now()-interval '1 second'`, status: 401, code: "AUTH_REQUIRED"},
		{name: "no session", session: "missing", method: "GET", tenant: "t", status: 401, code: "AUTH_REQUIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			if err := f.a.DB.Exec(
				`INSERT INTO tenants(id,name,slug,status) VALUES('t','Tenant','scope-test','active'),('other','Other','other-scope','active'); INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('mm','t','member','member','active'); INSERT INTO projects(id,tenant_id,name,path,mode) VALUES('unsynced','t','Project','scope/project','bind'),('foreign','other','Foreign','other/project','bind')`,
			).Error; err != nil {
				t.Fatal(err)
			}
			if tc.mutate != "" {
				if err := f.a.DB.Exec(tc.mutate).Error; err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(apiHandler(t, f.a))
			defer server.Close()
			sdk, err := tenantv1.New(
				tenantv1.Config{Endpoint: server.URL, ServiceToken: f.a.Account.Config.ServiceToken},
			)
			if err != nil {
				t.Fatal(err)
			}
			defer sdk.Close()
			token := f.member
			if tc.session == "operator" {
				token = f.operator
			}
			if tc.session == "missing" {
				token = ""
			}
			origin, csrf := tc.origin, tc.csrf
			if origin == "valid" {
				origin = f.a.Account.Config.PublicURL
			}
			if csrf == "valid" {
				csrf = identity.Hash(token + ":csrf")
			}
			out, err := sdk.ResolveScope(
				f.ctx,
				&tenantv1.ResolveScopeRequest{
					Session:   token,
					Method:    tc.method,
					Origin:    origin,
					CSRF:      csrf,
					TenantID:  tc.tenant,
					ProjectID: tc.project,
				},
			)
			if tc.status == 200 {
				if err != nil || out.UserID != "member" || out.TenantID != tc.tenant ||
					out.OwnershipState != "active" ||
					(out.ProjectID == nil) != (tc.project == nil) {
					t.Fatalf("scope: %+v %v", out, err)
				}
				return
			}
			var failure *tenantv1.Error
			if !errors.As(err, &failure) || failure.Status != tc.status || failure.Code != tc.code {
				t.Fatalf("want %d %s, got %v", tc.status, tc.code, err)
			}
		})
	}
}

func scopeString(s string) *string { return &s }

func TestScopeResolveServiceAuthAndUntrustedUser(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		name, body, credential string
		want                   int
	}{
		{name: "no bearer", body: `{}`, credential: "", want: 401},
		{name: "self reported user", body: `{"session":"s","method":"GET","tenant_id":"t","user_id":"operator"}`, credential: "Bearer " + f.a.Account.Config.ServiceToken, want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(
				http.MethodPost,
				"/internal/v1/scope/resolve",
				bytes.NewBufferString(tc.body),
			)
			r.Header.Set("Authorization", tc.credential)
			w := httptest.NewRecorder()
			apiHandler(t, f.a).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	// A database failure is not a missing/deleted scope.
	conn, err := f.a.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = f.a.Account.ResolveScope(
		context.Background(),
		tenantv1.ResolveScopeRequest{Session: f.member, Method: "GET", TenantID: "t"},
	)
	if err == nil {
		t.Fatal("closed database authenticated scope")
	}
	var detail *apperror.Error
	if errors.As(err, &detail) {
		t.Fatalf("database error converted to business error: %v", err)
	}
}

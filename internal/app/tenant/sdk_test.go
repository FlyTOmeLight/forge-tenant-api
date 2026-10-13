package tenant

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

// Exercise the public SDK against HTTPHost and the real PostgreSQL authorization chain.
func TestSDKServiceIntegration(t *testing.T) {
	f := setupProject(t)
	server := httptest.NewServer(apiHandler(t, f.a))
	defer server.Close()
	sdk, err := tenantv1.New(tenantv1.Config{Endpoint: server.URL, ServiceToken: f.a.Config.ServiceToken})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	session, err := sdk.CheckSession(f.ctx, &tenantv1.CheckSessionRequest{Session: f.member, Method: "GET"})
	if err != nil || session.ID != "member" {
		t.Fatalf("session: %v", err)
	}
	_, err = sdk.CheckSession(
		f.ctx,
		&tenantv1.CheckSessionRequest{
			Session: f.member,
			Method:  "POST",
			Origin:  f.a.Config.PublicURL,
			CSRF:    "wrong",
		},
	)
	var failure *tenantv1.Error
	if !errors.As(err, &failure) || failure.Status != 403 || failure.Code != "CSRF_REJECTED" ||
		failure.RequestID == "" {
		t.Fatalf("CSRF error contract: %v", err)
	}
	_, err = sdk.CheckSession(
		f.ctx,
		&tenantv1.CheckSessionRequest{
			Session: f.member,
			Method:  "POST",
			Origin:  f.a.Config.PublicURL,
			CSRF:    Hash(f.member + ":csrf"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	project, err := sdk.ResolveProject(f.ctx, &tenantv1.ResolveProjectRequest{TenantID: "t", ProjectID: "p"})
	if err != nil || project.GitLabID != 77 || project.Path != "task-test/repo" {
		t.Fatalf("project: %v", err)
	}
	workspace, err := sdk.CheckWorkspace(f.ctx, &tenantv1.CheckWorkspaceRequest{UserID: "member", TenantID: "t"})
	if err != nil || !workspace.CanEdit || len(workspace.Projects) != 1 {
		t.Fatalf("workspace: %v", err)
	}
	for _, action := range []tenantv1.AccessAction{tenantv1.AccessRead, tenantv1.AccessWrite, tenantv1.AccessManage} {
		access, err := sdk.CheckAccess(
			f.ctx,
			&tenantv1.CheckAccessRequest{UserID: "member", TenantID: "t", ProjectID: "p", Action: action},
		)
		if err != nil || access.Level != 30 || access.Allowed != (action != tenantv1.AccessManage) ||
			access.AuthorizationVersion != 1 {
			t.Fatalf("access %s: %+v %v", action, access, err)
		}
	}
	noGrant, err := sdk.CheckAccess(
		f.ctx,
		&tenantv1.CheckAccessRequest{
			UserID:    "other",
			TenantID:  "t",
			ProjectID: "p",
			Action:    tenantv1.AccessRead,
		},
	)
	if err != nil || noGrant.Allowed || noGrant.Reason != "NO_GRANT" {
		t.Fatalf("missing grant: %v", err)
	}
	_, err = sdk.CheckWorkspace(f.ctx, &tenantv1.CheckWorkspaceRequest{UserID: "operator", TenantID: "foreign"})
	if !errors.As(err, &failure) || failure.Status != 403 {
		t.Fatalf("cross-tenant access: %v", err)
	}
	code, out := f.call(t, f.operator, "DELETE", "/api/v1/tenants/t/members/mm", nil, 1, "")
	if code != 202 {
		t.Fatal(code, out)
	}
	access, err := sdk.CheckAccess(
		f.ctx,
		&tenantv1.CheckAccessRequest{
			UserID:    "member",
			TenantID:  "t",
			ProjectID: "p",
			Action:    tenantv1.AccessWrite,
		},
	)
	if err != nil || access.Allowed || access.Level != 0 || access.AuthorizationVersion != 2 {
		t.Fatalf("revoked access: %+v %v", access, err)
	}
	events, err := sdk.ListAccessEvents(f.ctx, &tenantv1.ListAccessEventsRequest{})
	if err != nil || len(events.Items) != 1 || events.Items[0].Reason != "remove_member" ||
		events.Items[0].Version != 2 ||
		events.Items[0].CreatedAt.IsZero() {
		t.Fatalf("events: %+v %v", events, err)
	}
	next, err := sdk.ListAccessEvents(f.ctx, &tenantv1.ListAccessEventsRequest{After: events.Items[0].ID})
	if err != nil || next.Items == nil || len(next.Items) != 0 {
		t.Fatalf("cursor page: %+v %v", next, err)
	}
	_, err = sdk.CheckWorkspace(f.ctx, &tenantv1.CheckWorkspaceRequest{UserID: "member", TenantID: "t"})
	if !errors.As(err, &failure) || failure.Code != "WORKSPACE_ACCESS_REQUIRED" {
		t.Fatalf("removed membership: %v", err)
	}
}

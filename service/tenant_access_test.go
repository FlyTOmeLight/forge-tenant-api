package service

import (
	"testing"

	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func setupTenantAccess(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	if err := f.a.DB.WithContext(f.ctx).Exec(
		`INSERT INTO tenants(id,name,slug,status) VALUES('t','Tenant','tenant-access','active');
INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('ma','t','operator','admin','active'),('mm','t','member','member','active')`,
	).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTenantLevelAccess(t *testing.T) {
	f := setupTenantAccess(t)
	check := func(t *testing.T, tenant, user, action string) *tenantv1.CheckAccessResponse {
		t.Helper()
		result, err := f.a.Authorization.CheckAccess(f.ctx, tenantv1.CheckAccessRequest{
			UserID: user, TenantID: tenant, ProjectID: "", Action: tenantv1.AccessAction(action),
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	for _, action := range []string{"read", "write", "manage"} {
		result := check(t, "t", "member", action)
		wantAllowed := action == "read"
		if result.Allowed != wantAllowed || result.Level != 20 || !result.Read ||
			result.Write || result.Manage || result.TenantID != "t" || result.ProjectID != "" ||
			result.AuthorizationVersion != 1 {
			t.Fatalf("member %s: %+v", action, result)
		}
	}
	for _, action := range []string{"read", "write", "manage"} {
		result := check(t, "t", "operator", action)
		if !result.Allowed || result.Level != 40 || !result.Read || !result.Write || !result.Manage ||
			result.TenantID != "t" || result.ProjectID != "" {
			t.Fatalf("admin %s: %+v", action, result)
		}
	}

	if result := check(t, "t", "other", "read"); result.Allowed || result.Reason != "NO_MEMBERSHIP" {
		t.Fatalf("non-member: %+v", result)
	}
	if err := f.a.DB.WithContext(f.ctx).
		Exec(`UPDATE memberships SET status='removed' WHERE id='mm'`).Error; err != nil {
		t.Fatal(err)
	}
	if result := check(t, "t", "member", "read"); result.Allowed || result.Reason != "MEMBER_INACTIVE" {
		t.Fatalf("removed member: %+v", result)
	}
	if err := f.a.DB.WithContext(f.ctx).
		Exec(`UPDATE memberships SET status='active' WHERE id='mm'; UPDATE users SET status='disabled' WHERE id='member'`).
		Error; err != nil {
		t.Fatal(err)
	}
	if result := check(t, "t", "member", "read"); result.Allowed || result.Reason != "MEMBER_INACTIVE" {
		t.Fatalf("disabled user: %+v", result)
	}
	if err := f.a.DB.WithContext(f.ctx).
		Exec(`UPDATE users SET status='active' WHERE id='member'; UPDATE tenants SET status='suspended' WHERE id='t'`).
		Error; err != nil {
		t.Fatal(err)
	}
	if result := check(t, "t", "operator", "manage"); result.Allowed || result.Reason != "TENANT_INACTIVE" {
		t.Fatalf("suspended tenant: %+v", result)
	}
	if err := f.a.DB.WithContext(f.ctx).
		Exec(`UPDATE tenants SET status='active' WHERE id='t'; UPDATE memberships SET status='invited' WHERE id='mm'`).
		Error; err != nil {
		t.Fatal(err)
	}
	if result := check(t, "t", "member", "read"); result.Allowed || result.Reason != "MEMBER_INACTIVE" {
		t.Fatalf("invited member: %+v", result)
	}
	if result := check(t, "missing", "member", "read"); result.Allowed || result.Reason != "TENANT_NOT_FOUND" {
		t.Fatalf("missing tenant: %+v", result)
	}
}

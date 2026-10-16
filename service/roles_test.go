package service

import (
	"testing"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func TestTeamGrantSourcesAndRevocation(t *testing.T) {
	f := setupProject(t)
	if e := f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO teams(id,tenant_id,name) VALUES('dev','t','Development'),('qa','t','Quality')`).
		Error; e != nil {
		t.Fatal(e)
	}
	version := int64(1)
	mutate := func(session, method, path string, body Object, want int) {
		t.Helper()
		code, out := f.call(t, session, method, "/api/v1/tenants/t"+path, body, version, "")
		if code != want {
			t.Fatal(method, path, code, out)
		}
		if code == 202 {
			version = int64(out["version"].(float64))
		}
	}
	check := func(desired, effective int, sources int) {
		t.Helper()
		var d, e int
		if err := f.a.DB.WithContext(f.ctx).
			Raw(`SELECT desired_level,effective_level FROM grants WHERE project_id='p' AND membership_id='mm'`).
			Row().
			Scan(&d, &e); err != nil {
			t.Fatal(err)
		}
		if d != desired || e != effective {
			t.Fatalf("levels = %d/%d, want %d/%d", d, e, desired, effective)
		}
		code, out := f.call(t, f.member, "GET", "/api/v1/tenants/t/permissions/me", nil, 0, "")
		if code != 200 {
			t.Fatal(code, out)
		}
		projects := out["projects"].([]any)
		if len(projects) != 1 || len(projects[0].(map[string]any)["sources"].([]any)) != sources {
			t.Fatal(out)
		}
	}
	mutate(f.member, "PUT", "/teams/dev/members/mm", nil, 404)
	mutate(f.member, "PUT", "/projects/p/team-grants/dev", Object{"role": "admin"}, 404)
	mutate(f.operator, "PUT", "/teams/dev/members/mm", nil, 202)
	mutate(f.operator, "PUT", "/teams/qa/members/mm", nil, 202)
	mutate(f.operator, "PUT", "/projects/p/team-grants/dev", Object{"role": "admin"}, 202)
	check(40, 30, 2) // Existing developer authority is retained while elevation waits.
	if e := f.a.Sync.RecordAccess(f.ctx, "t", version, "p", "mm", 40, true); e != nil {
		t.Fatal(e)
	}
	check(40, 40, 2)
	mutate(f.member, "PUT", "/projects/p/team-grants/qa", Object{"role": "reviewer"}, 202)
	mutate(f.operator, "DELETE", "/teams/dev/members/mm", nil, 202)
	check(30, 30, 2) // Direct developer and the other team's reviewer survive.
	mutate(f.operator, "DELETE", "/projects/p/grants/mm", nil, 202)
	check(20, 20, 1)
	mutate(f.operator, "PUT", "/teams/dev/members/mm", nil, 202)
	check(40, 20, 2) // Rejoining must not trust the old observation of 40.
	mutate(f.operator, "DELETE", "/teams/dev/members/mm", nil, 202)
	mutate(f.operator, "DELETE", "/projects/p/team-grants/qa", nil, 202)
	check(0, 0, 0)
	if e := f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO tenants(id,name,slug) VALUES('foreign','Foreign','foreign');INSERT INTO teams VALUES('foreign-team','foreign','Foreign')`).
		Error; e != nil {
		t.Fatal(e)
	}
	mutate(f.operator, "PUT", "/teams/foreign-team/members/mm", nil, 404)
	mutate(f.operator, "PUT", "/projects/p/team-grants/foreign-team", Object{"role": "admin"}, 404)
	for _, path := range []string{"/api/v1/tenants/t/members/ma/permissions", "/api/v1/tenants/foreign/permissions/me"} {
		code, _ := f.call(t, f.member, "GET", path, nil, 0, "")
		if code != 404 {
			t.Fatal(path, code)
		}
	}
	mutate(f.operator, "PUT", "/projects/p/team-grants/dev", Object{"role": "developer"}, 202)
	mutate(f.operator, "PUT", "/teams/dev/members/mm", nil, 202)
	mutate(f.operator, "DELETE", "/members/mm", nil, 202)
	var n int
	if e := f.a.DB.WithContext(f.ctx).Raw(`SELECT count(*) FROM team_memberships WHERE membership_id='mm'`).Row().
		Scan(&n); e != nil ||
		n != 0 {
		t.Fatal(n, e)
	}
	var effective int
	if e := f.a.DB.WithContext(f.ctx).
		Raw(`SELECT effective_level FROM grants WHERE membership_id='mm' AND project_id='p'`).
		Row().
		Scan(&effective); e != nil ||
		effective != 0 {
		t.Fatal(effective, e)
	}
}

func TestRoleMigrationPreservesAssignments(t *testing.T) {
	f := setupProject(t)
	// Reconstruct the actual pre-team schema, then run the production migrator twice.
	err := f.a.DB.WithContext(f.ctx).
		Exec(`DROP TABLE team_grants,team_memberships,direct_grants; DELETE FROM schema_versions WHERE version=5; ALTER TABLE memberships ADD COLUMN team_id text; INSERT INTO teams VALUES('old','t','Old team');UPDATE memberships SET team_id='old' WHERE id='mm'`).
		Error
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = table.Migrate(f.ctx, f.a.DB); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err = f.a.DB.WithContext(f.ctx).
		Raw(`SELECT count(*) FROM team_memberships WHERE team_id='old' AND membership_id='mm'`).
		Row().
		Scan(&n); err != nil ||
		n != 1 {
		t.Fatal(n, err)
	}
	if err = f.a.DB.WithContext(f.ctx).Raw(`SELECT count(*) FROM direct_grants WHERE level>0`).Row().
		Scan(&n); err != nil ||
		n != 2 {
		t.Fatal(n, err)
	}
	code, out := f.call(t, f.member, "GET", "/api/v1/tenants/t/permissions/me", nil, 0, "")
	if code != 200 || out["projects"].([]any)[0].(map[string]any)["effectiveRole"] != "developer" {
		t.Fatal(code, out)
	}
	if err = f.a.DB.WithContext(f.ctx).
		Exec(`DELETE FROM direct_grants WHERE membership_id='mm'`).
		Error; err != nil {
		t.Fatal(err)
	}
	if err = table.Migrate(f.ctx, f.a.DB); err != nil {
		t.Fatal(err)
	}
	if err = f.a.DB.WithContext(f.ctx).Raw(`SELECT count(*) FROM direct_grants WHERE membership_id='mm'`).Row().
		Scan(&n); err != nil ||
		n != 0 {
		t.Fatal("migration restored revoked source", n, err)
	}
}

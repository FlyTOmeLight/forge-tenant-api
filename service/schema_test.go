package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func TestSchemaMigrationPreservesDefaultsAndData(t *testing.T) {
	f := setup(t)
	db := f.a.DB.WithContext(f.ctx)
	for _, statement := range []string{
		`INSERT INTO users(id,issuer,username,name,email,status) VALUES('invited-a','issuer','invited-a','A','a@example.test','invited'),('invited-b','issuer','invited-b','B','b@example.test','invited')`,
		`INSERT INTO tenants(id,name,slug) VALUES('t','Tenant','tenant')`,
		`INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('m','t','member','member','active')`,
		`INSERT INTO projects(id,tenant_id,name,path,mode) VALUES('p','t','Project','tenant/project','create')`,
		`INSERT INTO grants(tenant_id,project_id,membership_id,desired_level) VALUES('t','p','m',30)`,
		`INSERT INTO operations(id,tenant_id,version,status) VALUES('op','t',1,'pending')`,
		`INSERT INTO audit_events(actor_id,action,target) VALUES('operator','create','t')`,
		`INSERT INTO access_events(tenant_id,version,reason) VALUES('t',1,'create')`,
		`INSERT INTO idempotency(actor_id,request_key,route,body_hash,status_code,response) VALUES('operator','key','route','hash',202,'\x000102')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := table.Migrate(f.ctx, f.a.DB); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct{ name, query string }{
		{name: "nullable invited identities", query: `SELECT count(*)=2 AND bool_and(subject IS NULL AND gitlab_id IS NULL AND NOT operator AND created_at IS NOT NULL) FROM users WHERE id IN ('invited-a','invited-b')`},
		{name: "tenant defaults", query: `SELECT status='active' AND version=1 AND gitlab_id IS NULL AND last_checked_at IS NULL AND created_at IS NOT NULL FROM tenants WHERE id='t'`},
		{name: "grant defaults", query: `SELECT desired_level=30 AND observed_level=0 AND effective_level=0 AND checked_at IS NULL FROM grants WHERE project_id='p' AND membership_id='m'`},
		{name: "operation defaults", query: `SELECT attempts=0 AND error_code='' AND error_message='' AND lease_until IS NULL AND next_attempt_at IS NOT NULL AND created_at IS NOT NULL AND updated_at IS NOT NULL FROM operations WHERE id='op'`},
		{name: "audit defaults", query: `SELECT id>0 AND tenant_id IS NULL AND detail='{}'::jsonb AND operation_id IS NULL AND created_at IS NOT NULL FROM audit_events WHERE target='t'`},
		{name: "access sequence", query: `SELECT id>0 AND version=1 AND created_at IS NOT NULL FROM access_events WHERE tenant_id='t'`},
		{name: "binary idempotency response", query: `SELECT response='\x000102'::bytea AND created_at IS NOT NULL FROM idempotency WHERE request_key='key'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var valid bool
			if err := db.Raw(test.query).Row().Scan(&valid); err != nil || !valid {
				t.Fatalf("defaults or persisted data changed: valid=%v err=%v", valid, err)
			}
		})
	}
}

func TestSchemaMigrationPreservesConstraints(t *testing.T) {
	f := setup(t)
	db := f.a.DB.WithContext(f.ctx)
	for _, statement := range []string{
		`INSERT INTO tenants(id,name,slug) VALUES('a','A','a'),('b','B','b')`,
		`INSERT INTO teams(id,tenant_id,name) VALUES('ta','a','Team'),('tb','b','Team')`,
		`INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('ma','a','member','member','active'),('mb','b','other','member','active')`,
		`INSERT INTO projects(id,tenant_id,name,path,mode) VALUES('pa','a','Project','project','create'),('pb','b','Project','project','bind')`,
		`INSERT INTO grants(tenant_id,project_id,membership_id,desired_level) VALUES('a','pa','ma',30)`,
		`INSERT INTO direct_grants(tenant_id,project_id,membership_id,level) VALUES('a','pa','ma',30)`,
		`INSERT INTO team_grants(tenant_id,project_id,team_id,level) VALUES('a','pa','ta',30)`,
		`INSERT INTO team_memberships(tenant_id,team_id,membership_id) VALUES('a','ta','ma')`,
		`INSERT INTO operations(id,tenant_id,version,status) VALUES('op','a',1,'pending')`,
		`INSERT INTO invitations(id,token_hash,membership_id,expires_at) VALUES('invite','token','ma',now())`,
		`UPDATE users SET gitlab_id=101 WHERE id='member'`,
		`UPDATE tenants SET gitlab_id=102 WHERE id='a'`,
		`UPDATE projects SET gitlab_id=103 WHERE id='pa'`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct{ name, statement, state string }{
		{name: "user primary key", statement: `INSERT INTO users SELECT * FROM users WHERE id='member'`, state: "23505"},
		{name: "user identity", statement: `UPDATE users SET subject='member' WHERE id='other'`, state: "23505"},
		{name: "user username", statement: `UPDATE users SET username='member' WHERE id='other'`, state: "23505"},
		{name: "user email", statement: `UPDATE users SET email=(SELECT email FROM users WHERE id='member') WHERE id='other'`, state: "23505"},
		{name: "user gitlab identity", statement: `UPDATE users SET gitlab_id=101 WHERE id='other'`, state: "23505"},
		{name: "tenant slug", statement: `UPDATE tenants SET slug='a' WHERE id='b'`, state: "23505"},
		{name: "tenant gitlab identity", statement: `UPDATE tenants SET gitlab_id=102 WHERE id='b'`, state: "23505"},
		{name: "team name in tenant", statement: `INSERT INTO teams(id,tenant_id,name) VALUES('duplicate','a','Team')`, state: "23505"},
		{name: "membership identity in tenant", statement: `INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('duplicate','a','member','member','active')`, state: "23505"},
		{name: "project path in tenant", statement: `INSERT INTO projects(id,tenant_id,name,path,mode) VALUES('duplicate','a','Project','project','create')`, state: "23505"},
		{name: "project gitlab identity", statement: `UPDATE projects SET gitlab_id=103 WHERE id='pb'`, state: "23505"},
		{name: "operation version in tenant", statement: `INSERT INTO operations(id,tenant_id,version,status) VALUES('duplicate','a',1,'pending')`, state: "23505"},
		{name: "invitation token", statement: `INSERT INTO invitations(id,token_hash,membership_id,expires_at) VALUES('duplicate','token','ma',now())`, state: "23505"},
		{name: "grant primary key", statement: `INSERT INTO grants SELECT * FROM grants WHERE project_id='pa'`, state: "23505"},
		{name: "direct grant primary key", statement: `INSERT INTO direct_grants SELECT * FROM direct_grants WHERE project_id='pa'`, state: "23505"},
		{name: "team grant primary key", statement: `INSERT INTO team_grants SELECT * FROM team_grants WHERE project_id='pa'`, state: "23505"},
		{name: "team membership primary key", statement: `INSERT INTO team_memberships SELECT * FROM team_memberships WHERE team_id='ta'`, state: "23505"},
		{name: "team tenant", statement: `UPDATE teams SET tenant_id='missing' WHERE id='tb'`, state: "23503"},
		{name: "membership user", statement: `UPDATE memberships SET user_id='missing' WHERE id='mb'`, state: "23503"},
		{name: "membership tenant", statement: `UPDATE memberships SET tenant_id='missing' WHERE id='mb'`, state: "23503"},
		{name: "project tenant", statement: `UPDATE projects SET tenant_id='missing' WHERE id='pb'`, state: "23503"},
		{name: "grant foreign project", statement: `UPDATE grants SET project_id='pb' WHERE project_id='pa'`, state: "23503"},
		{name: "grant foreign member", statement: `UPDATE grants SET membership_id='mb' WHERE project_id='pa'`, state: "23503"},
		{name: "direct grant foreign project", statement: `UPDATE direct_grants SET project_id='pb' WHERE project_id='pa'`, state: "23503"},
		{name: "direct grant foreign member", statement: `UPDATE direct_grants SET membership_id='mb' WHERE project_id='pa'`, state: "23503"},
		{name: "team grant foreign project", statement: `UPDATE team_grants SET project_id='pb' WHERE project_id='pa'`, state: "23503"},
		{name: "team grant foreign team", statement: `UPDATE team_grants SET team_id='tb' WHERE project_id='pa'`, state: "23503"},
		{name: "team membership foreign team", statement: `UPDATE team_memberships SET team_id='tb' WHERE team_id='ta'`, state: "23503"},
		{name: "team membership foreign member", statement: `UPDATE team_memberships SET membership_id='mb' WHERE team_id='ta'`, state: "23503"},
		{name: "operation tenant", statement: `UPDATE operations SET tenant_id='missing' WHERE id='op'`, state: "23503"},
		{name: "invitation member", statement: `UPDATE invitations SET membership_id='missing' WHERE id='invite'`, state: "23503"},
		{name: "session user", statement: `UPDATE sessions SET user_id='missing' WHERE user_id='member'`, state: "23503"},
		{name: "audit tenant", statement: `INSERT INTO audit_events(tenant_id,actor_id,action,target) VALUES('missing','operator','create','x')`, state: "23503"},
		{name: "access tenant", statement: `INSERT INTO access_events(tenant_id,version,reason) VALUES('missing',1,'create')`, state: "23503"},
		{name: "user status", statement: `UPDATE users SET status='invalid' WHERE id='member'`, state: "23514"},
		{name: "tenant status", statement: `UPDATE tenants SET status='invalid' WHERE id='a'`, state: "23514"},
		{name: "membership role", statement: `UPDATE memberships SET role='invalid' WHERE id='ma'`, state: "23514"},
		{name: "membership status", statement: `UPDATE memberships SET status='invalid' WHERE id='ma'`, state: "23514"},
		{name: "project mode", statement: `UPDATE projects SET mode='invalid' WHERE id='pa'`, state: "23514"},
		{name: "operation status", statement: `UPDATE operations SET status='invalid' WHERE id='op'`, state: "23514"},
		{name: "desired grant level", statement: `UPDATE grants SET desired_level=10 WHERE project_id='pa'`, state: "23514"},
		{name: "effective grant level", statement: `UPDATE grants SET effective_level=10 WHERE project_id='pa'`, state: "23514"},
		{name: "direct grant level", statement: `UPDATE direct_grants SET level=0 WHERE project_id='pa'`, state: "23514"},
		{name: "team grant level", statement: `UPDATE team_grants SET level=0 WHERE project_id='pa'`, state: "23514"},
		{name: "required tenant name", statement: `UPDATE tenants SET name=NULL WHERE id='a'`, state: "23502"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			err := tx.Exec(test.statement).Error
			var pg interface{ SQLState() string }
			if !errors.As(err, &pg) || pg.SQLState() != test.state {
				t.Fatalf("expected PostgreSQL %s, got %v", test.state, err)
			}
		})
	}
}

func TestSchemaMigrationPreservesColumnTypesAndIndexes(t *testing.T) {
	f := setup(t)
	db := f.a.DB.WithContext(f.ctx)
	for _, test := range []struct{ table, column, datatype, nullable string }{
		{table: "schema_versions", column: "version", datatype: "integer", nullable: "NO"},
		{table: "schema_versions", column: "applied_at", datatype: "timestamp with time zone", nullable: "NO"},
		{table: "users", column: "subject", datatype: "text", nullable: "YES"},
		{table: "users", column: "gitlab_id", datatype: "bigint", nullable: "YES"},
		{table: "users", column: "operator", datatype: "boolean", nullable: "NO"},
		{table: "users", column: "created_at", datatype: "timestamp with time zone", nullable: "NO"},
		{table: "tenants", column: "version", datatype: "bigint", nullable: "NO"},
		{table: "tenants", column: "gitlab_id", datatype: "bigint", nullable: "YES"},
		{table: "tenants", column: "last_checked_at", datatype: "timestamp with time zone", nullable: "YES"},
		{table: "projects", column: "gitlab_id", datatype: "bigint", nullable: "YES"},
		{table: "grants", column: "desired_level", datatype: "integer", nullable: "NO"},
		{table: "grants", column: "observed_level", datatype: "integer", nullable: "NO"},
		{table: "grants", column: "effective_level", datatype: "integer", nullable: "NO"},
		{table: "grants", column: "checked_at", datatype: "timestamp with time zone", nullable: "YES"},
		{table: "direct_grants", column: "level", datatype: "integer", nullable: "NO"},
		{table: "team_grants", column: "level", datatype: "integer", nullable: "NO"},
		{table: "operations", column: "version", datatype: "bigint", nullable: "NO"},
		{table: "operations", column: "attempts", datatype: "integer", nullable: "NO"},
		{table: "operations", column: "next_attempt_at", datatype: "timestamp with time zone", nullable: "NO"},
		{table: "operations", column: "lease_until", datatype: "timestamp with time zone", nullable: "YES"},
		{table: "audit_events", column: "id", datatype: "bigint", nullable: "NO"},
		{table: "audit_events", column: "detail", datatype: "jsonb", nullable: "NO"},
		{table: "audit_events", column: "tenant_id", datatype: "text", nullable: "YES"},
		{table: "audit_events", column: "operation_id", datatype: "text", nullable: "YES"},
		{table: "invitations", column: "expires_at", datatype: "timestamp with time zone", nullable: "NO"},
		{table: "invitations", column: "consumed_at", datatype: "timestamp with time zone", nullable: "YES"},
		{table: "sessions", column: "expires_at", datatype: "timestamp with time zone", nullable: "NO"},
		{table: "login_flows", column: "expires_at", datatype: "timestamp with time zone", nullable: "NO"},
		{table: "idempotency", column: "status_code", datatype: "integer", nullable: "NO"},
		{table: "idempotency", column: "response", datatype: "bytea", nullable: "NO"},
		{table: "access_events", column: "id", datatype: "bigint", nullable: "NO"},
		{table: "access_events", column: "version", datatype: "bigint", nullable: "NO"},
	} {
		t.Run(test.table+"."+test.column, func(t *testing.T) {
			var datatype, nullable string
			err := db.Raw(`SELECT data_type,is_nullable FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=? AND column_name=?`, test.table, test.column).
				Row().
				Scan(&datatype, &nullable)
			if err != nil || datatype != test.datatype || nullable != test.nullable {
				t.Fatalf(
					"type=%q nullable=%q err=%v; want %q/%q",
					datatype,
					nullable,
					err,
					test.datatype,
					test.nullable,
				)
			}
		})
	}
	for _, test := range []struct{ name, table, columns string }{
		{name: "operations_ready", table: "operations", columns: "next_attempt_at"},
		{name: "team_memberships_member", table: "team_memberships", columns: "tenant_id, membership_id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var definition, predicate string
			err := db.Raw(`SELECT pg_get_indexdef(i.indexrelid),coalesce(pg_get_expr(i.indpred,i.indrelid),'') FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname=?`, test.name).
				Row().
				Scan(&definition, &predicate)
			if err != nil ||
				!strings.Contains(
					definition,
					" ON "+currentTestSchema(t, f)+"."+test.table+" USING btree ("+test.columns+")",
				) {
				t.Fatalf("index=%q predicate=%q err=%v", definition, predicate, err)
			}
			if test.name == "team_memberships_member" {
				if predicate != "" {
					t.Fatalf("member index must cover all memberships: %q", predicate)
				}
				return
			}
			if predicate == "" {
				t.Fatal("operations index lost its status predicate")
			}
			var valid bool
			err = db.Raw(`SELECT bool_and((` + predicate + `) = (status IN ('pending','retrying','running'))) FROM (VALUES ('pending'),('retrying'),('running'),('failed'),('conflict'),('succeeded'),('superseded'),('invalid')) AS cases(status)`).
				Row().
				Scan(&valid)
			if err != nil || !valid {
				t.Fatalf("ready index selects incorrect operation statuses: %q, err=%v", predicate, err)
			}
		})
	}
}

func currentTestSchema(t *testing.T, f *fixture) string {
	t.Helper()
	var schema string
	if err := f.a.DB.WithContext(f.ctx).Raw(`SELECT current_schema()`).Row().Scan(&schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestSchemaMigrationRollsBackFailedLegacyUpgrade(t *testing.T) {
	f := setupProject(t)
	if err := f.a.DB.Exec(
		`DROP TABLE team_grants,team_memberships,direct_grants; DELETE FROM schema_versions WHERE version=5; ALTER TABLE memberships ADD COLUMN team_id text; UPDATE memberships SET team_id='missing-team' WHERE id='mm'`,
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := table.Migrate(f.ctx, f.a.DB); err == nil {
		t.Fatal("invalid legacy team unexpectedly migrated")
	}
	if !f.a.DB.Migrator().HasColumn(&table.Membership{}, "team_id") {
		t.Fatal("failed upgrade dropped the original team field")
	}
	for _, model := range []any{&table.TeamMembership{}, &table.DirectGrant{}, &table.TeamGrant{}} {
		if f.a.DB.Migrator().HasTable(model) {
			t.Fatal("failed upgrade left a partially migrated table")
		}
	}
	var versions int64
	if err := f.a.DB.Model(&table.SchemaVersion{}).
		Where("version = ?", 5).
		Count(&versions).
		Error; err != nil ||
		versions != 0 {
		t.Fatalf("failed upgrade recorded v5: count=%d err=%v", versions, err)
	}
	var team string
	if err := f.a.DB.Table("memberships").
		Select("team_id").
		Where("id = ?", "mm").
		Scan(&team).
		Error; err != nil ||
		team != "missing-team" {
		t.Fatalf("legacy data changed: team=%q err=%v", team, err)
	}
}

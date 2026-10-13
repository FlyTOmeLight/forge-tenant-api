package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func ResolveProject(ctx context.Context, db RowReader, tenantID string, projectID string) (Project, error) {
	var record Project
	err := db.QueryRow(
		ctx,
		`SELECT id,path,gitlab_id FROM projects WHERE tenant_id=$1 AND id=$2 AND gitlab_id IS NOT NULL`,
		tenantID,
		projectID,
	).Scan(&record.ID, &record.Path, &record.GitLabID)
	return record, err
}

func ListWorkspaceProjects(ctx context.Context, db Reader, tenantID string, userID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT p.id,p.name,least(g.desired_level,g.observed_level,g.effective_level) AS level FROM projects p JOIN grants g ON g.project_id=p.id AND g.tenant_id=p.tenant_id JOIN memberships m ON m.id=g.membership_id WHERE p.tenant_id=$1 AND m.user_id=$2 AND m.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20 ORDER BY p.name,p.id`,
		tenantID,
		userID,
	)
}

func RemoveMemberDirectGrants(
	ctx context.Context,
	db Writer,
	tenantID string,
	membershipID string,
) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `DELETE FROM direct_grants WHERE tenant_id=$1 AND membership_id=$2`, tenantID, membershipID)
}

func ProjectMemberState(ctx context.Context, db RowReader, tenantID, projectID, userID string) (ProjectMember, error) {
	var record ProjectMember
	err := db.QueryRow(ctx, `SELECT m.role,t.status,m.status,u.status,coalesce(g.desired_level,0),coalesce(g.observed_level,0),coalesce(g.effective_level,0) FROM projects p JOIN tenants t ON t.id=p.tenant_id JOIN memberships m ON m.tenant_id=t.id JOIN users u ON u.id=m.user_id LEFT JOIN grants g ON g.project_id=p.id AND g.membership_id=m.id WHERE p.tenant_id=$1 AND p.id=$2 AND m.user_id=$3`, tenantID, projectID, userID).
		Scan(&record.Role, &record.TenantStatus, &record.MemberStatus, &record.UserStatus, &record.Desired, &record.Observed, &record.Effective)
	return record, err
}

func ProjectAuthorization(
	ctx context.Context,
	db RowReader,
	tenantID string,
	projectID string,
	userID string,
) (Authorization, error) {
	var record Authorization
	err := db.QueryRow(
		ctx,
		`SELECT g.desired_level,g.observed_level,g.effective_level,t.version,t.status,m.status,u.status FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=g.tenant_id WHERE g.tenant_id=$1 AND g.project_id=$2 AND m.user_id=$3`,
		tenantID,
		projectID,
		userID,
	).Scan(&record.Desired, &record.Observed, &record.Effective, &record.Version, &record.TenantStatus, &record.MemberStatus, &record.UserStatus)
	return record, err
}

func InsertProject(
	ctx context.Context,
	db Writer,
	projectID string,
	tenantID string,
	name string,
	path string,
	mode string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO projects(id,tenant_id,name,path,mode) VALUES($1,$2,$3,$4,$5)`,
		projectID,
		tenantID,
		name,
		path,
		mode,
	)
}

func GrantTargetExists(
	ctx context.Context,
	db RowReader,
	tenantID string,
	membershipID string,
	projectID string,
	level int,
) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id=m.user_id JOIN projects p ON p.tenant_id=m.tenant_id WHERE m.tenant_id=$1 AND m.id=$2 AND p.id=$3 AND ($4=0 OR (m.status='active' AND u.status='active')))`,
		tenantID,
		membershipID,
		projectID,
		level,
	)
}

func DeleteDirectGrant(
	ctx context.Context,
	db Writer,
	tenantID string,
	projectID string,
	membershipID string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`DELETE FROM direct_grants WHERE tenant_id=$1 AND project_id=$2 AND membership_id=$3`,
		tenantID,
		projectID,
		membershipID,
	)
}

func UpsertDirectGrant(
	ctx context.Context,
	db Writer,
	tenantID string,
	projectID string,
	membershipID string,
	level int,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO direct_grants VALUES($1,$2,$3,$4) ON CONFLICT(project_id,membership_id) DO UPDATE SET level=EXCLUDED.level`,
		tenantID,
		projectID,
		membershipID,
		level,
	)
}

func ListProjectPeople(ctx context.Context, db Reader, tenantID string, projectID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT u.id,u.name,(least(g.desired_level,g.observed_level,g.effective_level)>=20) AS "canReview",(least(g.desired_level,g.observed_level,g.effective_level)>=30) AS "canOwn" FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE g.tenant_id=$1 AND g.project_id=$2 AND t.status='active' AND m.status='active' AND u.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20 ORDER BY u.name`,
		tenantID,
		projectID,
	)
}

func ListMemberProjects(
	ctx context.Context,
	db Reader,
	tenantID string,
	membershipID string,
	includeAll bool,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT p.id,p.name,coalesce(g.desired_level,0) AS desired_level,coalesce(g.observed_level,0) AS observed_level,coalesce(g.effective_level,0) AS effective_level FROM projects p LEFT JOIN grants g ON g.project_id=p.id AND g.membership_id=$2 WHERE p.tenant_id=$1 AND ($3 OR g.membership_id IS NOT NULL) ORDER BY p.name`,
		tenantID,
		membershipID,
		includeAll,
	)
}

func ListSyncProjects(ctx context.Context, db Reader, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT id,name,path,mode,coalesce(gitlab_id,0) AS gitlab_id FROM projects WHERE tenant_id=$1 ORDER BY id`,
		tenantID,
	)
}

func SetProjectGitLabID(ctx context.Context, db Writer, projectID string, gitlabID int64) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE projects SET gitlab_id=$2 WHERE id=$1`, projectID, gitlabID)
}

func ListSyncGrants(ctx context.Context, db Reader, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT g.project_id,g.membership_id,g.desired_level,m.status AS member_status,u.status AS user_status,coalesce(u.gitlab_id,0) AS user_remote,p.gitlab_id AS project_remote FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN projects p ON p.id=g.project_id WHERE g.tenant_id=$1 ORDER BY g.project_id,g.membership_id`,
		tenantID,
	)
}

func RecordObservedAccess(
	ctx context.Context,
	db Writer,
	projectID string,
	membershipID string,
	actual int,
	confirmed bool,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`UPDATE grants SET observed_level=$3,checked_at=now(),effective_level=CASE WHEN $4 THEN LEAST(desired_level,$3) ELSE LEAST(effective_level,desired_level,$3) END WHERE project_id=$1 AND membership_id=$2`,
		projectID,
		membershipID,
		actual,
		confirmed,
	)
}

func ClearTenantEffectiveAccess(ctx context.Context, db Writer, tenantID string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE grants SET effective_level=0 WHERE tenant_id=$1`, tenantID)
}

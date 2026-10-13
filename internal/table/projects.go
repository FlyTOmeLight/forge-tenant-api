package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

func ResolveProject(ctx context.Context, db *gorm.DB, tenantID string, projectID string) (Project, error) {
	var record Project
	err := db.WithContext(ctx).
		Raw(`SELECT id,path,gitlab_id FROM projects WHERE tenant_id=? AND id=? AND gitlab_id IS NOT NULL`, tenantID, projectID).
		Row().
		Scan(&record.ID, &record.Path, &record.GitLabID)
	return record, err
}

func ListWorkspaceProjects(ctx context.Context, db *gorm.DB, tenantID string, userID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT p.id,p.name,least(g.desired_level,g.observed_level,g.effective_level) AS level FROM projects p JOIN grants g ON g.project_id=p.id AND g.tenant_id=p.tenant_id JOIN memberships m ON m.id=g.membership_id WHERE p.tenant_id=? AND m.user_id=? AND m.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20 ORDER BY p.name,p.id`,
		tenantID,
		userID,
	)
}

func RemoveMemberDirectGrants(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	membershipID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`DELETE FROM direct_grants WHERE tenant_id=? AND membership_id=?`, tenantID, membershipID)
	return result.RowsAffected, result.Error
}

func ProjectMemberState(ctx context.Context, db *gorm.DB, tenantID, projectID, userID string) (ProjectMember, error) {
	var record ProjectMember
	err := db.WithContext(ctx).
		Raw(`SELECT m.role,t.status,m.status,u.status,coalesce(g.desired_level,0),coalesce(g.observed_level,0),coalesce(g.effective_level,0) FROM projects p JOIN tenants t ON t.id=p.tenant_id JOIN memberships m ON m.tenant_id=t.id JOIN users u ON u.id=m.user_id LEFT JOIN grants g ON g.project_id=p.id AND g.membership_id=m.id WHERE p.tenant_id=? AND p.id=? AND m.user_id=?`, tenantID, projectID, userID).
		Row().
		Scan(&record.Role, &record.TenantStatus, &record.MemberStatus, &record.UserStatus, &record.Desired, &record.Observed, &record.Effective)
	return record, err
}

func ProjectAuthorization(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	projectID string,
	userID string,
) (Authorization, error) {
	var record Authorization
	err := db.WithContext(ctx).
		Raw(`SELECT g.desired_level,g.observed_level,g.effective_level,t.version,t.status,m.status,u.status FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=g.tenant_id WHERE g.tenant_id=? AND g.project_id=? AND m.user_id=?`, tenantID, projectID, userID).
		Row().
		Scan(&record.Desired, &record.Observed, &record.Effective, &record.Version, &record.TenantStatus, &record.MemberStatus, &record.UserStatus)
	return record, err
}

func InsertProject(
	ctx context.Context,
	db *gorm.DB,
	projectID string,
	tenantID string,
	name string,
	path string,
	mode string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO projects(id,tenant_id,name,path,mode) VALUES(?,?,?,?,?)`, projectID, tenantID, name, path, mode)
	return result.RowsAffected, result.Error
}

func GrantTargetExists(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	membershipID string,
	projectID string,
	level int,
) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id=m.user_id JOIN projects p ON p.tenant_id=m.tenant_id WHERE m.tenant_id=? AND m.id=? AND p.id=? AND (?=0 OR (m.status='active' AND u.status='active')))`, tenantID, membershipID, projectID, level).
		Row()
}

func DeleteDirectGrant(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	projectID string,
	membershipID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`DELETE FROM direct_grants WHERE tenant_id=? AND project_id=? AND membership_id=?`, tenantID, projectID, membershipID)
	return result.RowsAffected, result.Error
}

func UpsertDirectGrant(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	projectID string,
	membershipID string,
	level int,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO direct_grants VALUES(?,?,?,?) ON CONFLICT(project_id,membership_id) DO UPDATE SET level=EXCLUDED.level`, tenantID, projectID, membershipID, level)
	return result.RowsAffected, result.Error
}

func ListProjectPeople(ctx context.Context, db *gorm.DB, tenantID string, projectID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT u.id,u.name,(least(g.desired_level,g.observed_level,g.effective_level)>=20) AS "canReview",(least(g.desired_level,g.observed_level,g.effective_level)>=30) AS "canOwn" FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE g.tenant_id=? AND g.project_id=? AND t.status='active' AND m.status='active' AND u.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20 ORDER BY u.name`,
		tenantID,
		projectID,
	)
}

func ListMemberProjects(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	membershipID string,
	includeAll bool,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT p.id,p.name,coalesce(g.desired_level,0) AS desired_level,coalesce(g.observed_level,0) AS observed_level,coalesce(g.effective_level,0) AS effective_level FROM projects p LEFT JOIN grants g ON g.project_id=p.id AND g.membership_id=? WHERE p.tenant_id=? AND (? OR g.membership_id IS NOT NULL) ORDER BY p.name`,
		membershipID,
		tenantID,
		includeAll,
	)
}

func ListSyncProjects(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT id,name,path,mode,coalesce(gitlab_id,0) AS gitlab_id FROM projects WHERE tenant_id=? ORDER BY id`,
		tenantID,
	)
}

func SetProjectGitLabID(ctx context.Context, db *gorm.DB, projectID string, gitlabID int64) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE projects SET gitlab_id=? WHERE id=?`, gitlabID, projectID)
	return result.RowsAffected, result.Error
}

func ListSyncGrants(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT g.project_id,g.membership_id,g.desired_level,m.status AS member_status,u.status AS user_status,coalesce(u.gitlab_id,0) AS user_remote,p.gitlab_id AS project_remote FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN projects p ON p.id=g.project_id WHERE g.tenant_id=? ORDER BY g.project_id,g.membership_id`,
		tenantID,
	)
}

func RecordObservedAccess(
	ctx context.Context,
	db *gorm.DB,
	projectID string,
	membershipID string,
	actual int,
	confirmed bool,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE grants SET observed_level=?,checked_at=now(),effective_level=CASE WHEN ? THEN LEAST(desired_level,?) ELSE LEAST(effective_level,desired_level,?) END WHERE project_id=? AND membership_id=?`, actual, confirmed, actual, actual, projectID, membershipID)
	return result.RowsAffected, result.Error
}

func ClearTenantEffectiveAccess(ctx context.Context, db *gorm.DB, tenantID string) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE grants SET effective_level=0 WHERE tenant_id=?`, tenantID)
	return result.RowsAffected, result.Error
}

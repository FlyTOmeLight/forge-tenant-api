package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

func RemoveMemberTeams(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	membershipID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`DELETE FROM team_memberships WHERE tenant_id=? AND membership_id=?`, tenantID, membershipID)
	return result.RowsAffected, result.Error
}

func InsertTeam(ctx context.Context, db *gorm.DB, id string, tenantID string, name string) (int64, error) {
	result := db.WithContext(ctx).Exec(`INSERT INTO teams(id,tenant_id,name) VALUES(?,?,?)`, id, tenantID, name)
	return result.RowsAffected, result.Error
}

func ListProjectTeamGrants(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	projectID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.id,t.name,coalesce(g.level,0) AS level,(SELECT count(*) FROM team_memberships tm JOIN memberships m ON m.id=tm.membership_id WHERE tm.team_id=t.id AND m.status='active') AS member_count FROM teams t LEFT JOIN team_grants g ON g.team_id=t.id AND g.project_id=? WHERE t.tenant_id=? ORDER BY t.name`,
		projectID,
		tenantID,
	)
}

func ListGrantSources(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	membershipID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT d.project_id,'direct' AS kind,d.membership_id AS id,'直接分配' AS name,d.level FROM direct_grants d WHERE d.tenant_id=? AND d.membership_id=? UNION ALL SELECT g.project_id,'team',t.id,t.name,g.level FROM team_grants g JOIN teams t ON t.id=g.team_id JOIN team_memberships tm ON tm.team_id=t.id WHERE g.tenant_id=? AND tm.membership_id=? ORDER BY project_id,kind,name`,
		tenantID,
		membershipID,
		tenantID,
		membershipID,
	)
}

func ListMemberTeams(ctx context.Context, db *gorm.DB, tenantID string, membershipID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.id,t.name FROM teams t JOIN team_memberships tm ON tm.team_id=t.id WHERE tm.tenant_id=? AND tm.membership_id=? ORDER BY t.name`,
		tenantID,
		membershipID,
	)
}

func TeamMemberTargetExists(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	teamID string,
	membershipID string,
	allowInactive bool,
) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM teams t JOIN memberships m ON m.tenant_id=t.tenant_id JOIN users u ON u.id=m.user_id WHERE t.tenant_id=? AND t.id=? AND m.id=? AND (? OR (m.status='active' AND u.status='active')))`, tenantID, teamID, membershipID, allowInactive).
		Row()
}

func DeleteTeamMember(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	teamID string,
	membershipID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`DELETE FROM team_memberships WHERE tenant_id=? AND team_id=? AND membership_id=?`, tenantID, teamID, membershipID)
	return result.RowsAffected, result.Error
}

func InsertTeamMember(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	teamID string,
	membershipID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO team_memberships VALUES(?,?,?) ON CONFLICT DO NOTHING`, tenantID, teamID, membershipID)
	return result.RowsAffected, result.Error
}

func ProjectTeamExists(ctx context.Context, db *gorm.DB, tenantID string, projectID string, teamID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM projects p JOIN teams t ON t.tenant_id=p.tenant_id WHERE p.tenant_id=? AND p.id=? AND t.id=?)`, tenantID, projectID, teamID).
		Row()
}

func DeleteTeamGrant(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	projectID string,
	teamID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`DELETE FROM team_grants WHERE tenant_id=? AND project_id=? AND team_id=?`, tenantID, projectID, teamID)
	return result.RowsAffected, result.Error
}

func UpsertTeamGrant(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	projectID string,
	teamID string,
	level int,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO team_grants VALUES(?,?,?,?) ON CONFLICT(project_id,team_id) DO UPDATE SET level=EXCLUDED.level`, tenantID, projectID, teamID, level)
	return result.RowsAffected, result.Error
}

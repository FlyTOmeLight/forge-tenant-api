package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func RemoveMemberTeams(
	ctx context.Context,
	db Writer,
	tenantID string,
	membershipID string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`DELETE FROM team_memberships WHERE tenant_id=$1 AND membership_id=$2`,
		tenantID,
		membershipID,
	)
}

func InsertTeam(ctx context.Context, db Writer, id string, tenantID string, name string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `INSERT INTO teams(id,tenant_id,name) VALUES($1,$2,$3)`, id, tenantID, name)
}

func ListProjectTeamGrants(
	ctx context.Context,
	db Reader,
	tenantID string,
	projectID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.id,t.name,coalesce(g.level,0) AS level,(SELECT count(*) FROM team_memberships tm JOIN memberships m ON m.id=tm.membership_id WHERE tm.team_id=t.id AND m.status='active') AS member_count FROM teams t LEFT JOIN team_grants g ON g.team_id=t.id AND g.project_id=$2 WHERE t.tenant_id=$1 ORDER BY t.name`,
		tenantID,
		projectID,
	)
}

func ListGrantSources(ctx context.Context, db Reader, tenantID string, membershipID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT d.project_id,'direct' AS kind,d.membership_id AS id,'直接分配' AS name,d.level FROM direct_grants d WHERE d.tenant_id=$1 AND d.membership_id=$2 UNION ALL SELECT g.project_id,'team',t.id,t.name,g.level FROM team_grants g JOIN teams t ON t.id=g.team_id JOIN team_memberships tm ON tm.team_id=t.id WHERE g.tenant_id=$1 AND tm.membership_id=$2 ORDER BY project_id,kind,name`,
		tenantID,
		membershipID,
	)
}

func ListMemberTeams(ctx context.Context, db Reader, tenantID string, membershipID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.id,t.name FROM teams t JOIN team_memberships tm ON tm.team_id=t.id WHERE tm.tenant_id=$1 AND tm.membership_id=$2 ORDER BY t.name`,
		tenantID,
		membershipID,
	)
}

func TeamMemberTargetExists(
	ctx context.Context,
	db RowReader,
	tenantID string,
	teamID string,
	membershipID string,
	allowInactive bool,
) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM teams t JOIN memberships m ON m.tenant_id=t.tenant_id JOIN users u ON u.id=m.user_id WHERE t.tenant_id=$1 AND t.id=$2 AND m.id=$3 AND ($4 OR (m.status='active' AND u.status='active')))`,
		tenantID,
		teamID,
		membershipID,
		allowInactive,
	)
}

func DeleteTeamMember(
	ctx context.Context,
	db Writer,
	tenantID string,
	teamID string,
	membershipID string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`DELETE FROM team_memberships WHERE tenant_id=$1 AND team_id=$2 AND membership_id=$3`,
		tenantID,
		teamID,
		membershipID,
	)
}

func InsertTeamMember(
	ctx context.Context,
	db Writer,
	tenantID string,
	teamID string,
	membershipID string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO team_memberships VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		tenantID,
		teamID,
		membershipID,
	)
}

func ProjectTeamExists(ctx context.Context, db RowReader, tenantID string, projectID string, teamID string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM projects p JOIN teams t ON t.tenant_id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$2 AND t.id=$3)`,
		tenantID,
		projectID,
		teamID,
	)
}

func DeleteTeamGrant(
	ctx context.Context,
	db Writer,
	tenantID string,
	projectID string,
	teamID string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`DELETE FROM team_grants WHERE tenant_id=$1 AND project_id=$2 AND team_id=$3`,
		tenantID,
		projectID,
		teamID,
	)
}

func UpsertTeamGrant(
	ctx context.Context,
	db Writer,
	tenantID string,
	projectID string,
	teamID string,
	level int,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO team_grants VALUES($1,$2,$3,$4) ON CONFLICT(project_id,team_id) DO UPDATE SET level=EXCLUDED.level`,
		tenantID,
		projectID,
		teamID,
		level,
	)
}

package table

import (
	"context"
)

func ListSnapshotGrants(
	ctx context.Context,
	db Reader,
	tenantID string,
	admin bool,
	userID string,
) ([]map[string]any, error) {
	return collect(ctx, db, `SELECT g.*,coalesce(d.level,0) AS direct_level,
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',t.id,'name',t.name,'level',tg.level)) FROM team_grants tg JOIN teams t ON t.id=tg.team_id JOIN team_memberships tm ON tm.team_id=t.id WHERE tg.project_id=g.project_id AND tm.membership_id=g.membership_id),'[]'::jsonb) AS team_sources,u.name,u.email,m.status AS member_status,u.status AS user_status FROM grants g LEFT JOIN direct_grants d ON d.project_id=g.project_id AND d.membership_id=g.membership_id JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id WHERE g.tenant_id=$1 AND ($2 OR m.user_id=$3 OR EXISTS(SELECT 1 FROM grants own JOIN memberships om ON om.id=own.membership_id WHERE own.project_id=g.project_id AND om.user_id=$3 AND om.status='active' AND least(own.desired_level,own.observed_level,own.effective_level)>=40)) ORDER BY g.project_id,u.name LIMIT 1000`, tenantID, admin, userID)
}

func ListSnapshotTeams(ctx context.Context, db Reader, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.*,(SELECT count(*) FROM team_memberships tm JOIN memberships m ON m.id=tm.membership_id WHERE tm.team_id=t.id AND m.status='active') AS member_count FROM teams t WHERE tenant_id=$1 ORDER BY name LIMIT 500`,
		tenantID,
	)
}

func ListSnapshotOperations(ctx context.Context, db Reader, tenantID string) ([]map[string]any, error) {
	return collect(ctx, db, `SELECT * FROM operations WHERE tenant_id=$1 ORDER BY version DESC LIMIT 100`, tenantID)
}

func ListSnapshotAudit(ctx context.Context, db Reader, tenantID string) ([]map[string]any, error) {
	return collect(ctx, db, `SELECT * FROM audit_events WHERE tenant_id=$1 ORDER BY id DESC LIMIT 100`, tenantID)
}

func ListSnapshotMembers(
	ctx context.Context,
	db Reader,
	tenantID string,
	admin bool,
	userID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT m.*,u.name,u.email,u.status AS user_status,u.gitlab_id,coalesce((SELECT jsonb_agg(jsonb_build_object('id',t.id,'name',t.name) ORDER BY t.name) FROM team_memberships tm JOIN teams t ON t.id=tm.team_id WHERE tm.membership_id=m.id),'[]'::jsonb) AS teams FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.status<>'removed' AND ($2 OR m.user_id=$3) ORDER BY u.name LIMIT 500`,
		tenantID,
		admin,
		userID,
	)
}

func ListSnapshotProjects(
	ctx context.Context,
	db Reader,
	tenantID string,
	admin bool,
	userID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT p.* FROM projects p WHERE p.tenant_id=$1 AND ($2 OR EXISTS(SELECT 1 FROM grants g JOIN memberships m ON m.id=g.membership_id WHERE g.project_id=p.id AND m.user_id=$3 AND m.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20)) ORDER BY p.name LIMIT 500`,
		tenantID,
		admin,
		userID,
	)
}

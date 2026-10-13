package table

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func LoadGrantSources(ctx context.Context, db Reader, tenantID string) ([]GrantSource, error) {
	rows, err := db.Query(ctx, `WITH sources AS (
 SELECT project_id,membership_id,level FROM direct_grants WHERE tenant_id=$1
 UNION ALL SELECT g.project_id,m.membership_id,g.level FROM team_grants g JOIN team_memberships m ON m.tenant_id=g.tenant_id AND m.team_id=g.team_id WHERE g.tenant_id=$1
 UNION ALL SELECT project_id,membership_id,0 FROM grants WHERE tenant_id=$1
 ) SELECT s.project_id,s.membership_id,s.level,m.status,coalesce(g.effective_level,0) AS effective_level
 FROM sources s JOIN memberships m ON m.id=s.membership_id AND m.tenant_id=$1
 LEFT JOIN grants g ON g.tenant_id=$1 AND g.project_id=s.project_id AND g.membership_id=s.membership_id`, tenantID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[GrantSource])
}

func SaveComputedGrants(
	ctx context.Context,
	db Writer,
	tenantID string,
	grants []ComputedGrant,
) (pgconn.CommandTag, error) {
	data, err := json.Marshal(grants)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return db.Exec(ctx, `INSERT INTO grants(tenant_id,project_id,membership_id,desired_level,effective_level)
 SELECT $1,project_id,membership_id,desired_level,effective_level FROM jsonb_to_recordset($2::jsonb) AS g(project_id text,membership_id text,desired_level integer,effective_level integer)
 ON CONFLICT(project_id,membership_id) DO UPDATE SET desired_level=EXCLUDED.desired_level,effective_level=EXCLUDED.effective_level`, tenantID, data)
}

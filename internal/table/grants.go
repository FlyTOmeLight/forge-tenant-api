package table

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
)

func LoadGrantSources(ctx context.Context, db *gorm.DB, tenantID string) ([]GrantSource, error) {
	var result []GrantSource
	err := db.WithContext(ctx).Raw(`WITH sources AS (
 SELECT project_id,membership_id,level FROM direct_grants WHERE tenant_id=?
 UNION ALL SELECT g.project_id,m.membership_id,g.level FROM team_grants g JOIN team_memberships m ON m.tenant_id=g.tenant_id AND m.team_id=g.team_id WHERE g.tenant_id=?
 UNION ALL SELECT project_id,membership_id,0 FROM grants WHERE tenant_id=?
 ) SELECT s.project_id,s.membership_id,s.level,m.status,coalesce(g.effective_level,0) AS effective_level
 FROM sources s JOIN memberships m ON m.id=s.membership_id AND m.tenant_id=?
 LEFT JOIN grants g ON g.tenant_id=? AND g.project_id=s.project_id AND g.membership_id=s.membership_id`, tenantID, tenantID, tenantID, tenantID, tenantID).Scan(&result).Error
	return result, err
}

func SaveComputedGrants(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	grants []ComputedGrant,
) (int64, error) {
	data, err := json.Marshal(grants)
	if err != nil {
		return 0, err
	}
	result := db.WithContext(ctx).
		Exec(`INSERT INTO grants(tenant_id,project_id,membership_id,desired_level,effective_level)
 SELECT ?,project_id,membership_id,desired_level,effective_level FROM jsonb_to_recordset(?::jsonb) AS g(project_id text,membership_id text,desired_level integer,effective_level integer)
 ON CONFLICT(project_id,membership_id) DO UPDATE SET desired_level=EXCLUDED.desired_level,effective_level=EXCLUDED.effective_level`, tenantID, string(data))
	return result.RowsAffected, result.Error
}

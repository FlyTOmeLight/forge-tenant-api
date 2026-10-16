package table

import (
	"context"

	"gorm.io/gorm"
)

type Team struct {
	ID       string  `gorm:"column:id;type:text;primaryKey;autoIncrement:false;uniqueIndex:teams_tenant_id_id_key,priority:2"`
	TenantID string  `gorm:"column:tenant_id;type:text;not null;uniqueIndex:teams_tenant_id_name_key,priority:1;uniqueIndex:teams_tenant_id_id_key,priority:1"`
	Name     string  `gorm:"column:name;type:text;not null;uniqueIndex:teams_tenant_id_name_key,priority:2"`
	Tenant   *Tenant `gorm:"foreignKey:TenantID;references:ID;constraint:teams_tenant_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
}

func (Team) TableName() string { return "teams" }

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

func ListMemberTeams(ctx context.Context, db *gorm.DB, tenantID string, membershipID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.id,t.name FROM teams t JOIN team_memberships tm ON tm.team_id=t.id WHERE tm.tenant_id=? AND tm.membership_id=? ORDER BY t.name`,
		tenantID,
		membershipID,
	)
}

func ListSnapshotTeams(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.*,(SELECT count(*) FROM team_memberships tm JOIN memberships m ON m.id=tm.membership_id WHERE tm.team_id=t.id AND m.status='active') AS member_count FROM teams t WHERE tenant_id=? ORDER BY name LIMIT 500`,
		tenantID,
	)
}

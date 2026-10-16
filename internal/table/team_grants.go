package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

type TeamGrant struct {
	TenantID  string   `gorm:"column:tenant_id;type:text;not null"`
	ProjectID string   `gorm:"column:project_id;type:text;primaryKey;autoIncrement:false"`
	TeamID    string   `gorm:"column:team_id;type:text;primaryKey;autoIncrement:false"`
	Level     int      `gorm:"column:level;type:integer;not null;check:team_grants_level_check,level IN (20,30,40)"`
	Project   *Project `gorm:"foreignKey:TenantID,ProjectID;references:TenantID,ID;constraint:team_grants_tenant_id_project_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
	Team      *Team    `gorm:"foreignKey:TenantID,TeamID;references:TenantID,ID;constraint:team_grants_tenant_id_team_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
}

func (TeamGrant) TableName() string { return "team_grants" }

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

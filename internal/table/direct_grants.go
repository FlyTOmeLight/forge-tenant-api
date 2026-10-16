package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

type DirectGrant struct {
	TenantID     string      `gorm:"column:tenant_id;type:text;not null"`
	ProjectID    string      `gorm:"column:project_id;type:text;primaryKey;autoIncrement:false"`
	MembershipID string      `gorm:"column:membership_id;type:text;primaryKey;autoIncrement:false"`
	Level        int         `gorm:"column:level;type:integer;not null;check:direct_grants_level_check,level IN (20,30,40)"`
	Project      *Project    `gorm:"foreignKey:TenantID,ProjectID;references:TenantID,ID;constraint:direct_grants_tenant_id_project_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
	Membership   *Membership `gorm:"foreignKey:TenantID,MembershipID;references:TenantID,ID;constraint:direct_grants_tenant_id_membership_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
}

func (DirectGrant) TableName() string { return "direct_grants" }

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

package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

type TeamMembership struct {
	TenantID     string      `gorm:"column:tenant_id;type:text;not null;index:team_memberships_member,priority:1"`
	TeamID       string      `gorm:"column:team_id;type:text;primaryKey;autoIncrement:false"`
	MembershipID string      `gorm:"column:membership_id;type:text;primaryKey;autoIncrement:false;index:team_memberships_member,priority:2"`
	Team         *Team       `gorm:"foreignKey:TenantID,TeamID;references:TenantID,ID;constraint:team_memberships_tenant_id_team_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
	Membership   *Membership `gorm:"foreignKey:TenantID,MembershipID;references:TenantID,ID;constraint:team_memberships_tenant_id_membership_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
}

func (TeamMembership) TableName() string { return "team_memberships" }

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

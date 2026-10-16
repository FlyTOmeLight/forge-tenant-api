package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

type Membership struct {
	ID       string  `gorm:"column:id;type:text;primaryKey;autoIncrement:false;uniqueIndex:memberships_tenant_id_id_key,priority:2"`
	TenantID string  `gorm:"column:tenant_id;type:text;not null;uniqueIndex:memberships_tenant_id_user_id_key,priority:1;uniqueIndex:memberships_tenant_id_id_key,priority:1"`
	UserID   string  `gorm:"column:user_id;type:text;not null;uniqueIndex:memberships_tenant_id_user_id_key,priority:2"`
	Role     string  `gorm:"column:role;type:text;not null;check:memberships_role_check,role IN ('admin','member')"`
	Status   string  `gorm:"column:status;type:text;not null;check:memberships_status_check,status IN ('invited','active','removed')"`
	Tenant   *Tenant `gorm:"foreignKey:TenantID;references:ID;constraint:memberships_tenant_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
	User     *User   `gorm:"foreignKey:UserID;references:ID;constraint:memberships_user_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
}

func (Membership) TableName() string { return "memberships" }

type MemberState struct{ Role, Status string }

type ProjectMember struct {
	Role, TenantStatus, MemberStatus, UserStatus string
	Desired, Observed, Effective                 int
}

func MembershipState(ctx context.Context, db *gorm.DB, tenantID string, membershipID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT role,status FROM memberships WHERE tenant_id=? AND id=?`, tenantID, membershipID).
		Row()
}

func ActiveAdminCount(ctx context.Context, db *gorm.DB, tenantID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT count(*) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.role='admin' AND m.status='active' AND u.status='active'`, tenantID).
		Row()
}

func ActiveMembershipExists(ctx context.Context, db *gorm.DB, tenantID string, userID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM memberships m JOIN tenants t ON t.id=m.tenant_id JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.user_id=? AND m.status='active' AND t.status='active' AND u.status='active')`, tenantID, userID).
		Row()
}

func ActivateMembership(ctx context.Context, db *gorm.DB, membershipID string) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE memberships SET status='active' WHERE id=?`, membershipID)
	return result.RowsAffected, result.Error
}

func MembershipByUser(ctx context.Context, db *gorm.DB, tenantID string, userID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT id,status FROM memberships WHERE tenant_id=? AND user_id=?`, tenantID, userID).
		Row()
}

func ReinviteMembership(ctx context.Context, db *gorm.DB, membershipID string, role string) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE memberships SET status='invited',role=? WHERE id=?`, role, membershipID)
	return result.RowsAffected, result.Error
}

func InsertMembership(
	ctx context.Context,
	db *gorm.DB,
	membershipID string,
	tenantID string,
	userID string,
	role string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES(?,?,?,?,'invited')`, membershipID, tenantID, userID, role)
	return result.RowsAffected, result.Error
}

func MembershipRole(ctx context.Context, db *gorm.DB, tenantID string, membershipID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT role FROM memberships WHERE tenant_id=? AND id=? AND status<>'removed'`, tenantID, membershipID).
		Row()
}

func UpdateMembershipRole(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	membershipID string,
	role string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE memberships SET role=? WHERE tenant_id=? AND id=?`, role, tenantID, membershipID)
	return result.RowsAffected, result.Error
}

func RemoveMembership(ctx context.Context, db *gorm.DB, tenantID string, membershipID string) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE memberships SET status='removed' WHERE tenant_id=? AND id=?`, tenantID, membershipID)
	return result.RowsAffected, result.Error
}

func InvitedMembershipExists(ctx context.Context, db *gorm.DB, tenantID string, membershipID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=? AND id=? AND status='invited')`, tenantID, membershipID).
		Row()
}

func TenantMemberState(ctx context.Context, db *gorm.DB, tenantID, userID string) (MemberState, error) {
	var record MemberState
	err := db.WithContext(ctx).
		Raw(`SELECT role,status FROM memberships WHERE tenant_id=? AND user_id=?`, tenantID, userID).
		Row().
		Scan(&record.Role, &record.Status)
	return record, err
}

func ListGrantCandidates(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT m.id,u.name,u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.status='active' AND u.status='active' ORDER BY u.name LIMIT 500`,
		tenantID,
	)
}

func MembershipExists(ctx context.Context, db *gorm.DB, tenantID string, userID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=? AND user_id=? AND status='active')`, tenantID, userID).
		Row()
}

func ListPermissionMember(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	membershipID string,
	userID string,
	admin bool,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT m.id,m.user_id,m.role,m.status,u.name,u.status AS user_status,u.operator,t.status AS tenant_status FROM memberships m JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE m.tenant_id=? AND ((?='' AND m.user_id=?) OR m.id=?) AND (? OR m.user_id=?) AND m.status<>'removed'`,
		tenantID,
		membershipID,
		userID,
		membershipID,
		admin,
		userID,
	)
}

func InsertTenantAdmin(
	ctx context.Context,
	db *gorm.DB,
	membershipID string,
	tenantID string,
	admin string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES(?,?,?,'admin','active')`, membershipID, tenantID, admin)
	return result.RowsAffected, result.Error
}

func ListSnapshotMembers(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	admin bool,
	userID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT m.*,u.name,u.email,u.status AS user_status,u.gitlab_id,coalesce((SELECT jsonb_agg(jsonb_build_object('id',t.id,'name',t.name) ORDER BY t.name) FROM team_memberships tm JOIN teams t ON t.id=tm.team_id WHERE tm.membership_id=m.id),'[]'::jsonb) AS teams FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.status<>'removed' AND (? OR m.user_id=?) ORDER BY u.name LIMIT 500`,
		tenantID,
		admin,
		userID,
	)
}

func ProjectMemberState(ctx context.Context, db *gorm.DB, tenantID, projectID, userID string) (ProjectMember, error) {
	var record ProjectMember
	err := db.WithContext(ctx).
		Raw(`SELECT m.role,t.status,m.status,u.status,coalesce(g.desired_level,0),coalesce(g.observed_level,0),coalesce(g.effective_level,0) FROM projects p JOIN tenants t ON t.id=p.tenant_id JOIN memberships m ON m.tenant_id=t.id JOIN users u ON u.id=m.user_id LEFT JOIN grants g ON g.project_id=p.id AND g.membership_id=m.id WHERE p.tenant_id=? AND p.id=? AND m.user_id=?`, tenantID, projectID, userID).
		Row().
		Scan(&record.Role, &record.TenantStatus, &record.MemberStatus, &record.UserStatus, &record.Desired, &record.Observed, &record.Effective)
	return record, err
}

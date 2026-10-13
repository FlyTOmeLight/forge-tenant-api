package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

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

func VisibleTenant(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	admin bool,
	userID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.* FROM tenants t WHERE id=? AND (? OR (status='active' AND EXISTS(SELECT 1 FROM memberships m WHERE m.tenant_id=t.id AND m.user_id=? AND m.status='active')))`,
		tenantID,
		admin,
		userID,
	)
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

func ListSyncUsers(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT DISTINCT u.id,u.subject,u.username,u.name,u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.status='active' AND u.status='active'`,
		tenantID,
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

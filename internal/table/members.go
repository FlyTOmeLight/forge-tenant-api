package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func MembershipState(ctx context.Context, db RowReader, tenantID string, membershipID string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT role,status FROM memberships WHERE tenant_id=$1 AND id=$2`,
		tenantID,
		membershipID,
	)
}

func ActiveAdminCount(ctx context.Context, db RowReader, tenantID string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT count(*) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.role='admin' AND m.status='active' AND u.status='active'`,
		tenantID,
	)
}

func ActiveMembershipExists(ctx context.Context, db RowReader, tenantID string, userID string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM memberships m JOIN tenants t ON t.id=m.tenant_id JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.user_id=$2 AND m.status='active' AND t.status='active' AND u.status='active')`,
		tenantID,
		userID,
	)
}

func ActivateMembership(ctx context.Context, db Writer, membershipID string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE memberships SET status='active' WHERE id=$1`, membershipID)
}

func MembershipByUser(ctx context.Context, db RowReader, tenantID string, userID string) pgx.Row {
	return db.QueryRow(ctx, `SELECT id,status FROM memberships WHERE tenant_id=$1 AND user_id=$2`, tenantID, userID)
}

func ReinviteMembership(ctx context.Context, db Writer, membershipID string, role string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE memberships SET status='invited',role=$2 WHERE id=$1`, membershipID, role)
}

func InsertMembership(
	ctx context.Context,
	db Writer,
	membershipID string,
	tenantID string,
	userID string,
	role string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES($1,$2,$3,$4,'invited')`,
		membershipID,
		tenantID,
		userID,
		role,
	)
}

func MembershipRole(ctx context.Context, db RowReader, tenantID string, membershipID string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT role FROM memberships WHERE tenant_id=$1 AND id=$2 AND status<>'removed'`,
		tenantID,
		membershipID,
	)
}

func UpdateMembershipRole(
	ctx context.Context,
	db Writer,
	tenantID string,
	membershipID string,
	role string,
) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE memberships SET role=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, membershipID, role)
}

func RemoveMembership(ctx context.Context, db Writer, tenantID string, membershipID string) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`UPDATE memberships SET status='removed' WHERE tenant_id=$1 AND id=$2`,
		tenantID,
		membershipID,
	)
}

func InvitedMembershipExists(ctx context.Context, db RowReader, tenantID string, membershipID string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=$1 AND id=$2 AND status='invited')`,
		tenantID,
		membershipID,
	)
}

func TenantMemberState(ctx context.Context, db RowReader, tenantID, userID string) (MemberState, error) {
	var record MemberState
	err := db.QueryRow(ctx, `SELECT role,status FROM memberships WHERE tenant_id=$1 AND user_id=$2`, tenantID, userID).
		Scan(&record.Role, &record.Status)
	return record, err
}

func VisibleTenant(
	ctx context.Context,
	db Reader,
	tenantID string,
	admin bool,
	userID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.* FROM tenants t WHERE id=$1 AND ($2 OR (status='active' AND EXISTS(SELECT 1 FROM memberships m WHERE m.tenant_id=t.id AND m.user_id=$3 AND m.status='active')))`,
		tenantID,
		admin,
		userID,
	)
}

func ListGrantCandidates(ctx context.Context, db Reader, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT m.id,u.name,u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.status='active' AND u.status='active' ORDER BY u.name LIMIT 500`,
		tenantID,
	)
}

func MembershipExists(ctx context.Context, db RowReader, tenantID string, userID string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=$1 AND user_id=$2 AND status='active')`,
		tenantID,
		userID,
	)
}

func ListPermissionMember(
	ctx context.Context,
	db Reader,
	tenantID string,
	membershipID string,
	userID string,
	admin bool,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT m.id,m.user_id,m.role,m.status,u.name,u.status AS user_status,u.operator,t.status AS tenant_status FROM memberships m JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE m.tenant_id=$1 AND (($2='' AND m.user_id=$3) OR m.id=$2) AND ($4 OR m.user_id=$3) AND m.status<>'removed'`,
		tenantID,
		membershipID,
		userID,
		admin,
	)
}

func ListSyncUsers(ctx context.Context, db Reader, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT DISTINCT u.id,u.subject,u.username,u.name,u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.status='active' AND u.status='active'`,
		tenantID,
	)
}

func InsertTenantAdmin(
	ctx context.Context,
	db Writer,
	membershipID string,
	tenantID string,
	admin string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES($1,$2,$3,'admin','active')`,
		membershipID,
		tenantID,
		admin,
	)
}

package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func InspectInvitation(ctx context.Context, db RowReader, tokenHash string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT u.name,u.email,u.status,t.name FROM invitations i JOIN memberships m ON m.id=i.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE i.token_hash=$1 AND i.consumed_at IS NULL AND i.expires_at>now() AND m.status='invited' AND t.status='active'`,
		tokenHash,
	)
}

func InvitationTenant(ctx context.Context, db RowReader, tokenHash string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT m.tenant_id FROM invitations i JOIN memberships m ON m.id=i.membership_id WHERE i.token_hash=$1`,
		tokenHash,
	)
}

func LockInvitation(ctx context.Context, db RowReader, tokenHash string) (Invitation, error) {
	var record Invitation
	err := db.QueryRow(
		ctx,
		`SELECT i.id,m.id,u.id,u.name,u.email,u.username,u.status FROM invitations i JOIN memberships m ON m.id=i.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE i.token_hash=$1 AND i.consumed_at IS NULL AND i.expires_at>now() AND m.status='invited' AND t.status='active' FOR UPDATE OF i,u`,
		tokenHash,
	).Scan(&record.ID, &record.MembershipID, &record.UserID, &record.Name, &record.Email, &record.Username, &record.UserStatus)
	return record, err
}

func ConsumeInvitation(ctx context.Context, db Writer, invitationID string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE invitations SET consumed_at=now() WHERE id=$1`, invitationID)
}

func InvalidateInvitations(ctx context.Context, db Writer, membershipID string) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`UPDATE invitations SET consumed_at=now() WHERE membership_id=$1 AND consumed_at IS NULL`,
		membershipID,
	)
}

func InsertInvitation(
	ctx context.Context,
	db Writer,
	invitationID string,
	Hashtoken string,
	membershipID string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO invitations(id,token_hash,membership_id,expires_at) VALUES($1,$2,$3,now()+interval '24 hours')`,
		invitationID,
		Hashtoken,
		membershipID,
	)
}

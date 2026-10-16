package table

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
)

type Invitation struct {
	ID           string     `gorm:"type:text;primaryKey"`
	TokenHash    string     `gorm:"type:text;not null;unique"`
	MembershipID string     `gorm:"type:text;not null"`
	ExpiresAt    time.Time  `gorm:"type:timestamptz;not null;autoCreateTime:false;autoUpdateTime:false"`
	ConsumedAt   *time.Time `gorm:"type:timestamptz;autoCreateTime:false;autoUpdateTime:false"`
	CreatedAt    time.Time  `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false;autoUpdateTime:false"`
	Membership   Membership `gorm:"foreignKey:MembershipID;references:ID;constraint:invitations_membership_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION"`
}

func (Invitation) TableName() string { return "invitations" }

type InvitationIdentity struct{ ID, MembershipID, UserID, Name, Email, Username, UserStatus string }

func InspectInvitation(ctx context.Context, db *gorm.DB, tokenHash string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT u.name,u.email,u.status,t.name FROM invitations i JOIN memberships m ON m.id=i.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE i.token_hash=? AND i.consumed_at IS NULL AND i.expires_at>now() AND m.status='invited' AND t.status='active'`, tokenHash).
		Row()
}

func InvitationTenant(ctx context.Context, db *gorm.DB, tokenHash string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT m.tenant_id FROM invitations i JOIN memberships m ON m.id=i.membership_id WHERE i.token_hash=?`, tokenHash).
		Row()
}

func LockInvitation(ctx context.Context, db *gorm.DB, tokenHash string) (InvitationIdentity, error) {
	var record InvitationIdentity
	err := db.WithContext(ctx).
		Raw(`SELECT i.id,m.id,u.id,u.name,u.email,u.username,u.status FROM invitations i JOIN memberships m ON m.id=i.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE i.token_hash=? AND i.consumed_at IS NULL AND i.expires_at>now() AND m.status='invited' AND t.status='active' FOR UPDATE OF i,u`, tokenHash).
		Row().
		Scan(&record.ID, &record.MembershipID, &record.UserID, &record.Name, &record.Email, &record.Username, &record.UserStatus)
	return record, err
}

func ConsumeInvitation(ctx context.Context, db *gorm.DB, invitationID string) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE invitations SET consumed_at=now() WHERE id=?`, invitationID)
	return result.RowsAffected, result.Error
}

func InvalidateInvitations(ctx context.Context, db *gorm.DB, membershipID string) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE invitations SET consumed_at=now() WHERE membership_id=? AND consumed_at IS NULL`, membershipID)
	return result.RowsAffected, result.Error
}

func InsertInvitation(
	ctx context.Context,
	db *gorm.DB,
	invitationID string,
	Hashtoken string,
	membershipID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO invitations(id,token_hash,membership_id,expires_at) VALUES(?,?,?,now()+interval '24 hours')`, invitationID, Hashtoken, membershipID)
	return result.RowsAffected, result.Error
}

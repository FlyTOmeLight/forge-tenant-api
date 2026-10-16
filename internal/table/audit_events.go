package table

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type AuditEvent struct {
	ID          int64     `gorm:"type:bigint;primaryKey;autoIncrement"`
	TenantID    *string   `gorm:"type:text"`
	ActorID     string    `gorm:"type:text;not null"`
	Action      string    `gorm:"type:text;not null"`
	Target      string    `gorm:"type:text;not null"`
	Detail      []byte    `gorm:"type:jsonb;not null;default:'{}'"`
	OperationID *string   `gorm:"type:text"`
	CreatedAt   time.Time `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false;autoUpdateTime:false"`
	Tenant      *Tenant   `gorm:"foreignKey:TenantID;references:ID;constraint:audit_events_tenant_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION"`
}

func (AuditEvent) TableName() string { return "audit_events" }

func InsertChangeAudit(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	userID string,
	action string,
	target string,
	detail []byte,
	operationID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO audit_events(tenant_id,actor_id,action,target,detail,operation_id) VALUES(?,?,?,?,?,?)`, tenantID, userID, action, target, string(detail), operationID)
	return result.RowsAffected, result.Error
}

func InsertInvitationAudit(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	userID string,
	membershipID string,
	operationID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO audit_events(tenant_id,actor_id,action,target,operation_id) VALUES(?,?,'accept_invitation',?,?)`, tenantID, userID, membershipID, operationID)
	return result.RowsAffected, result.Error
}

func InsertSyncAudit(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	tenantIDTarget string,
	version int64,
	operationID string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO audit_events(tenant_id,actor_id,action,target,detail,operation_id) VALUES(?,'worker','sync_succeeded',?,jsonb_build_object('version',?::bigint),?)`, tenantID, tenantIDTarget, version, operationID)
	return result.RowsAffected, result.Error
}

func ListSnapshotAudit(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(ctx, db, `SELECT * FROM audit_events WHERE tenant_id=? ORDER BY id DESC LIMIT 100`, tenantID)
}

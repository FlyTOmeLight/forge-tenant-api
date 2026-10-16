package table

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type AccessEvent struct {
	ID        int64     `gorm:"type:bigint;primaryKey;autoIncrement"`
	TenantID  string    `gorm:"type:text;not null"`
	Version   int64     `gorm:"type:bigint;not null"`
	Reason    string    `gorm:"type:text;not null"`
	CreatedAt time.Time `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false;autoUpdateTime:false"`
	Tenant    Tenant    `gorm:"foreignKey:TenantID;references:ID;constraint:access_events_tenant_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION"`
}

func (AccessEvent) TableName() string { return "access_events" }

func InsertAccessEvent(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	version int64,
	action string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO access_events(tenant_id,version,reason) VALUES(?,?,?)`, tenantID, version, action)
	return result.RowsAffected, result.Error
}

func ListAccessEvents(ctx context.Context, db *gorm.DB, after int64) ([]map[string]any, error) {
	return collect(ctx, db, `SELECT * FROM access_events WHERE id>? ORDER BY id LIMIT 200`, after)
}

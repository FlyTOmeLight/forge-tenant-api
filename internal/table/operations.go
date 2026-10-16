package table

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
)

type Operation struct {
	ID            string     `gorm:"type:text;primaryKey"`
	TenantID      string     `gorm:"type:text;not null;uniqueIndex:operations_tenant_id_version_key,priority:1"`
	Version       int64      `gorm:"type:bigint;not null;uniqueIndex:operations_tenant_id_version_key,priority:2"`
	Status        string     `gorm:"type:text;not null;check:operations_status_check,status IN ('pending','running','retrying','failed','conflict','succeeded','superseded')"`
	Attempts      int        `gorm:"type:integer;not null;default:0"`
	NextAttemptAt time.Time  `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false;autoUpdateTime:false;index:operations_ready,where:status = 'pending' OR status = 'retrying' OR status = 'running'"`
	LeaseUntil    *time.Time `gorm:"type:timestamptz;autoCreateTime:false;autoUpdateTime:false"`
	ErrorCode     string     `gorm:"type:text;not null;default:''"`
	ErrorMessage  string     `gorm:"type:text;not null;default:''"`
	CreatedAt     time.Time  `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false;autoUpdateTime:false"`
	UpdatedAt     time.Time  `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false;autoUpdateTime:false"`
	Tenant        Tenant     `gorm:"foreignKey:TenantID;references:ID;constraint:operations_tenant_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION"`
}

func (Operation) TableName() string { return "operations" }

func SupersedeOperations(ctx context.Context, db *gorm.DB, tenantID string) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE operations SET status='superseded',updated_at=now() WHERE tenant_id=? AND status IN ('pending','running','retrying','failed','conflict')`, tenantID)
	return result.RowsAffected, result.Error
}

func InsertOperation(ctx context.Context, db *gorm.DB, id string, tenantID string, v int64) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO operations(id,tenant_id,version,status) VALUES(?,?,?,'pending')`, id, tenantID, v)
	return result.RowsAffected, result.Error
}

func RetryableOperationExists(ctx context.Context, db *gorm.DB, tenantID string, operationID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM operations o JOIN tenants t ON t.id=o.tenant_id WHERE o.tenant_id=? AND o.id=? AND o.version=t.version AND o.status IN ('failed','conflict'))`, tenantID, operationID).
		Row()
}

func RescheduleReconciliation(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE operations o SET status='pending',attempts=0,next_attempt_at=now(),updated_at=now() FROM tenants t WHERE o.tenant_id=t.id AND o.version=t.version AND o.status='succeeded' AND o.updated_at<now()-interval '5 minutes'`)
	return result.RowsAffected, result.Error
}

func NextOperation(ctx context.Context, db *gorm.DB) (Operation, error) {
	var record Operation
	err := db.WithContext(ctx).
		Raw(`SELECT id,tenant_id,version FROM operations WHERE (status IN ('pending','retrying') AND next_attempt_at<=now()) OR (status='running' AND lease_until<now()) ORDER BY next_attempt_at LIMIT 1`).
		Row().
		Scan(&record.ID, &record.TenantID, &record.Version)
	return record, err
}

func ClaimOperation(ctx context.Context, db *gorm.DB, operationID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`UPDATE operations SET status='running',attempts=attempts+1,lease_until=now()+interval '2 minutes',updated_at=now() WHERE id=? AND ((status IN ('pending','retrying') AND next_attempt_at<=now()) OR (status='running' AND lease_until<now())) RETURNING attempts`, operationID).
		Row()
}

func FinishOperation(
	ctx context.Context,
	db *gorm.DB,
	operationID string,
	status string,
	code string,
	message string,
	delaySeconds float64,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE operations SET status=?,error_code=?,error_message=?,next_attempt_at=now()+make_interval(secs=>?),lease_until=NULL,updated_at=now() WHERE id=? AND status='running'`, status, code, message, delaySeconds, operationID)
	return result.RowsAffected, result.Error
}

func ListSnapshotOperations(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(ctx, db, `SELECT * FROM operations WHERE tenant_id=? ORDER BY version DESC LIMIT 100`, tenantID)
}

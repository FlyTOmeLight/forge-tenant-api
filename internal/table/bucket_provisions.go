package table

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// TenantBucketProvision is independent of versioned GitLab synchronization.
type TenantBucketProvision struct {
	Tenant        *Tenant    `gorm:"foreignKey:TenantID;references:ID;constraint:tenant_bucket_provisions_tenant_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
	TenantID      string     `gorm:"type:text;primaryKey;autoIncrement:false"`
	Status        string     `gorm:"type:text;not null;default:pending;check:tenant_bucket_provisions_status_check,status IN ('pending','running','succeeded','failed')"`
	Attempts      int        `gorm:"not null;default:0"`
	NextAttemptAt time.Time  `gorm:"type:timestamptz;not null;default:now();index"`
	LeaseID       string     `gorm:"type:text;not null;default:''"`
	LeaseUntil    *time.Time `gorm:"type:timestamptz"`
	LastError     string     `gorm:"type:text;not null;default:''"`
	BackendID     string     `gorm:"type:text;not null;default:''"`
	Bucket        string     `gorm:"type:text;not null;default:''"`
}

func (TenantBucketProvision) TableName() string { return "tenant_bucket_provisions" }

func EnqueueTenantBucket(ctx context.Context, db *gorm.DB, tenantID string) error {
	return db.WithContext(ctx).
		Exec(`INSERT INTO tenant_bucket_provisions(tenant_id) VALUES(?) ON CONFLICT (tenant_id) DO NOTHING`, tenantID).
		Error
}

// BackfillTenantBuckets registers existing tenants without replacing confirmed work.
func BackfillTenantBuckets(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO tenant_bucket_provisions(tenant_id) SELECT id FROM tenants ON CONFLICT (tenant_id) DO NOTHING`)
	return result.RowsAffected, result.Error
}

// RetryFailedTenantBucket is an explicit operator action, scoped to one failed job.
func RetryFailedTenantBucket(ctx context.Context, db *gorm.DB, tenantID string) (bool, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE tenant_bucket_provisions SET status='pending',attempts=0,next_attempt_at=now(),lease_id='',lease_until=NULL,last_error='' WHERE tenant_id=? AND status='failed'`, tenantID)
	return result.RowsAffected == 1, result.Error
}

// ClaimTenantBucket atomically fences an eligible job for a bounded remote call.
func ClaimTenantBucket(
	ctx context.Context,
	db *gorm.DB,
	leaseID string,
	lease time.Duration,
	maxAttempts int,
) (TenantBucketProvision, error) {
	var job TenantBucketProvision
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			`UPDATE tenant_bucket_provisions SET status='failed',lease_id='',lease_until=NULL,last_error='attempt budget exhausted' WHERE attempts>=? AND (status='pending' OR (status='running' AND lease_until<=now()))`,
			maxAttempts,
		).Error; err != nil {
			return err
		}
		return tx.Raw(`WITH candidate AS (SELECT tenant_id FROM tenant_bucket_provisions WHERE attempts<? AND ((status='pending' AND next_attempt_at<=now()) OR (status='running' AND lease_until<=now())) ORDER BY next_attempt_at,tenant_id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE tenant_bucket_provisions p SET status='running',attempts=p.attempts+1,lease_id=?,lease_until=now()+(? * interval '1 second') FROM candidate c WHERE p.tenant_id=c.tenant_id RETURNING p.*`, maxAttempts, leaseID, int(lease/time.Second)).
			Scan(&job).
			Error
	})
	return job, err
}

// CompleteTenantBucket refuses acknowledgements from replaced leases.
func CompleteTenantBucket(ctx context.Context, db *gorm.DB, job TenantBucketProvision) (bool, error) {
	result := db.WithContext(ctx).
		Model(&TenantBucketProvision{}).
		Where("tenant_id=? AND status='running' AND lease_id=?", job.TenantID, job.LeaseID).
		Updates(map[string]any{
			"status":          job.Status,
			"backend_id":      job.BackendID,
			"bucket":          job.Bucket,
			"last_error":      job.LastError,
			"next_attempt_at": job.NextAttemptAt,
			"lease_id":        "",
			"lease_until":     nil,
		})
	return result.RowsAffected == 1, result.Error
}

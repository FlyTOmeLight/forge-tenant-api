package table

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
)

type Tenant struct {
	ID            string     `gorm:"column:id;type:text;primaryKey;autoIncrement:false"`
	Name          string     `gorm:"column:name;type:text;not null"`
	Slug          string     `gorm:"column:slug;type:text;not null;unique"`
	Status        string     `gorm:"column:status;type:text;not null;default:active;check:tenants_status_check,status IN ('active','suspended')"`
	Version       int64      `gorm:"column:version;type:bigint;not null;default:1;autoIncrement:false"`
	GitLabID      *int64     `gorm:"column:gitlab_id;type:bigint;unique"`
	LastCheckedAt *time.Time `gorm:"column:last_checked_at;type:timestamptz"`
	CreatedAt     time.Time  `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false"`
}

func (Tenant) TableName() string { return "tenants" }

type TenantSyncState struct {
	Name, Slug, Status string
	GitLabID           int64
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

func ListUserTenants(ctx context.Context, db *gorm.DB, userID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.*, (SELECT count(*) FROM memberships m WHERE m.tenant_id=t.id AND m.status<>'removed') AS member_count,(SELECT count(*) FROM projects p WHERE p.tenant_id=t.id) AS project_count,coalesce((SELECT status FROM operations o WHERE o.tenant_id=t.id AND o.version=t.version),'pending') AS sync_status FROM tenants t WHERE EXISTS(SELECT 1 FROM memberships m WHERE m.tenant_id=t.id AND m.user_id=? AND m.status='active') ORDER BY t.created_at DESC LIMIT 200`,
		userID,
	)
}

func ListPlatformTenants(ctx context.Context, db *gorm.DB) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.*, (SELECT count(*) FROM memberships m WHERE m.tenant_id=t.id AND m.status<>'removed') AS member_count,(SELECT count(*) FROM projects p WHERE p.tenant_id=t.id) AS project_count,coalesce((SELECT status FROM operations o WHERE o.tenant_id=t.id AND o.version=t.version),'pending') AS sync_status FROM tenants t ORDER BY created_at DESC LIMIT 200`,
	)
}

func TenantVersion(ctx context.Context, db *gorm.DB, tenantID string) *sql.Row {
	return db.WithContext(ctx).Raw(`SELECT version FROM tenants WHERE id=?`, tenantID).Row()
}

func AdvanceTenantVersion(ctx context.Context, db *gorm.DB, tenantID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`UPDATE tenants SET version=version+1 WHERE id=? RETURNING version`, tenantID).
		Row()
}

func TenantActive(ctx context.Context, db *gorm.DB, tenantID string) *sql.Row {
	return db.WithContext(ctx).Raw(`SELECT status='active' FROM tenants WHERE id=?`, tenantID).Row()
}

func TenantSlug(ctx context.Context, db *gorm.DB, tenantID string) *sql.Row {
	return db.WithContext(ctx).Raw(`SELECT slug FROM tenants WHERE id=?`, tenantID).Row()
}

func MarkTenantChecked(ctx context.Context, db *gorm.DB, tenantID string, version int64) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE tenants SET last_checked_at=now() WHERE id=? AND version=?`, tenantID, version)
	return result.RowsAffected, result.Error
}

func TenantSyncTarget(ctx context.Context, db *gorm.DB, tenantID string) (TenantSyncState, error) {
	var record TenantSyncState
	err := db.WithContext(ctx).
		Raw(`SELECT name,slug,status,coalesce(gitlab_id,0) FROM tenants WHERE id=?`, tenantID).
		Row().
		Scan(&record.Name, &record.Slug, &record.Status, &record.GitLabID)
	return record, err
}

func SetTenantGitLabID(ctx context.Context, db *gorm.DB, tenantID string, gitlabID int64) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE tenants SET gitlab_id=? WHERE id=?`, gitlabID, tenantID)
	return result.RowsAffected, result.Error
}

func InsertTenant(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	name string,
	slug string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO tenants(id,name,slug,version) VALUES(?,?,?,0)`, tenantID, name, slug)
	return result.RowsAffected, result.Error
}

func TenantStatus(ctx context.Context, db *gorm.DB, tenantID string) *sql.Row {
	return db.WithContext(ctx).Raw(`SELECT status FROM tenants WHERE id=?`, tenantID).Row()
}

func UpdateTenantStatus(ctx context.Context, db *gorm.DB, tenantID string, status string) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE tenants SET status=? WHERE id=?`, status, tenantID)
	return result.RowsAffected, result.Error
}

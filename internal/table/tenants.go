package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

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

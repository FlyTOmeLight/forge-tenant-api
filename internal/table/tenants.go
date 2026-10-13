package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TenantVersion(ctx context.Context, db RowReader, tenantID string) pgx.Row {
	return db.QueryRow(ctx, `SELECT version FROM tenants WHERE id=$1`, tenantID)
}

func AdvanceTenantVersion(ctx context.Context, db RowReader, tenantID string) pgx.Row {
	return db.QueryRow(ctx, `UPDATE tenants SET version=version+1 WHERE id=$1 RETURNING version`, tenantID)
}

func TenantActive(ctx context.Context, db RowReader, tenantID string) pgx.Row {
	return db.QueryRow(ctx, `SELECT status='active' FROM tenants WHERE id=$1`, tenantID)
}

func TenantSlug(ctx context.Context, db RowReader, tenantID string) pgx.Row {
	return db.QueryRow(ctx, `SELECT slug FROM tenants WHERE id=$1`, tenantID)
}

func MarkTenantChecked(ctx context.Context, db Writer, tenantID string, version int64) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE tenants SET last_checked_at=now() WHERE id=$1 AND version=$2`, tenantID, version)
}

func TenantSyncTarget(ctx context.Context, db RowReader, tenantID string) (TenantSyncState, error) {
	var record TenantSyncState
	err := db.QueryRow(ctx, `SELECT name,slug,status,coalesce(gitlab_id,0) FROM tenants WHERE id=$1`, tenantID).
		Scan(&record.Name, &record.Slug, &record.Status, &record.GitLabID)
	return record, err
}

func SetTenantGitLabID(ctx context.Context, db Writer, tenantID string, gitlabID int64) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE tenants SET gitlab_id=$2 WHERE id=$1`, tenantID, gitlabID)
}

func InsertTenant(
	ctx context.Context,
	db Writer,
	tenantID string,
	name string,
	slug string,
) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `INSERT INTO tenants(id,name,slug,version) VALUES($1,$2,$3,0)`, tenantID, name, slug)
}

func TenantStatus(ctx context.Context, db RowReader, tenantID string) pgx.Row {
	return db.QueryRow(ctx, `SELECT status FROM tenants WHERE id=$1`, tenantID)
}

func UpdateTenantStatus(ctx context.Context, db Writer, tenantID string, status string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE tenants SET status=$2 WHERE id=$1`, tenantID, status)
}

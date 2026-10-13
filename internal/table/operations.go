package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

func FindIdempotency(ctx context.Context, db *gorm.DB, userID string, key string) (Idempotency, error) {
	var record Idempotency
	err := db.WithContext(ctx).
		Raw(`SELECT route,body_hash,status_code,response FROM idempotency WHERE actor_id=? AND request_key=?`, userID, key).
		Row().
		Scan(&record.Route, &record.Digest, &record.Status, &record.Response)
	return record, err
}

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

func InsertIdempotency(
	ctx context.Context,
	db *gorm.DB,
	userID string,
	key string,
	route string,
	digest string,
	encryptedResult []byte,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO idempotency(actor_id,request_key,route,body_hash,status_code,response) VALUES(?,?,?,?,202,?)`, userID, key, route, digest, encryptedResult)
	return result.RowsAffected, result.Error
}

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

func ListAccessEvents(ctx context.Context, db *gorm.DB, after int64) ([]map[string]any, error) {
	return collect(ctx, db, `SELECT * FROM access_events WHERE id>? ORDER BY id LIMIT 200`, after)
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

func RetryableOperationExists(ctx context.Context, db *gorm.DB, tenantID string, operationID string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM operations o JOIN tenants t ON t.id=o.tenant_id WHERE o.tenant_id=? AND o.id=? AND o.version=t.version AND o.status IN ('failed','conflict'))`, tenantID, operationID).
		Row()
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

func RescheduleReconciliation(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE operations o SET status='pending',attempts=0,next_attempt_at=now(),updated_at=now() FROM tenants t WHERE o.tenant_id=t.id AND o.version=t.version AND o.status='succeeded' AND o.updated_at<now()-interval '5 minutes'`)
	return result.RowsAffected, result.Error
}

func PruneIdempotency(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).Exec(`DELETE FROM idempotency WHERE created_at<now()-interval '24 hours'`)
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

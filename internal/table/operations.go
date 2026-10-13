package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func FindIdempotency(ctx context.Context, db RowReader, userID string, key string) (Idempotency, error) {
	var record Idempotency
	err := db.QueryRow(
		ctx,
		`SELECT route,body_hash,status_code,response FROM idempotency WHERE actor_id=$1 AND request_key=$2`,
		userID,
		key,
	).Scan(&record.Route, &record.Digest, &record.Status, &record.Response)
	return record, err
}

func InsertChangeAudit(
	ctx context.Context,
	db Writer,
	tenantID string,
	userID string,
	action string,
	target string,
	detail []byte,
	operationID string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO audit_events(tenant_id,actor_id,action,target,detail,operation_id) VALUES($1,$2,$3,$4,$5,$6)`,
		tenantID,
		userID,
		action,
		target,
		detail,
		operationID,
	)
}

func InsertAccessEvent(
	ctx context.Context,
	db Writer,
	tenantID string,
	version int64,
	action string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO access_events(tenant_id,version,reason) VALUES($1,$2,$3)`,
		tenantID,
		version,
		action,
	)
}

func InsertIdempotency(
	ctx context.Context,
	db Writer,
	userID string,
	key string,
	route string,
	digest string,
	encryptedResult []byte,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO idempotency(actor_id,request_key,route,body_hash,status_code,response) VALUES($1,$2,$3,$4,202,$5)`,
		userID,
		key,
		route,
		digest,
		encryptedResult,
	)
}

func SupersedeOperations(ctx context.Context, db Writer, tenantID string) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`UPDATE operations SET status='superseded',updated_at=now() WHERE tenant_id=$1 AND status IN ('pending','running','retrying','failed','conflict')`,
		tenantID,
	)
}

func InsertOperation(ctx context.Context, db Writer, id string, tenantID string, v int64) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO operations(id,tenant_id,version,status) VALUES($1,$2,$3,'pending')`,
		id,
		tenantID,
		v,
	)
}

func ListAccessEvents(ctx context.Context, db Reader, after int64) ([]map[string]any, error) {
	return collect(ctx, db, `SELECT * FROM access_events WHERE id>$1 ORDER BY id LIMIT 200`, after)
}

func InsertInvitationAudit(
	ctx context.Context,
	db Writer,
	tenantID string,
	userID string,
	membershipID string,
	operationID string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO audit_events(tenant_id,actor_id,action,target,operation_id) VALUES($1,$2,'accept_invitation',$3,$4)`,
		tenantID,
		userID,
		membershipID,
		operationID,
	)
}

func RetryableOperationExists(ctx context.Context, db RowReader, tenantID string, operationID string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM operations o JOIN tenants t ON t.id=o.tenant_id WHERE o.tenant_id=$1 AND o.id=$2 AND o.version=t.version AND o.status IN ('failed','conflict'))`,
		tenantID,
		operationID,
	)
}

func ListUserTenants(ctx context.Context, db Reader, userID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.*, (SELECT count(*) FROM memberships m WHERE m.tenant_id=t.id AND m.status<>'removed') AS member_count,(SELECT count(*) FROM projects p WHERE p.tenant_id=t.id) AS project_count,coalesce((SELECT status FROM operations o WHERE o.tenant_id=t.id AND o.version=t.version),'pending') AS sync_status FROM tenants t WHERE EXISTS(SELECT 1 FROM memberships m WHERE m.tenant_id=t.id AND m.user_id=$1 AND m.status='active') ORDER BY t.created_at DESC LIMIT 200`,
		userID,
	)
}

func ListPlatformTenants(ctx context.Context, db Reader) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT t.*, (SELECT count(*) FROM memberships m WHERE m.tenant_id=t.id AND m.status<>'removed') AS member_count,(SELECT count(*) FROM projects p WHERE p.tenant_id=t.id) AS project_count,coalesce((SELECT status FROM operations o WHERE o.tenant_id=t.id AND o.version=t.version),'pending') AS sync_status FROM tenants t ORDER BY created_at DESC LIMIT 200`,
	)
}

func RescheduleReconciliation(ctx context.Context, db Writer) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`UPDATE operations o SET status='pending',attempts=0,next_attempt_at=now(),updated_at=now() FROM tenants t WHERE o.tenant_id=t.id AND o.version=t.version AND o.status='succeeded' AND o.updated_at<now()-interval '5 minutes'`,
	)
}

func PruneIdempotency(ctx context.Context, db Writer) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `DELETE FROM idempotency WHERE created_at<now()-interval '24 hours'`)
}

func NextOperation(ctx context.Context, db RowReader) (Operation, error) {
	var record Operation
	err := db.QueryRow(
		ctx,
		`SELECT id,tenant_id,version FROM operations WHERE (status IN ('pending','retrying') AND next_attempt_at<=now()) OR (status='running' AND lease_until<now()) ORDER BY next_attempt_at LIMIT 1`,
	).Scan(&record.ID, &record.TenantID, &record.Version)
	return record, err
}

func ClaimOperation(ctx context.Context, db RowReader, operationID string) pgx.Row {
	return db.QueryRow(
		ctx,
		`UPDATE operations SET status='running',attempts=attempts+1,lease_until=now()+interval '2 minutes',updated_at=now() WHERE id=$1 AND ((status IN ('pending','retrying') AND next_attempt_at<=now()) OR (status='running' AND lease_until<now())) RETURNING attempts`,
		operationID,
	)
}

func FinishOperation(
	ctx context.Context,
	db Writer,
	operationID string,
	status string,
	code string,
	message string,
	delaySeconds float64,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`UPDATE operations SET status=$2,error_code=$3,error_message=$4,next_attempt_at=now()+make_interval(secs=>$5),lease_until=NULL,updated_at=now() WHERE id=$1 AND status='running'`,
		operationID,
		status,
		code,
		message,
		delaySeconds,
	)
}

func InsertSyncAudit(
	ctx context.Context,
	db Writer,
	tenantID string,
	tenantIDTarget string,
	version int64,
	operationID string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO audit_events(tenant_id,actor_id,action,target,detail,operation_id) VALUES($1,'worker','sync_succeeded',$2,jsonb_build_object('version',$3::bigint),$4)`,
		tenantID,
		tenantIDTarget,
		version,
		operationID,
	)
}

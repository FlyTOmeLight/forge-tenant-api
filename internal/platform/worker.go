package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/remote"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errSuperseded = errors.New("target version superseded")

func (a *App) RunWorker(ctx context.Context) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	reconcile := time.NewTicker(5 * time.Minute)
	defer reconcile.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if e := a.runNext(ctx); e != nil && ctx.Err() == nil {
				slog.Error("sync worker error", "error", e.Error())
			}
		case <-reconcile.C:
			if e := a.reconcile(ctx); e != nil && ctx.Err() == nil {
				slog.Error("reconcile failed", "error", e.Error())
			}
		}
	}
}
func (a *App) reconcile(ctx context.Context) error {
	tx, e := a.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `UPDATE operations o SET status='pending',attempts=0,next_attempt_at=now(),updated_at=now() FROM tenants t WHERE o.tenant_id=t.id AND o.version=t.version AND o.status='succeeded' AND o.updated_at<now()-interval '5 minutes'`)
	if e != nil {
		return e
	}
	for _, table := range []string{"sessions", "login_flows"} {
		if _, e = tx.Exec(ctx, "DELETE FROM "+table+" WHERE expires_at<now()"); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `DELETE FROM idempotency WHERE created_at<now()-interval '24 hours'`); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (a *App) runNext(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	conn, e := a.DB.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	var oid, tid string
	var version int64
	e = conn.QueryRow(ctx, `SELECT id,tenant_id,version FROM operations WHERE (status IN ('pending','retrying') AND next_attempt_at<=now()) OR (status='running' AND lease_until<now()) ORDER BY next_attempt_at LIMIT 1`).Scan(&oid, &tid, &version)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	var locked bool
	e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "sync:"+tid).Scan(&locked)
	if e != nil || !locked {
		return e
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "sync:"+tid); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	var attempts int
	e = conn.QueryRow(ctx, `UPDATE operations SET status='running',attempts=attempts+1,lease_until=now()+interval '2 minutes',updated_at=now() WHERE id=$1 AND ((status IN ('pending','retrying') AND next_attempt_at<=now()) OR (status='running' AND lease_until<now())) RETURNING attempts`, oid).Scan(&attempts)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	e = a.synchronize(ctx, conn, tid, version)
	status, code, message, delay := syncResult(e, attempts)
	// The job context may have timed out; persist the result with a fresh bounded context.
	finish, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	tx, dbErr := a.DB.Begin(finish)
	if dbErr != nil {
		return dbErr
	}
	defer tx.Rollback(finish)
	tag, dbErr := tx.Exec(finish, `UPDATE operations SET status=$2,error_code=$3,error_message=$4,next_attempt_at=now()+make_interval(secs=>$5),lease_until=NULL,updated_at=now() WHERE id=$1 AND status='running'`, oid, status, code, message, delay.Seconds())
	if dbErr != nil {
		return dbErr
	}
	if tag.RowsAffected() > 0 && status == "succeeded" {
		if _, dbErr = tx.Exec(finish, `UPDATE tenants SET last_checked_at=now() WHERE id=$1 AND version=$2`, tid, version); dbErr != nil {
			return dbErr
		}
		if _, dbErr = tx.Exec(finish, `INSERT INTO audit_events(tenant_id,actor_id,action,target,detail,operation_id) VALUES($1,'worker','sync_succeeded',$2,jsonb_build_object('version',$3::bigint),$4)`, tid, tid, version, oid); dbErr != nil {
			return dbErr
		}
	}
	if dbErr = tx.Commit(finish); dbErr != nil {
		return dbErr
	}
	slog.Info("sync finished", "operation_id", oid, "tenant_id", tid, "version", version, "status", status, "error_code", code)
	return nil
}
func syncResult(err error, attempts int) (status, code, message string, delay time.Duration) {
	if err == nil {
		return "succeeded", "", "", 0
	}
	if errors.Is(err, errSuperseded) {
		return "superseded", "SUPERSEDED", "目标已被新版本替代", 0
	}
	var r *remote.Error
	if errors.As(err, &r) {
		code = r.Service + "_" + r.Code
		message = r.Error()
		if r.Status == 409 {
			return "conflict", code, "远端资源归属或有效权限存在冲突，请处理后重试", 0
		}
		if r.Status == 401 || r.Status == 403 {
			return "failed", code, "远端凭证无效或权限不足，请修复接入后重试", 0
		}
		if r.Status != 0 && r.Status != 429 && r.Status < 500 {
			return "failed", code, "远端拒绝请求，请检查身份、路径和资源配置", 0
		}
		delay = r.RetryAfter
	} else {
		code = "SYNC_ERROR"
		message = "同步未完成，可查看服务日志并重试"
	}
	if attempts >= 8 {
		return "failed", code, message, 0
	}
	if delay <= 0 {
		delay = time.Duration(1<<min(attempts, 8))*time.Second + time.Duration(rand.IntN(1000))*time.Millisecond
	}
	return "retrying", code, message, delay
}
func (a *App) currentVersion(ctx context.Context, tid string, v int64) error {
	var actual int64
	e := a.DB.QueryRow(ctx, "SELECT version FROM tenants WHERE id=$1", tid).Scan(&actual)
	if e != nil {
		return e
	}
	if actual != v {
		return errSuperseded
	}
	return nil
}
func (a *App) synchronize(ctx context.Context, conn *pgxpool.Conn, tid string, version int64) error {
	if e := a.currentVersion(ctx, tid, version); e != nil {
		return e
	}
	var name, slug, status string
	var groupID int64
	if e := a.DB.QueryRow(ctx, `SELECT name,slug,status,coalesce(gitlab_id,0) FROM tenants WHERE id=$1`, tid).Scan(&name, &slug, &status, &groupID); e != nil {
		return e
	}
	gid, e := a.GitLab.EnsureGroup(ctx, tid, slug, name, groupID)
	if e != nil {
		return e
	}
	if _, e = a.DB.Exec(ctx, "UPDATE tenants SET gitlab_id=$2 WHERE id=$1", tid, gid); e != nil {
		return e
	}
	users, e := collect(ctx, a.DB, `SELECT DISTINCT u.id,u.subject,u.username,u.name,u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.status='active' AND u.status='active'`, tid)
	if e != nil {
		return e
	}
	for _, u := range users {
		if e = a.currentVersion(ctx, tid, version); e != nil {
			return e
		}
		uid := u["id"].(string)
		// One shared identity can be provisioned by several tenants concurrently.
		if _, e = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, "identity-sync:"+uid); e != nil {
			return e
		}
		remoteID, remoteErr := a.GitLab.EnsureUser(ctx, u["subject"].(string), u["username"].(string), u["name"].(string), u["email"].(string))
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		_, unlockErr := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "identity-sync:"+uid)
		done()
		if unlockErr != nil {
			_ = conn.Conn().Close(context.Background())
			return unlockErr
		}
		if remoteErr != nil {
			return remoteErr
		}
		if _, e = a.DB.Exec(ctx, `UPDATE users SET gitlab_id=$2 WHERE id=$1`, uid, remoteID); e != nil {
			return e
		}
	}
	ps, e := collect(ctx, a.DB, `SELECT id,name,path,mode,coalesce(gitlab_id,0) AS gitlab_id FROM projects WHERE tenant_id=$1 ORDER BY id`, tid)
	if e != nil {
		return e
	}
	for _, p := range ps {
		if e = a.currentVersion(ctx, tid, version); e != nil {
			return e
		}
		pid := p["id"].(string)
		remoteID, e := a.GitLab.EnsureProject(ctx, pid, p["path"].(string), p["name"].(string), p["mode"].(string), gid, p["gitlab_id"].(int64))
		if e != nil {
			return e
		}
		if _, e = a.DB.Exec(ctx, `UPDATE projects SET gitlab_id=$2 WHERE id=$1`, pid, remoteID); e != nil {
			return e
		}
	}
	gs, e := collect(ctx, a.DB, `SELECT g.project_id,g.membership_id,g.desired_level,m.status AS member_status,u.status AS user_status,coalesce(u.gitlab_id,0) AS user_remote,p.gitlab_id AS project_remote FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN projects p ON p.id=g.project_id WHERE g.tenant_id=$1 ORDER BY g.project_id,g.membership_id`, tid)
	if e != nil {
		return e
	}
	var firstError error
	for _, g := range gs {
		if e = a.currentVersion(ctx, tid, version); e != nil {
			return e
		}
		desired := int(g["desired_level"].(int32))
		if status != "active" || g["member_status"] != "active" || g["user_status"] != "active" {
			desired = 0
		}
		uid := g["user_remote"].(int64)
		observed := 0
		var syncErr error
		if uid != 0 {
			observed, syncErr = a.GitLab.SetAccess(ctx, g["project_remote"].(int64), uid, desired)
		} else if desired != 0 {
			syncErr = fmt.Errorf("identity not provisioned")
		}
		var r *remote.Error
		if syncErr == nil || (errors.As(syncErr, &r) && r.Code == "EFFECTIVE_PERMISSION_CONFLICT") {
			if e = a.recordAccess(ctx, tid, version, g["project_id"].(string), g["membership_id"].(string), observed, syncErr == nil); e != nil {
				return e
			}
		}
		if syncErr != nil && firstError == nil {
			firstError = syncErr
		}
	}
	if e = a.currentVersion(ctx, tid, version); e != nil {
		return e
	}
	return firstError
}

// A short database transaction fences authorization updates; no remote call holds this lock.
func (a *App) recordAccess(ctx context.Context, tid string, version int64, pid, mid string, actual int, confirmed bool) error {
	tx, e := a.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "mutation:"+tid); e != nil {
		return e
	}
	var current int64
	if e = tx.QueryRow(ctx, `SELECT version FROM tenants WHERE id=$1`, tid).Scan(&current); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE grants SET observed_level=$3,checked_at=now(),effective_level=CASE WHEN $4 THEN LEAST(desired_level,$3) ELSE LEAST(effective_level,desired_level,$3) END WHERE project_id=$1 AND membership_id=$2`, pid, mid, actual, current == version && confirmed)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

package synchronization

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/FlyTOmeLight/server-go/logger"
	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

var errSuperseded = errors.New("target version superseded")

func (a *Service) Run(ctx context.Context) error {
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
				logger.Errorf(ctx, "sync worker error: %v", e)
			}
		case <-reconcile.C:
			if e := a.reconcile(ctx); e != nil && ctx.Err() == nil {
				logger.Errorf(ctx, "reconcile failed: %v", e)
			}
		}
	}
}

func (a *Service) reconcile(ctx context.Context) error {
	tx := a.DB.WithContext(ctx).Begin()
	e := tx.Error
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = table.RescheduleReconciliation(ctx, tx)
	if e != nil {
		return e
	}
	if _, e = table.PruneSessions(ctx, tx); e != nil {
		return e
	}
	if _, e = table.PruneLoginFlows(ctx, tx); e != nil {
		return e
	}

	if _, e = table.PruneIdempotency(ctx, tx); e != nil {
		return e
	}
	return tx.Commit().Error
}

func (a *Service) runNext(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	return a.DB.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		candidate, e := table.NextOperation(ctx, conn)
		oid, tid, version := candidate.ID, candidate.TenantID, candidate.Version

		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		var locked bool
		e = table.TrySessionLock(ctx, conn, "sync:"+tid).Scan(&locked)
		if e != nil || !locked {
			return e
		}
		defer func() {
			cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			if _, err := table.UnlockSession(cleanup, conn, "sync:"+tid); err != nil {
				_ = conn.Statement.ConnPool.(*sql.Conn).Raw(
					func(any) error { return driver.ErrBadConn },
				)
			}
		}()
		var attempts int
		e = table.ClaimOperation(ctx, conn, oid).
			Scan(&attempts)
		if errors.Is(e, sql.ErrNoRows) {
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
		tx := a.DB.WithContext(finish).Begin()
		dbErr := tx.Error
		if dbErr != nil {
			return dbErr
		}
		defer tx.Rollback()
		tag, dbErr := table.FinishOperation(finish, tx, oid, status, code, message, delay.Seconds())
		if dbErr != nil {
			return dbErr
		}
		if tag > 0 && status == "succeeded" {
			if _, dbErr = table.MarkTenantChecked(finish, tx, tid, version); dbErr != nil {
				return dbErr
			}
			if _, dbErr = table.InsertSyncAudit(finish, tx, tid, tid, version, oid); dbErr != nil {
				return dbErr
			}
		}
		if dbErr = tx.Commit().Error; dbErr != nil {
			return dbErr
		}
		logger.Infof(
			parent,
			"sync finished operation_id=%s tenant_id=%s version=%d status=%s error_code=%s",
			oid,
			tid,
			version,
			status,
			code,
		)
		return nil
	})
}

func syncResult(err error, attempts int) (status, code, message string, delay time.Duration) {
	if err == nil {
		return "succeeded", "", "", 0
	}
	if errors.Is(err, errSuperseded) {
		return "superseded", "SUPERSEDED", "目标已被新版本替代", 0
	}
	var r *client.Error
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

func (a *Service) currentVersion(ctx context.Context, tid string, v int64) error {
	var actual int64
	e := table.TenantVersion(ctx, a.DB, tid).Scan(&actual)
	if e != nil {
		return e
	}
	if actual != v {
		return errSuperseded
	}
	return nil
}

func (a *Service) synchronize(ctx context.Context, conn *gorm.DB, tid string, version int64) error {
	if e := a.currentVersion(ctx, tid, version); e != nil {
		return e
	}
	target, e := table.TenantSyncTarget(ctx, a.DB, tid)
	if e != nil {
		return e
	}
	name, slug, status, groupID := target.Name, target.Slug, target.Status, target.GitLabID

	gid, e := a.GitLab.EnsureGroup(ctx, tid, slug, name, groupID)
	if e != nil {
		return e
	}
	if _, e = table.SetTenantGitLabID(ctx, a.DB, tid, gid); e != nil {
		return e
	}
	users, e := table.ListSyncUsers(ctx, a.DB, tid)
	if e != nil {
		return e
	}
	for _, u := range users {
		if e = a.currentVersion(ctx, tid, version); e != nil {
			return e
		}
		uid := u["id"].(string)
		// One shared identity can be provisioned by several tenants concurrently.
		if _, e = table.LockSession(ctx, conn, "identity-sync:"+uid); e != nil {
			return e
		}
		remoteID, remoteErr := a.GitLab.EnsureUser(
			ctx,
			u["subject"].(string),
			u["username"].(string),
			u["name"].(string),
			u["email"].(string),
		)
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		_, unlockErr := table.UnlockSession(cleanup, conn, "identity-sync:"+uid)
		done()
		if unlockErr != nil {
			_ = conn.Statement.ConnPool.(*sql.Conn).Raw(func(any) error { return driver.ErrBadConn })
			return unlockErr
		}
		if remoteErr != nil {
			return remoteErr
		}
		if _, e = table.SetUserGitLabID(ctx, a.DB, uid, remoteID); e != nil {
			return e
		}
	}
	ps, e := table.ListSyncProjects(ctx, a.DB, tid)
	if e != nil {
		return e
	}
	for _, p := range ps {
		if e = a.currentVersion(ctx, tid, version); e != nil {
			return e
		}
		pid := p["id"].(string)
		remoteID, e := a.GitLab.EnsureProject(
			ctx,
			pid,
			p["path"].(string),
			p["name"].(string),
			p["mode"].(string),
			gid,
			p["gitlab_id"].(int64),
		)
		if e != nil {
			return e
		}
		if _, e = table.SetProjectGitLabID(ctx, a.DB, pid, remoteID); e != nil {
			return e
		}
	}
	gs, e := table.ListSyncGrants(ctx, a.DB, tid)
	if e != nil {
		return e
	}
	var firstError error
	for _, g := range gs {
		if e = a.currentVersion(ctx, tid, version); e != nil {
			return e
		}
		desired := int(g["desired_level"].(int64))
		if status != "active" || g["member_status"] != "active" || g["user_status"] != "active" {
			desired = 0
		}
		uid := g["user_remote"].(int64)
		observed := 0
		var syncErr error
		if uid != 0 {
			observed, syncErr = a.GitLab.SetAccess(ctx, g["project_remote"].(int64), uid, desired)
		} else if desired != 0 {
			syncErr = errors.New("identity not provisioned")
		}
		var r *client.Error
		if syncErr == nil || (errors.As(syncErr, &r) && r.Code == "EFFECTIVE_PERMISSION_CONFLICT") {
			if e = a.RecordAccess(
				ctx,
				tid,
				version,
				g["project_id"].(string),
				g["membership_id"].(string),
				observed,
				syncErr == nil,
			); e != nil {
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

func (a *Service) RecordAccess(
	ctx context.Context,
	tid string,
	version int64,
	pid, mid string,
	actual int,
	confirmed bool,
) error {
	tx := a.DB.WithContext(ctx).Begin()
	e := tx.Error
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = table.LockTransaction(ctx, tx, "mutation:"+tid); e != nil {
		return e
	}
	var current int64
	if e = table.TenantVersion(ctx, tx, tid).Scan(&current); e != nil {
		return e
	}
	_, e = table.RecordObservedAccess(ctx, tx, pid, mid, actual, current == version && confirmed)
	if e != nil {
		return e
	}
	return tx.Commit().Error
}

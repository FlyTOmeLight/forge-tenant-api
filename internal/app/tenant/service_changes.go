package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

type Change struct {
	TenantID, ProjectID, MemberID, TeamID, OperationID string
	Remove                                             bool
	Key, Version, Route, Target                        string
	Raw                                                []byte
}

type Receipt struct {
	Status int
	Data   []byte
}

type mutation func(context.Context, pgx.Tx, User, Change, Object) (Object, error)

func (a *Service) change(
	ctx context.Context,
	u User,
	action string,
	fn mutation,
	r Change,
	body Object,
) (Receipt, error) {
	tid := r.TenantID
	raw := r.Raw

	key := r.Key
	if key == "" || len(key) > 128 {
		return Receipt{}, problem(400, "IDEMPOTENCY_KEY_REQUIRED", "请提供 1–128 字符的 Idempotency-Key")
	}
	tx, e := a.DB.Begin(ctx)
	if e != nil {
		return Receipt{}, e
	}
	defer tx.Rollback(ctx)
	lock := tid
	if lock == "" {
		lock = "create-tenant"
	}
	if _, e = table.LockTransaction(ctx, tx, "mutation:"+lock); e != nil {
		return Receipt{}, e
	}
	if action == "create_tenant" || action == "update_tenant" {
		if !u.Operator {
			return Receipt{}, problem(403, "FORBIDDEN", "需要平台管理权限")
		}
	} else {
		var allowed bool
		if action == "set_grant" || action == "revoke_grant" || action == "set_team_grant" ||
			action == "revoke_team_grant" {
			allowed, e = grantManager(ctx, tx, u, tid, r.ProjectID)
		} else {
			allowed, e = canManage(ctx, tx, u, tid)
		}
		if e != nil {
			return Receipt{}, e
		}
		if !allowed {
			return Receipt{}, problem(404, "NOT_FOUND", "资源不存在或无管理权限")
		}
		if action != "retry_sync" {
			if e = requireActive(ctx, tx, tid); e != nil {
				return Receipt{}, e
			}
		}
	}
	route := r.Route
	digest := Hash(string(raw) + "|" + r.Version)
	record, e := table.FindIdempotency(ctx, tx, u.ID, key)
	oldRoute, oldHash, oldCode, encrypted := record.Route, record.Digest, record.Status, record.Response

	if e == nil {
		if oldRoute != route || oldHash != digest {
			return Receipt{}, problem(409, "IDEMPOTENCY_CONFLICT", "同一请求标识不能用于不同内容")
		}
		decoded, e := a.open(encrypted)
		if e != nil {
			return Receipt{}, e
		}
		return Receipt{Status: oldCode, Data: decoded}, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return Receipt{}, e
	}
	if tid != "" {
		var version int64
		e = table.TenantVersion(ctx, tx, tid).Scan(&version)
		if errors.Is(e, pgx.ErrNoRows) {
			return Receipt{}, problem(404, "NOT_FOUND", "企业不存在")
		}
		if e != nil {
			return Receipt{}, e
		}
		expected, er := strconv.ParseInt(strings.Trim(r.Version, "\""), 10, 64)
		if er != nil || version != expected {
			return Receipt{}, problem(412, "VERSION_MISMATCH", "数据已变化，请刷新后重试")
		}
	}
	result, e := fn(ctx, tx, u, r, body)
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && (pg.Code == "23505" || pg.Code == "23503" || pg.Code == "23514") {
			e = problem(409, "RESOURCE_CONFLICT", "资源已存在或关联关系无效")
		}
		return Receipt{}, e
	}
	if tid == "" {
		tid = result["tenant_id"].(string)
	}
	op, version, e := enqueue(ctx, tx, tid)
	if e != nil {
		return Receipt{}, e
	}
	detail := Object{}
	for k, v := range result {
		if k != "invitation_url" {
			detail[k] = v
		}
	}
	encodedDetail, e := json.Marshal(detail)
	if e != nil {
		return Receipt{}, e
	}
	if _, e = table.InsertChangeAudit(ctx, tx, tid, u.ID, action, r.Target, encodedDetail, op); e != nil {
		return Receipt{}, e
	}
	if _, e = table.InsertAccessEvent(ctx, tx, tid, version, action); e != nil {
		return Receipt{}, e
	}
	result["operation_id"] = op
	result["version"] = version
	data, e := json.Marshal(result)
	if e != nil {
		return Receipt{}, e
	}
	if _, e = table.InsertIdempotency(ctx, tx, u.ID, key, route, digest, a.seal(data)); e != nil {
		return Receipt{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Receipt{}, e
	}
	return Receipt{Status: 202, Data: data}, nil
}

func str(o Object, k string) string { s, _ := o[k].(string); return strings.TrimSpace(s) }

func validName(s string) bool { return len([]rune(s)) > 0 && len([]rune(s)) <= 80 }

func enqueue(ctx context.Context, tx pgx.Tx, tid string) (string, int64, error) {
	var v int64
	e := table.AdvanceTenantVersion(ctx, tx, tid).Scan(&v)
	if e != nil {
		return "", 0, e
	}
	_, e = table.SupersedeOperations(ctx, tx, tid)
	if e != nil {
		return "", 0, e
	}
	id := ID("op")
	_, e = table.InsertOperation(ctx, tx, id, tid, v)
	return id, v, e
}

func requireActive(ctx context.Context, tx pgx.Tx, tid string) error {
	var active bool
	e := table.TenantActive(ctx, tx, tid).Scan(&active)
	if e != nil {
		return e
	}
	if !active {
		return problem(409, "TENANT_SUSPENDED", "企业已停用，不能增加成员或权限")
	}
	return nil
}

func protectAdmin(ctx context.Context, tx pgx.Tx, tid, mid string) error {
	var role, status string
	e := table.MembershipState(ctx, tx, tid, mid).
		Scan(&role, &status)
	if errors.Is(e, pgx.ErrNoRows) {
		return problem(404, "NOT_FOUND", "成员不存在")
	}
	if e != nil {
		return e
	}
	if role == "admin" && status == "active" {
		var n int
		e = table.ActiveAdminCount(ctx, tx, tid).
			Scan(&n)
		if e != nil {
			return e
		}
		if n <= 1 {
			return problem(409, "LAST_TENANT_ADMIN", "请先指定另一位已激活的企业管理员")
		}
	}
	return nil
}

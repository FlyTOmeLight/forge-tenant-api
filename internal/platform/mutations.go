package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mutation func(context.Context, pgx.Tx, User, *http.Request, Object) (Object, error)

func str(o Object, k string) string { s, _ := o[k].(string); return strings.TrimSpace(s) }
func validName(s string) bool       { return len([]rune(s)) > 0 && len([]rune(s)) <= 80 }
func (a *App) mutate(action string, fn mutation) func(http.ResponseWriter, *http.Request, User) {
	return func(w http.ResponseWriter, r *http.Request, u User) {
		ctx := r.Context()
		tid := r.PathValue("tid")
		raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
		if e != nil {
			fail(w, problem(400, "INVALID_BODY", "请求过大"))
			return
		}
		body := Object{}
		if len(raw) > 0 {
			if e = json.Unmarshal(raw, &body); e != nil {
				fail(w, problem(400, "INVALID_JSON", "请求格式无效"))
				return
			}
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 128 {
			fail(w, problem(400, "IDEMPOTENCY_KEY_REQUIRED", "请提供 1–128 字符的 Idempotency-Key"))
			return
		}
		tx, e := a.DB.Begin(ctx)
		if e != nil {
			fail(w, e)
			return
		}
		defer tx.Rollback(ctx)
		lock := tid
		if lock == "" {
			lock = "create-tenant"
		}
		if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "mutation:"+lock); e != nil {
			fail(w, e)
			return
		}
		var admin bool
		if tid != "" {
			admin, e = canManage(ctx, tx, u, tid)
			if e != nil {
				fail(w, e)
				return
			}
			if !admin && action != "set_grant" && action != "revoke_grant" {
				fail(w, problem(404, "NOT_FOUND", "企业不存在或无管理权限"))
				return
			}
			if !admin {
				var ok bool
				e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN tenants t ON t.id=m.tenant_id WHERE g.tenant_id=$1 AND g.project_id=$2 AND m.user_id=$3 AND m.status='active' AND t.status='active' AND g.desired_level=40 AND g.effective_level>=40)`, tid, r.PathValue("pid"), u.ID).Scan(&ok)
				if e != nil {
					fail(w, e)
					return
				}
				if !ok {
					fail(w, problem(404, "NOT_FOUND", "项目不存在或无授权管理权限"))
					return
				}
			}
		} else if !u.Operator {
			fail(w, problem(403, "FORBIDDEN", "仅平台运营可创建企业"))
			return
		}
		route := r.Method + " " + r.URL.Path
		digest := Hash(string(raw) + "|" + r.Header.Get("If-Match"))
		var oldRoute, oldHash string
		var oldCode int
		var encrypted []byte
		e = tx.QueryRow(ctx, `SELECT route,body_hash,status_code,response FROM idempotency WHERE actor_id=$1 AND request_key=$2`, u.ID, key).Scan(&oldRoute, &oldHash, &oldCode, &encrypted)
		if e == nil {
			if oldRoute != route || oldHash != digest {
				fail(w, problem(409, "IDEMPOTENCY_CONFLICT", "同一请求标识不能用于不同内容"))
				return
			}
			decoded, e := a.open(encrypted)
			if e != nil {
				fail(w, e)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(oldCode)
			_, _ = w.Write(decoded)
			return
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			fail(w, e)
			return
		}
		if tid != "" {
			var version int64
			e = tx.QueryRow(ctx, "SELECT version FROM tenants WHERE id=$1", tid).Scan(&version)
			if errors.Is(e, pgx.ErrNoRows) {
				fail(w, problem(404, "NOT_FOUND", "企业不存在"))
				return
			}
			if e != nil {
				fail(w, e)
				return
			}
			expected, er := strconv.ParseInt(strings.Trim(r.Header.Get("If-Match"), "\""), 10, 64)
			if er != nil || version != expected {
				fail(w, problem(412, "VERSION_MISMATCH", "数据已变化，请刷新后重试"))
				return
			}
		}
		result, e := fn(ctx, tx, u, r, body)
		if e != nil {
			var pg *pgconn.PgError
			if errors.As(e, &pg) && (pg.Code == "23505" || pg.Code == "23503" || pg.Code == "23514") {
				e = problem(409, "RESOURCE_CONFLICT", "资源已存在或关联关系无效")
			}
			fail(w, e)
			return
		}
		if tid == "" {
			tid = result["tenant_id"].(string)
		}
		op, version, e := enqueue(ctx, tx, tid)
		if e != nil {
			fail(w, e)
			return
		}
		detail := Object{}
		for k, v := range result {
			if k != "invitation_url" {
				detail[k] = v
			}
		}
		encodedDetail, e := json.Marshal(detail)
		if e != nil {
			fail(w, e)
			return
		}
		if _, e = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,target,detail,operation_id) VALUES($1,$2,$3,$4,$5,$6)`, tid, u.ID, action, r.URL.Path, encodedDetail, op); e != nil {
			fail(w, e)
			return
		}
		if _, e = tx.Exec(ctx, `INSERT INTO access_events(tenant_id,version,reason) VALUES($1,$2,$3)`, tid, version, action); e != nil {
			fail(w, e)
			return
		}
		result["operation_id"] = op
		result["version"] = version
		data, e := json.Marshal(result)
		if e != nil {
			fail(w, e)
			return
		}
		if _, e = tx.Exec(ctx, `INSERT INTO idempotency(actor_id,request_key,route,body_hash,status_code,response) VALUES($1,$2,$3,$4,202,$5)`, u.ID, key, route, digest, a.seal(data)); e != nil {
			fail(w, e)
			return
		}
		if e = tx.Commit(ctx); e != nil {
			fail(w, e)
			return
		}
		JSON(w, 202, result)
	}
}
func enqueue(ctx context.Context, tx pgx.Tx, tid string) (string, int64, error) {
	var v int64
	e := tx.QueryRow(ctx, `UPDATE tenants SET version=version+1 WHERE id=$1 RETURNING version`, tid).Scan(&v)
	if e != nil {
		return "", 0, e
	}
	_, e = tx.Exec(ctx, `UPDATE operations SET status='superseded',updated_at=now() WHERE tenant_id=$1 AND status IN ('pending','running','retrying','failed','conflict')`, tid)
	if e != nil {
		return "", 0, e
	}
	id := ID("op")
	_, e = tx.Exec(ctx, `INSERT INTO operations(id,tenant_id,version,status) VALUES($1,$2,$3,'pending')`, id, tid, v)
	return id, v, e
}
func (a *App) createTenant(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	name, slug, admin := str(b, "name"), str(b, "slug"), str(b, "admin_user_id")
	if admin == "" {
		admin = u.ID
	}
	if !validName(name) || !regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`).MatchString(slug) {
		return nil, problem(422, "INVALID_TENANT", "企业名称必填，标识须为 2–40 位小写字母、数字或连字符")
	}
	var active bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active')`, admin).Scan(&active); e != nil {
		return nil, e
	}
	if !active {
		return nil, problem(422, "INVALID_ADMIN", "首位管理员必须是已激活账号")
	}
	tid := ID("t")
	if _, e := tx.Exec(ctx, `INSERT INTO tenants(id,name,slug,version) VALUES($1,$2,$3,0)`, tid, name, slug); e != nil {
		return nil, e
	}
	_, e := tx.Exec(ctx, `INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES($1,$2,$3,'admin','active')`, ID("m"), tid, admin)
	return Object{"tenant_id": tid, "name": name, "slug": slug, "admin_user_id": admin}, e
}
func (a *App) updateTenant(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	if !u.Operator {
		return nil, problem(403, "FORBIDDEN", "仅平台运营可启用或停用企业")
	}
	status := str(b, "status")
	if status != "active" && status != "suspended" {
		return nil, problem(422, "INVALID_STATUS", "企业状态无效")
	}
	var before string
	if e := tx.QueryRow(ctx, `SELECT status FROM tenants WHERE id=$1`, r.PathValue("tid")).Scan(&before); e != nil {
		return nil, e
	}
	_, e := tx.Exec(ctx, `UPDATE tenants SET status=$2 WHERE id=$1`, r.PathValue("tid"), status)
	if e == nil {
		_, e = tx.Exec(ctx, `UPDATE grants SET effective_level=0 WHERE tenant_id=$1`, r.PathValue("tid"))
	}
	return Object{"before": before, "status": status, "agent_execution": "integration_required"}, e
}
func requireActive(ctx context.Context, tx pgx.Tx, tid string) error {
	var active bool
	e := tx.QueryRow(ctx, `SELECT status='active' FROM tenants WHERE id=$1`, tid).Scan(&active)
	if e != nil {
		return e
	}
	if !active {
		return problem(409, "TENANT_SUSPENDED", "企业已停用，不能增加成员或权限")
	}
	return nil
}
func (a *App) invitation(ctx context.Context, tx pgx.Tx, mid string) (string, error) {
	token := Token()
	if _, e := tx.Exec(ctx, `UPDATE invitations SET consumed_at=now() WHERE membership_id=$1 AND consumed_at IS NULL`, mid); e != nil {
		return "", e
	}
	_, e := tx.Exec(ctx, `INSERT INTO invitations(id,token_hash,membership_id,expires_at) VALUES($1,$2,$3,now()+interval '24 hours')`, ID("inv"), Hash(token), mid)
	return a.Config.PublicURL + "/invite#" + token, e
}
func (a *App) addMember(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	tid := r.PathValue("tid")
	if e := requireActive(ctx, tx, tid); e != nil {
		return nil, e
	}
	name, email, role := str(b, "name"), strings.ToLower(str(b, "email")), str(b, "role")
	if role != "admin" && role != "member" {
		return nil, problem(422, "INVALID_ROLE", "企业角色无效")
	}
	addr, e := mail.ParseAddress(email)
	if e != nil || addr.Address != email || !validName(name) {
		return nil, problem(422, "INVALID_MEMBER", "请填写有效姓名和邮箱")
	}
	// Serialize the shared identity creation across tenant transactions.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "identity:"+email); e != nil {
		return nil, e
	}
	var uid, status string
	e = tx.QueryRow(ctx, `SELECT id,status FROM users WHERE email=$1`, email).Scan(&uid, &status)
	if errors.Is(e, pgx.ErrNoRows) {
		uid = ID("u")
		_, e = tx.Exec(ctx, `INSERT INTO users(id,issuer,username,name,email,status) VALUES($1,$2,$3,$4,$5,'invited')`, uid, a.Config.Issuer, "forge-"+strings.TrimPrefix(uid, "u_"), name, email)
	}
	if e != nil {
		return nil, e
	}
	if status == "disabled" {
		return nil, problem(409, "USER_DISABLED", "统一账号已禁用")
	}
	mid := ID("m")
	var existing, memberStatus string
	e = tx.QueryRow(ctx, `SELECT id,status FROM memberships WHERE tenant_id=$1 AND user_id=$2`, tid, uid).Scan(&existing, &memberStatus)
	if e == nil {
		if memberStatus != "removed" {
			return nil, problem(409, "MEMBER_EXISTS", "此账号已在企业中，请使用重新邀请或成员设置")
		}
		mid = existing
		_, e = tx.Exec(ctx, `UPDATE memberships SET status='invited',role=$2,team_id=NULL WHERE id=$1`, mid, role)
	} else if errors.Is(e, pgx.ErrNoRows) {
		_, e = tx.Exec(ctx, `INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES($1,$2,$3,$4,'invited')`, mid, tid, uid, role)
	}
	if e != nil {
		return nil, e
	}
	link, e := a.invitation(ctx, tx, mid)
	return Object{"membership_id": mid, "user_id": uid, "email": email, "role": role, "status": "invited", "invitation_url": link}, e
}
func protectAdmin(ctx context.Context, tx pgx.Tx, tid, mid string) error {
	var role, status string
	e := tx.QueryRow(ctx, `SELECT role,status FROM memberships WHERE tenant_id=$1 AND id=$2`, tid, mid).Scan(&role, &status)
	if errors.Is(e, pgx.ErrNoRows) {
		return problem(404, "NOT_FOUND", "成员不存在")
	}
	if e != nil {
		return e
	}
	if role == "admin" && status == "active" {
		var n int
		e = tx.QueryRow(ctx, `SELECT count(*) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.role='admin' AND m.status='active' AND u.status='active'`, tid).Scan(&n)
		if e != nil {
			return e
		}
		if n <= 1 {
			return problem(409, "LAST_TENANT_ADMIN", "请先指定另一位已激活的企业管理员")
		}
	}
	return nil
}
func (a *App) updateMember(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	tid, mid, role := r.PathValue("tid"), r.PathValue("mid"), str(b, "role")
	if role != "admin" && role != "member" {
		return nil, problem(422, "INVALID_ROLE", "角色无效")
	}
	if role != "admin" {
		if e := protectAdmin(ctx, tx, tid, mid); e != nil {
			return nil, e
		}
	}
	var before string
	if e := tx.QueryRow(ctx, `SELECT role FROM memberships WHERE tenant_id=$1 AND id=$2 AND status<>'removed'`, tid, mid).Scan(&before); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, problem(404, "NOT_FOUND", "成员不存在")
		}
		return nil, e
	}
	tag, e := tx.Exec(ctx, `UPDATE memberships SET role=$3,team_id=nullif($4,'') WHERE tenant_id=$1 AND id=$2`, tid, mid, role, str(b, "team_id"))
	if e != nil {
		return nil, e
	}
	if tag.RowsAffected() != 1 {
		return nil, problem(404, "NOT_FOUND", "成员不存在")
	}
	return Object{"membership_id": mid, "before_role": before, "role": role, "team_id": str(b, "team_id")}, nil
}
func (a *App) removeMember(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	tid, mid := r.PathValue("tid"), r.PathValue("mid")
	if e := protectAdmin(ctx, tx, tid, mid); e != nil {
		return nil, e
	}
	if _, e := tx.Exec(ctx, `UPDATE memberships SET status='removed' WHERE tenant_id=$1 AND id=$2`, tid, mid); e != nil {
		return nil, e
	}
	if _, e := tx.Exec(ctx, `UPDATE grants SET desired_level=0,effective_level=0 WHERE tenant_id=$1 AND membership_id=$2`, tid, mid); e != nil {
		return nil, e
	}
	_, e := tx.Exec(ctx, `UPDATE invitations SET consumed_at=now() WHERE membership_id=$1 AND consumed_at IS NULL`, mid)
	return Object{"membership_id": mid, "status": "removed", "global_account_preserved": true}, e
}
func (a *App) resendInvitation(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	tid, mid := r.PathValue("tid"), r.PathValue("mid")
	if e := requireActive(ctx, tx, tid); e != nil {
		return nil, e
	}
	var ok bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=$1 AND id=$2 AND status='invited')`, tid, mid).Scan(&ok)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, problem(409, "NOT_INVITED", "仅待接受的成员可重新邀请")
	}
	link, e := a.invitation(ctx, tx, mid)
	return Object{"membership_id": mid, "invitation_url": link}, e
}
func (a *App) createTeam(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	name := str(b, "name")
	if !validName(name) {
		return nil, problem(422, "INVALID_NAME", "团队名称必填，最长 80 字")
	}
	id := ID("team")
	_, e := tx.Exec(ctx, `INSERT INTO teams(id,tenant_id,name) VALUES($1,$2,$3)`, id, r.PathValue("tid"), name)
	return Object{"team_id": id, "name": name}, e
}
func (a *App) addProject(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	tid, name, path, mode := r.PathValue("tid"), str(b, "name"), str(b, "path"), str(b, "mode")
	if e := requireActive(ctx, tx, tid); e != nil {
		return nil, e
	}
	if !validName(name) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,79}$`).MatchString(path) || (mode != "create" && mode != "bind") {
		return nil, problem(422, "INVALID_PROJECT", "项目名称、路径或接入方式无效")
	}
	var slug string
	if e := tx.QueryRow(ctx, `SELECT slug FROM tenants WHERE id=$1`, tid).Scan(&slug); e != nil {
		return nil, e
	}
	pid := ID("p")
	_, e := tx.Exec(ctx, `INSERT INTO projects(id,tenant_id,name,path,mode) VALUES($1,$2,$3,$4,$5)`, pid, tid, name, slug+"/"+path, mode)
	return Object{"project_id": pid, "name": name, "path": slug + "/" + path, "mode": mode}, e
}
func (a *App) setGrant(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	tid, pid, mid := r.PathValue("tid"), r.PathValue("pid"), r.PathValue("mid")
	level := 0
	if r.Method != "DELETE" {
		if e := requireActive(ctx, tx, tid); e != nil {
			return nil, e
		}
		levels := map[string]int{"reviewer": 20, "developer": 30, "admin": 40}
		level = levels[str(b, "role")]
		if level == 0 {
			return nil, problem(422, "INVALID_ROLE", "项目角色无效")
		}
	}
	var exists bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id=m.user_id JOIN projects p ON p.tenant_id=m.tenant_id WHERE m.tenant_id=$1 AND m.id=$2 AND p.id=$3 AND ($4=0 OR (m.status='active' AND u.status='active')))`, tid, mid, pid, level).Scan(&exists)
	if e != nil {
		return nil, e
	}
	if !exists {
		return nil, problem(404, "NOT_FOUND", "项目或已激活成员不存在")
	}
	var before int
	e = tx.QueryRow(ctx, `SELECT desired_level FROM grants WHERE project_id=$1 AND membership_id=$2`, pid, mid).Scan(&before)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO grants(tenant_id,project_id,membership_id,desired_level) VALUES($1,$2,$3,$4) ON CONFLICT(project_id,membership_id) DO UPDATE SET desired_level=EXCLUDED.desired_level,effective_level=LEAST(grants.effective_level,EXCLUDED.desired_level)`, tid, pid, mid, level)
	return Object{"project_id": pid, "membership_id": mid, "before_level": before, "desired_level": level}, e
}
func (a *App) retryOperation(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	var ok bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operations o JOIN tenants t ON t.id=o.tenant_id WHERE o.tenant_id=$1 AND o.id=$2 AND o.version=t.version AND o.status IN ('failed','conflict'))`, r.PathValue("tid"), r.PathValue("oid")).Scan(&ok)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, problem(409, "OPERATION_NOT_RETRYABLE", "仅可重试当前版本的失败或冲突操作")
	}
	return Object{"replaces_operation_id": r.PathValue("oid")}, nil
}

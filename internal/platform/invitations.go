package platform

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type invitationInput struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (a *App) invitationInfo(w http.ResponseWriter, r *http.Request) {
	if !a.origin(r) {
		fail(w, problem(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	var in invitationInput
	if e := readJSON(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	var name, email, status, tenantName string
	e := a.DB.QueryRow(r.Context(), `SELECT u.name,u.email,u.status,t.name FROM invitations i JOIN memberships m ON m.id=i.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE i.token_hash=$1 AND i.consumed_at IS NULL AND i.expires_at>now() AND m.status='invited' AND t.status='active'`, Hash(in.Token)).Scan(&name, &email, &status, &tenantName)
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, problem(410, "INVITATION_EXPIRED", "邀请已失效，请联系企业管理员重新邀请"))
		return
	}
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, Object{"name": name, "email": email, "tenant": tenantName, "new_identity": status == "invited"})
}
func (a *App) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	if !a.origin(r) {
		fail(w, problem(403, "CSRF_REJECTED", "请求来源无效"))
		return
	}
	var in invitationInput
	if e := readJSON(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	ctx := r.Context()
	var tid string
	e := a.DB.QueryRow(ctx, `SELECT m.tenant_id FROM invitations i JOIN memberships m ON m.id=i.membership_id WHERE i.token_hash=$1`, Hash(in.Token)).Scan(&tid)
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, problem(410, "INVITATION_EXPIRED", "邀请已失效"))
		return
	}
	if e != nil {
		fail(w, e)
		return
	}
	tx, e := a.DB.Begin(ctx)
	if e != nil {
		fail(w, e)
		return
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "mutation:"+tid); e != nil {
		fail(w, e)
		return
	}
	var inviteID, mid, uid, name, email, username, status string
	e = tx.QueryRow(ctx, `SELECT i.id,m.id,u.id,u.name,u.email,u.username,u.status FROM invitations i JOIN memberships m ON m.id=i.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE i.token_hash=$1 AND i.consumed_at IS NULL AND i.expires_at>now() AND m.status='invited' AND t.status='active' FOR UPDATE OF i,u`, Hash(in.Token)).Scan(&inviteID, &mid, &uid, &name, &email, &username, &status)
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, problem(410, "INVITATION_EXPIRED", "邀请已失效或已使用"))
		return
	}
	if e != nil {
		fail(w, e)
		return
	}
	if status == "invited" {
		if len(in.Password) < 12 || len(in.Password) > 128 {
			fail(w, problem(422, "INVALID_PASSWORD", "密码需为 12–128 个字节"))
			return
		}
		identity, er := a.Casdoor.CreateUser(ctx, username, name, email, in.Password)
		in.Password = ""
		if er != nil {
			fail(w, problem(502, "IDENTITY_CREATION_FAILED", "身份创建失败，请检查身份服务后重试"))
			return
		}
		if identity == nil || identity.ID == "" || identity.Forbidden {
			fail(w, problem(409, "IDENTITY_INVALID", "身份状态无效，请联系管理员"))
			return
		}
		if _, e = tx.Exec(ctx, `UPDATE users SET subject=$2,status='active' WHERE id=$1`, uid, identity.ID); e != nil {
			fail(w, e)
			return
		}
	} else if status == "active" {
		actor, _, er := a.authenticate(r)
		if er != nil {
			fail(w, problem(401, "AUTH_REQUIRED", "请先使用受邀账号登录"))
			return
		}
		if actor.ID != uid {
			fail(w, problem(403, "INVITATION_IDENTITY_MISMATCH", "请切换到受邀的统一账号"))
			return
		}
	} else {
		fail(w, problem(403, "USER_DISABLED", "此账号已禁用"))
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE memberships SET status='active' WHERE id=$1`, mid); e != nil {
		fail(w, e)
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE invitations SET consumed_at=now() WHERE id=$1`, inviteID); e != nil {
		fail(w, e)
		return
	}
	op, version, e := enqueue(ctx, tx, tid)
	if e != nil {
		fail(w, e)
		return
	}
	if _, e = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,target,operation_id) VALUES($1,$2,'accept_invitation',$3,$4)`, tid, uid, mid, op); e != nil {
		fail(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		fail(w, e)
		return
	}
	JSON(w, 202, Object{"accepted": true, "username": username, "operation_id": op, "version": version})
}

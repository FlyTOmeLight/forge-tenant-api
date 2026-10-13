package tenant

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) invitation(ctx context.Context, tx *gorm.DB, mid string) (string, error) {
	token := Token()
	if _, e := table.InvalidateInvitations(ctx, tx, mid); e != nil {
		return "", e
	}
	_, e := table.InsertInvitation(ctx, tx, ID("inv"), Hash(token), mid)
	return a.Config.PublicURL + "/invite#" + token, e
}

func (a *Service) addMember(ctx context.Context, tx *gorm.DB, u User, r Change, b Object) (Object, error) {
	tid := r.TenantID
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
	if _, e = table.LockTransaction(ctx, tx, "identity:"+email); e != nil {
		return nil, e
	}
	var uid, status string
	e = table.UserByEmail(ctx, tx, email).Scan(&uid, &status)
	if errors.Is(e, sql.ErrNoRows) {
		uid = ID("u")
		_, e = table.InsertInvitedUser(
			ctx,
			tx,
			uid,
			a.Config.Issuer,
			"forge-"+strings.TrimPrefix(uid, "u_"),
			name,
			email,
		)
	}
	if e != nil {
		return nil, e
	}
	if status == "disabled" {
		return nil, problem(409, "USER_DISABLED", "统一账号已禁用")
	}
	mid := ID("m")
	var existing, memberStatus string
	e = table.MembershipByUser(ctx, tx, tid, uid).
		Scan(&existing, &memberStatus)
	if e == nil {
		if memberStatus != "removed" {
			return nil, problem(409, "MEMBER_EXISTS", "此账号已在企业中，请使用重新邀请或成员设置")
		}
		mid = existing
		_, e = table.ReinviteMembership(ctx, tx, mid, role)
	} else if errors.Is(e, sql.ErrNoRows) {
		_, e = table.InsertMembership(ctx, tx, mid, tid, uid, role)
	}
	if e != nil {
		return nil, e
	}
	link, e := a.invitation(ctx, tx, mid)
	return Object{
		"membership_id":  mid,
		"user_id":        uid,
		"email":          email,
		"role":           role,
		"status":         "invited",
		"invitation_url": link,
	}, e
}

func (a *Service) updateMember(ctx context.Context, tx *gorm.DB, u User, r Change, b Object) (Object, error) {
	tid, mid, role := r.TenantID, r.MemberID, str(b, "role")
	if role != "admin" && role != "member" {
		return nil, problem(422, "INVALID_ROLE", "角色无效")
	}
	if role != "admin" {
		if e := protectAdmin(ctx, tx, tid, mid); e != nil {
			return nil, e
		}
	}
	var before string
	if e := table.MembershipRole(ctx, tx, tid, mid).
		Scan(&before); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, problem(404, "NOT_FOUND", "成员不存在")
		}
		return nil, e
	}
	tag, e := table.UpdateMembershipRole(ctx, tx, tid, mid, role)
	if e != nil {
		return nil, e
	}
	if tag != 1 {
		return nil, problem(404, "NOT_FOUND", "成员不存在")
	}
	return Object{"membership_id": mid, "before_role": before, "role": role}, nil
}

func (a *Service) removeMember(ctx context.Context, tx *gorm.DB, u User, r Change, b Object) (Object, error) {
	tid, mid := r.TenantID, r.MemberID
	if e := protectAdmin(ctx, tx, tid, mid); e != nil {
		return nil, e
	}
	if _, e := table.RemoveMembership(ctx, tx, tid, mid); e != nil {
		return nil, e
	}
	if _, e := table.RemoveMemberDirectGrants(ctx, tx, tid, mid); e != nil {
		return nil, e
	}
	if _, e := table.RemoveMemberTeams(ctx, tx, tid, mid); e != nil {
		return nil, e
	}
	if e := recomputeGrants(ctx, tx, tid); e != nil {
		return nil, e
	}
	_, e := table.InvalidateInvitations(ctx, tx, mid)
	return Object{"membership_id": mid, "status": "removed", "global_account_preserved": true}, e
}

func (a *Service) resendInvitation(ctx context.Context, tx *gorm.DB, u User, r Change, b Object) (Object, error) {
	tid, mid := r.TenantID, r.MemberID
	if e := requireActive(ctx, tx, tid); e != nil {
		return nil, e
	}
	var ok bool
	e := table.InvitedMembershipExists(ctx, tx, tid, mid).
		Scan(&ok)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, problem(409, "NOT_INVITED", "仅待接受的成员可重新邀请")
	}
	link, e := a.invitation(ctx, tx, mid)
	return Object{"membership_id": mid, "invitation_url": link}, e
}

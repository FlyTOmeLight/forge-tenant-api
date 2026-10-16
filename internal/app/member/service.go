package member

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/invitation"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type Service struct {
	writes      *tenantwrite.Writer
	invitations *invitation.Service
	issuer      string
}

func New(writes *tenantwrite.Writer, invitations *invitation.Service, issuer string) *Service {
	return &Service{writes: writes, invitations: invitations, issuer: issuer}
}

type InviteMemberRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type RoleRequest struct {
	Role string `json:"role"`
}

type EmptyRequest struct{}

func (a *Service) AddMember(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req InviteMemberRequest,
) (tenantwrite.Receipt, error) {
	return a.writes.Apply(ctx, u, "invite_member", func(tx *gorm.DB) (map[string]any, error) {
		tid := r.TenantID
		name, email, role := strings.TrimSpace(
			req.Name,
		), strings.ToLower(
			strings.TrimSpace(req.Email),
		), strings.TrimSpace(
			req.Role,
		)
		if role != "admin" && role != "member" {
			return nil, apperror.New(422, "INVALID_ROLE", "企业角色无效")
		}
		addr, e := mail.ParseAddress(email)
		if e != nil || addr.Address != email || (len([]rune(name)) == 0 || len([]rune(name)) > 80) {
			return nil, apperror.New(422, "INVALID_MEMBER", "请填写有效姓名和邮箱")
		}
		// Serialize the shared identity creation across tenant transactions.
		if _, e = table.LockTransaction(ctx, tx, "identity:"+email); e != nil {
			return nil, e
		}
		var uid, status string
		e = table.UserByEmail(ctx, tx, email).Scan(&uid, &status)
		if errors.Is(e, sql.ErrNoRows) {
			uid = identity.ID("u")
			_, e = table.InsertInvitedUser(
				ctx,
				tx,
				uid,
				a.issuer,
				"forge-"+strings.TrimPrefix(uid, "u_"),
				name,
				email,
			)
		}
		if e != nil {
			return nil, e
		}
		if status == "disabled" {
			return nil, apperror.New(409, "USER_DISABLED", "统一账号已禁用")
		}
		mid := identity.ID("m")
		var existing, memberStatus string
		e = table.MembershipByUser(ctx, tx, tid, uid).
			Scan(&existing, &memberStatus)
		if e == nil {
			if memberStatus != "removed" {
				return nil, apperror.New(409, "MEMBER_EXISTS", "此账号已在企业中，请使用重新邀请或成员设置")
			}
			mid = existing
			_, e = table.ReinviteMembership(ctx, tx, mid, role)
		} else if errors.Is(e, sql.ErrNoRows) {
			_, e = table.InsertMembership(ctx, tx, mid, tid, uid, role)
		}
		if e != nil {
			return nil, e
		}
		link, e := a.invitations.Issue(ctx, tx, mid)
		return map[string]any{
			"membership_id":  mid,
			"user_id":        uid,
			"email":          email,
			"role":           role,
			"status":         "invited",
			"invitation_url": link,
		}, e
	}, r)
}

func (a *Service) UpdateMember(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req RoleRequest,
) (tenantwrite.Receipt, error) {
	return a.writes.Apply(ctx, u, "update_member", func(tx *gorm.DB) (map[string]any, error) {
		tid, mid, role := r.TenantID, r.MemberID, strings.TrimSpace(req.Role)
		if role != "admin" && role != "member" {
			return nil, apperror.New(422, "INVALID_ROLE", "角色无效")
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
				return nil, apperror.New(404, "NOT_FOUND", "成员不存在")
			}
			return nil, e
		}
		tag, e := table.UpdateMembershipRole(ctx, tx, tid, mid, role)
		if e != nil {
			return nil, e
		}
		if tag != 1 {
			return nil, apperror.New(404, "NOT_FOUND", "成员不存在")
		}
		return map[string]any{"membership_id": mid, "before_role": before, "role": role}, nil
	}, r)
}

func (a *Service) RemoveMember(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req EmptyRequest,
) (tenantwrite.Receipt, error) {
	return a.writes.Apply(ctx, u, "remove_member", func(tx *gorm.DB) (map[string]any, error) {
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
		if e := authorization.RecomputeGrants(ctx, tx, tid); e != nil {
			return nil, e
		}
		_, e := table.InvalidateInvitations(ctx, tx, mid)
		return map[string]any{"membership_id": mid, "status": "removed", "global_account_preserved": true}, e
	}, r)
}

func (a *Service) ResendInvitation(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req EmptyRequest,
) (tenantwrite.Receipt, error) {
	return a.writes.Apply(ctx, u, "resend_invitation", func(tx *gorm.DB) (map[string]any, error) {
		tid, mid := r.TenantID, r.MemberID
		var ok bool
		e := table.InvitedMembershipExists(ctx, tx, tid, mid).
			Scan(&ok)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, apperror.New(409, "NOT_INVITED", "仅待接受的成员可重新邀请")
		}
		link, e := a.invitations.Issue(ctx, tx, mid)
		return map[string]any{"membership_id": mid, "invitation_url": link}, e
	}, r)
}

func protectAdmin(ctx context.Context, tx *gorm.DB, tid, mid string) error {
	var role, status string
	e := table.MembershipState(ctx, tx, tid, mid).
		Scan(&role, &status)
	if errors.Is(e, sql.ErrNoRows) {
		return apperror.New(404, "NOT_FOUND", "成员不存在")
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
			return apperror.New(409, "LAST_TENANT_ADMIN", "请先指定另一位已激活的企业管理员")
		}
	}
	return nil
}

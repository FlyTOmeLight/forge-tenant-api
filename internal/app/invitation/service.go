package invitation

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authentication"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type Service struct {
	DB       *gorm.DB
	Config   config.Config
	Casdoor  *client.Casdoor
	Accounts *authentication.Service
}

func New(db *gorm.DB, c config.Config, casdoor *client.Casdoor, accounts *authentication.Service) *Service {
	return &Service{DB: db, Config: c, Casdoor: casdoor, Accounts: accounts}
}

type InvitationRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (a *Service) Issue(ctx context.Context, tx *gorm.DB, mid string) (string, error) {
	token := identity.Token()
	if _, e := table.InvalidateInvitations(ctx, tx, mid); e != nil {
		return "", e
	}
	_, e := table.InsertInvitation(ctx, tx, identity.ID("inv"), identity.Hash(token), mid)
	return a.Config.PublicURL + "/invite#" + token, e
}

func (a *Service) InspectInvitation(ctx context.Context, in InvitationRequest) (map[string]any, error) {
	var name, email, status, tenantName string
	e := table.InspectInvitation(ctx, a.DB, identity.Hash(in.Token)).
		Scan(&name, &email, &status, &tenantName)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apperror.New(410, "INVITATION_EXPIRED", "邀请已失效，请联系企业管理员重新邀请")
	}
	if e != nil {
		return nil, e
	}
	return map[string]any{
		"name":         name,
		"email":        email,
		"tenant":       tenantName,
		"new_identity": status == "invited",
	}, nil
}

func (a *Service) AcceptInvitation(ctx context.Context, in InvitationRequest, session string) (map[string]any, error) {
	var tid string
	e := table.InvitationTenant(ctx, a.DB, identity.Hash(in.Token)).
		Scan(&tid)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apperror.New(410, "INVITATION_EXPIRED", "邀请已失效")
	}
	if e != nil {
		return nil, e
	}
	tx := a.DB.WithContext(ctx).Begin()
	e = tx.Error
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = table.LockTransaction(ctx, tx, "mutation:"+tid); e != nil {
		return nil, e
	}
	record, e := table.LockInvitation(ctx, tx, identity.Hash(in.Token))
	inviteID, mid, uid, name, email, username, status := record.ID, record.MembershipID, record.UserID, record.Name, record.Email, record.Username, record.UserStatus

	if errors.Is(e, sql.ErrNoRows) {
		return nil, apperror.New(410, "INVITATION_EXPIRED", "邀请已失效或已使用")
	}
	if e != nil {
		return nil, e
	}
	if status == "invited" {
		if len(in.Password) < 12 || len(in.Password) > 128 {
			return nil, apperror.New(422, "INVALID_PASSWORD", "密码需为 12–128 个字节")
		}
		identity, er := a.Casdoor.CreateUser(ctx, username, name, email, in.Password)
		in.Password = ""
		if er != nil {
			return nil, apperror.New(502, "IDENTITY_CREATION_FAILED", "身份创建失败，请检查身份服务后重试")
		}
		if identity == nil || identity.ID == "" || identity.Forbidden {
			return nil, apperror.New(409, "IDENTITY_INVALID", "身份状态无效，请联系管理员")
		}
		if _, e = table.ActivateIdentity(ctx, tx, uid, identity.ID); e != nil {
			return nil, e
		}
	} else if status == "active" {
		actor, _, er := a.Accounts.Authenticate(ctx, session)
		if er != nil {
			return nil, apperror.New(401, "AUTH_REQUIRED", "请先使用受邀账号登录")
		}
		if actor.ID != uid {
			return nil, apperror.New(403, "INVITATION_IDENTITY_MISMATCH", "请切换到受邀的统一账号")
		}
	} else {
		return nil, apperror.New(403, "USER_DISABLED", "此账号已禁用")
	}
	if _, e = table.ActivateMembership(ctx, tx, mid); e != nil {
		return nil, e
	}
	if _, e = table.ConsumeInvitation(ctx, tx, inviteID); e != nil {
		return nil, e
	}
	if e = authorization.RecomputeGrants(ctx, tx, tid); e != nil {
		return nil, e
	}
	op, version, e := tenantwrite.Enqueue(ctx, tx, tid)
	if e != nil {
		return nil, e
	}
	if _, e = table.InsertInvitationAudit(ctx, tx, tid, uid, mid, op); e != nil {
		return nil, e
	}
	if e = tx.Commit().Error; e != nil {
		return nil, e
	}
	return map[string]any{"accepted": true, "username": username, "operation_id": op, "version": version}, nil
}

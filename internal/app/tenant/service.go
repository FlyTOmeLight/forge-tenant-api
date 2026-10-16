package tenant

import (
	"context"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type Service struct {
	DB     *gorm.DB
	writes *tenantwrite.Writer
}

func New(db *gorm.DB, writes *tenantwrite.Writer) *Service { return &Service{DB: db, writes: writes} }

type CreateTenantRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	AdminUserID string `json:"admin_user_id"`
}

type TenantStatusRequest struct {
	Status string `json:"status"`
}

func (a *Service) CreateTenant(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req CreateTenantRequest,
) (tenantwrite.Receipt, error) {
	return a.writes.Apply(ctx, u, "create_tenant", func(tx *gorm.DB) (map[string]any, error) {
		name, slug, admin := strings.TrimSpace(
			req.Name,
		), strings.TrimSpace(
			req.Slug,
		), strings.TrimSpace(
			req.AdminUserID,
		)
		if admin == "" {
			return nil, apperror.New(422, "ADMIN_REQUIRED", "请明确指定首位租户管理员")
		}
		if (len([]rune(name)) == 0 || len([]rune(name)) > 80) ||
			!regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`).MatchString(slug) {
			return nil, apperror.New(422, "INVALID_TENANT", "企业名称必填，标识须为 2–40 位小写字母、数字或连字符")
		}
		var active bool
		if e := table.ActiveUserExists(ctx, tx, admin).
			Scan(&active); e != nil {
			return nil, e
		}
		if !active {
			return nil, apperror.New(422, "INVALID_ADMIN", "首位管理员必须是已激活账号")
		}
		tid := identity.ID("t")
		if _, e := table.InsertTenant(ctx, tx, tid, name, slug); e != nil {
			return nil, e
		}
		_, e := table.InsertTenantAdmin(ctx, tx, identity.ID("m"), tid, admin)
		if e == nil {
			e = table.EnqueueTenantBucket(ctx, tx, tid)
		}
		return map[string]any{"tenant_id": tid, "name": name, "slug": slug, "admin_user_id": admin}, e
	}, r)
}

func (a *Service) UpdateTenant(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req TenantStatusRequest,
) (tenantwrite.Receipt, error) {
	return a.writes.Apply(ctx, u, "update_tenant", func(tx *gorm.DB) (map[string]any, error) {
		if !u.Operator {
			return nil, apperror.New(403, "FORBIDDEN", "仅平台运营可启用或停用企业")
		}
		status := strings.TrimSpace(req.Status)
		if status != "active" && status != "suspended" {
			return nil, apperror.New(422, "INVALID_STATUS", "企业状态无效")
		}
		var before string
		if e := table.TenantStatus(ctx, tx, r.TenantID).Scan(&before); e != nil {
			return nil, e
		}
		_, e := table.UpdateTenantStatus(ctx, tx, r.TenantID, status)
		if e == nil {
			_, e = table.ClearTenantEffectiveAccess(ctx, tx, r.TenantID)
		}
		return map[string]any{"before": before, "status": status, "agent_execution": "integration_required"}, e
	}, r)
}

func (a *Service) ListTenants(ctx context.Context, u identity.User) (map[string]any, error) {
	rows, e := table.ListUserTenants(ctx, a.DB, u.ID)
	if e != nil {
		return nil, e
	}
	return map[string]any{"items": rows}, nil
}

func (a *Service) ListPlatformTenants(ctx context.Context, u identity.User) (map[string]any, error) {
	if !u.Operator {
		return nil, apperror.New(403, "FORBIDDEN", "需要平台管理权限")
	}
	rows, e := table.ListPlatformTenants(ctx, a.DB)
	if e != nil {
		return nil, e
	}
	return map[string]any{"items": rows}, nil
}

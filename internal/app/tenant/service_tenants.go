package tenant

import (
	"context"
	"regexp"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) createTenant(ctx context.Context, tx *gorm.DB, u User, r Change, b Object) (Object, error) {
	name, slug, admin := str(b, "name"), str(b, "slug"), str(b, "admin_user_id")
	if admin == "" {
		return nil, problem(422, "ADMIN_REQUIRED", "请明确指定首位租户管理员")
	}
	if !validName(name) || !regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`).MatchString(slug) {
		return nil, problem(422, "INVALID_TENANT", "企业名称必填，标识须为 2–40 位小写字母、数字或连字符")
	}
	var active bool
	if e := table.ActiveUserExists(ctx, tx, admin).
		Scan(&active); e != nil {
		return nil, e
	}
	if !active {
		return nil, problem(422, "INVALID_ADMIN", "首位管理员必须是已激活账号")
	}
	tid := ID("t")
	if _, e := table.InsertTenant(ctx, tx, tid, name, slug); e != nil {
		return nil, e
	}
	_, e := table.InsertTenantAdmin(ctx, tx, ID("m"), tid, admin)
	return Object{"tenant_id": tid, "name": name, "slug": slug, "admin_user_id": admin}, e
}

func (a *Service) updateTenant(ctx context.Context, tx *gorm.DB, u User, r Change, b Object) (Object, error) {
	if !u.Operator {
		return nil, problem(403, "FORBIDDEN", "仅平台运营可启用或停用企业")
	}
	status := str(b, "status")
	if status != "active" && status != "suspended" {
		return nil, problem(422, "INVALID_STATUS", "企业状态无效")
	}
	var before string
	if e := table.TenantStatus(ctx, tx, r.TenantID).Scan(&before); e != nil {
		return nil, e
	}
	_, e := table.UpdateTenantStatus(ctx, tx, r.TenantID, status)
	if e == nil {
		_, e = table.ClearTenantEffectiveAccess(ctx, tx, r.TenantID)
	}
	return Object{"before": before, "status": status, "agent_execution": "integration_required"}, e
}

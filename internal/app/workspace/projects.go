package workspace

import (
	"context"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) AddProject(ctx context.Context, u identity.User, r Change, req ProjectRequest) (Receipt, error) {
	return a.mutate(ctx, u, "add_project", func(tx *gorm.DB) (Object, error) {
		tid, name, path, mode := r.TenantID, strings.TrimSpace(
			req.Name,
		), strings.TrimSpace(
			req.Path,
		), strings.TrimSpace(
			req.Mode,
		)
		if e := requireActive(ctx, tx, tid); e != nil {
			return nil, e
		}
		if !validName(name) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,79}$`).MatchString(path) ||
			(mode != "create" && mode != "bind") {
			return nil, apperror.New(422, "INVALID_PROJECT", "项目名称、路径或接入方式无效")
		}
		var slug string
		if e := table.TenantSlug(ctx, tx, tid).Scan(&slug); e != nil {
			return nil, e
		}
		pid := identity.ID("p")
		_, e := table.InsertProject(ctx, tx, pid, tid, name, slug+"/"+path, mode)
		return Object{"project_id": pid, "name": name, "path": slug + "/" + path, "mode": mode}, e
	}, r)
}

func (a *Service) SetGrant(ctx context.Context, u identity.User, r Change, req RoleRequest) (Receipt, error) {
	action := "set_grant"
	if r.Remove {
		action = "revoke_grant"
	}
	return a.mutate(ctx, u, action, func(tx *gorm.DB) (Object, error) {
		tid, pid, mid := r.TenantID, r.ProjectID, r.MemberID
		level := 0
		if !r.Remove {
			if e := requireActive(ctx, tx, tid); e != nil {
				return nil, e
			}
			levels := map[string]int{"reviewer": 20, "developer": 30, "admin": 40}
			level = levels[strings.TrimSpace(req.Role)]
			if level == 0 {
				return nil, apperror.New(422, "INVALID_ROLE", "项目角色无效")
			}
		}
		var exists bool
		e := table.GrantTargetExists(ctx, tx, tid, mid, pid, level).
			Scan(&exists)
		if e != nil {
			return nil, e
		}
		if !exists {
			return nil, apperror.New(404, "NOT_FOUND", "项目或已激活成员不存在")
		}
		if r.Remove {
			_, e = table.DeleteDirectGrant(ctx, tx, tid, pid, mid)
		} else {
			_, e = table.UpsertDirectGrant(ctx, tx, tid, pid, mid, level)
		}
		if e != nil {
			return nil, e
		}
		if e = authorization.RecomputeGrants(ctx, tx, tid); e != nil {
			return nil, e
		}
		return Object{
			"project_id":    pid,
			"membership_id": mid,
			"direct_role":   authorization.GrantRole(level),
		}, nil
	}, r)
}

func (a *Service) RetryOperation(ctx context.Context, u identity.User, r Change, req EmptyRequest) (Receipt, error) {
	return a.mutate(ctx, u, "retry_sync", func(tx *gorm.DB) (Object, error) {
		var ok bool
		e := table.RetryableOperationExists(ctx, tx, r.TenantID, r.OperationID).
			Scan(&ok)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, apperror.New(409, "OPERATION_NOT_RETRYABLE", "仅可重试当前版本的失败或冲突操作")
		}
		return Object{"replaces_operation_id": r.OperationID}, nil
	}, r)
}

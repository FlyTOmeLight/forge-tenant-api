package project

import (
	"context"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type Service struct {
	DB            *gorm.DB
	writes        *tenantwrite.Writer
	authorization *authorization.Service
}

func New(db *gorm.DB, writes *tenantwrite.Writer) *Service {
	return &Service{DB: db, writes: writes, authorization: authorization.New(db)}
}

type ProjectRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Mode string `json:"mode"`
}

type RoleRequest struct {
	Role string `json:"role"`
}

func (a *Service) AddProject(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req ProjectRequest,
) (tenantwrite.Receipt, error) {
	return a.writes.Apply(ctx, u, "add_project", func(tx *gorm.DB) (map[string]any, error) {
		tid, name, path, mode := r.TenantID, strings.TrimSpace(
			req.Name,
		), strings.TrimSpace(
			req.Path,
		), strings.TrimSpace(
			req.Mode,
		)
		if (len([]rune(name)) == 0 || len([]rune(name)) > 80) ||
			!regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,79}$`).MatchString(path) ||
			(mode != "create" && mode != "bind") {
			return nil, apperror.New(422, "INVALID_PROJECT", "项目名称、路径或接入方式无效")
		}
		var slug string
		if e := table.TenantSlug(ctx, tx, tid).Scan(&slug); e != nil {
			return nil, e
		}
		pid := identity.ID("p")
		_, e := table.InsertProject(ctx, tx, pid, tid, name, slug+"/"+path, mode)
		return map[string]any{"project_id": pid, "name": name, "path": slug + "/" + path, "mode": mode}, e
	}, r)
}

func (a *Service) SetGrant(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req RoleRequest,
) (tenantwrite.Receipt, error) {
	action := "set_grant"
	if r.Remove {
		action = "revoke_grant"
	}
	return a.writes.Apply(ctx, u, action, func(tx *gorm.DB) (map[string]any, error) {
		tid, pid, mid := r.TenantID, r.ProjectID, r.MemberID
		level := 0
		if !r.Remove {
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
		return map[string]any{
			"project_id":    pid,
			"membership_id": mid,
			"direct_role":   authorization.GrantRole(level),
		}, nil
	}, r)
}

func (a *Service) SetTeamGrant(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req RoleRequest,
) (tenantwrite.Receipt, error) {
	action := "set_team_grant"
	if r.Remove {
		action = "revoke_team_grant"
	}
	return a.writes.Apply(ctx, u, action, func(tx *gorm.DB) (map[string]any, error) {
		tid, pid, team := r.TenantID, r.ProjectID, r.TeamID
		level := authorization.ProjectRoleLevel(strings.TrimSpace(req.Role))
		if !r.Remove && level == 0 {
			return nil, apperror.New(422, "INVALID_ROLE", "项目角色无效")
		}
		var ok bool
		err := table.ProjectTeamExists(ctx, tx, tid, pid, team).
			Scan(&ok)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, apperror.New(404, "NOT_FOUND", "项目或团队不存在")
		}
		if r.Remove {
			_, err = table.DeleteTeamGrant(ctx, tx, tid, pid, team)
		} else {
			_, err = table.UpsertTeamGrant(ctx, tx, tid, pid, team, level)
		}
		if err != nil {
			return nil, err
		}
		if err = authorization.RecomputeGrants(ctx, tx, tid); err != nil {
			return nil, err
		}
		return map[string]any{
			"project_id": pid,
			"team_id":    team,
			"role":       authorization.GrantRole(level),
			"removed":    r.Remove,
		}, nil
	}, r)
}

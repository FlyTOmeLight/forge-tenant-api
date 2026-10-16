package authorization

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) MemberPermissions(
	ctx context.Context,
	u identity.User,
	tid, mid string,
) (*MemberPermissionsResponse, error) {
	admin, err := CanManage(ctx, a.DB, u, tid)
	if err != nil {
		return nil, err
	}
	var actorMember bool
	if err = table.MembershipExists(ctx, a.DB, tid, u.ID).
		Scan(&actorMember); err != nil {
		return nil, err
	}
	if !actorMember {
		return nil, apperror.New(404, "NOT_FOUND", "未加入该租户")
	}
	members, err := table.ListPermissionMember(ctx, a.DB, tid, mid, u.ID, admin)
	if err != nil {
		return nil, err
	}
	if len(members) != 1 {
		return nil, apperror.New(404, "NOT_FOUND", "成员不存在或不可查看")
	}
	m := members[0]
	mid = m["id"].(string)
	active := m["status"] == "active" && m["user_status"] == "active" && m["tenant_status"] == "active"
	tenantAdmin := m["role"] == "admin" && m["status"] == "active" && m["user_status"] == "active"
	sources, err := table.ListGrantSources(ctx, a.DB, tid, mid)
	if err != nil {
		return nil, err
	}
	for _, s := range sources {
		s["role"] = GrantRole(GrantLevel(s["level"]))
		delete(s, "level")
	}
	projects, err := table.ListMemberProjects(ctx, a.DB, tid, mid, tenantAdmin)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		desired := GrantLevel(p["desired_level"])
		level := min(desired, GrantLevel(p["observed_level"]), GrantLevel(p["effective_level"]))
		reason := "已生效"
		if !active {
			level = 0
			reason = "租户、成员或账号未激活"
		} else if desired == 0 {
			reason = "未分配项目代码角色"
		} else if level < desired {
			reason = "等待 GitLab 确认，暂按当前有效角色执行"
		}
		p["desiredRole"] = GrantRole(desired)
		p["effectiveRole"] = GrantRole(level)
		p["permissions"] = ProjectCapabilities(level, tenantAdmin, active)
		p["reason"] = reason
		own := []map[string]any{}
		for _, s := range sources {
			if s["project_id"] == p["id"] {
				own = append(own, s)
			}
		}
		p["sources"] = own
		delete(p, "desired_level")
		delete(p, "observed_level")
		delete(p, "effective_level")
	}
	teams, err := table.ListMemberTeams(ctx, a.DB, tid, mid)
	if err != nil {
		return nil, err
	}
	return &MemberPermissionsResponse{
		Member:            m,
		Teams:             teams,
		TenantPermissions: TenantPermissions(tenantAdmin, active),
		Projects:          projects,
	}, nil
}

type MemberPermissionsResponse struct {
	Member            map[string]any   `json:"member"`
	Teams             []map[string]any `json:"teams"`
	TenantPermissions map[string]any   `json:"tenantPermissions"`
	Projects          []map[string]any `json:"projects"`
}

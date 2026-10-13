package tenant

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) listUsers(ctx context.Context, u User) (Object, error) {
	if !u.Operator {
		return nil, problem(403, "FORBIDDEN", "需要平台运营权限")
	}
	rows, e := table.ListActiveUsers(ctx, a.DB)
	if e != nil {
		return nil, e
	}
	return Object{"items": rows}, nil
}

func (a *Service) listTenants(ctx context.Context, u User) (Object, error) {
	rows, e := table.ListUserTenants(ctx, a.DB, u.ID)
	if e != nil {
		return nil, e
	}
	return Object{"items": rows}, nil
}

func (a *Service) listPlatformTenants(ctx context.Context, u User) (Object, error) {
	if !u.Operator {
		return nil, problem(403, "FORBIDDEN", "需要平台管理权限")
	}
	rows, e := table.ListPlatformTenants(ctx, a.DB)
	if e != nil {
		return nil, e
	}
	return Object{"items": rows}, nil
}

func (a *Service) snapshot(ctx context.Context, u User, tid string) (Object, error) {
	admin, e := canManage(ctx, a.DB, u, tid)
	if e != nil {
		return nil, e
	}
	ts, e := table.VisibleTenant(ctx, a.DB, tid, admin, u.ID)
	if e != nil {
		return nil, e
	}
	if len(ts) != 1 {
		return nil, problem(404, "NOT_FOUND", "企业不存在或不可访问")
	}
	result := Object{
		"tenant":      ts[0],
		"permissions": tenantPermissions(admin, ts[0]["status"] == "active"),
		"teams":       []Object{},
		"operations":  []Object{},
		"audit":       []Object{},
	}
	queries := []struct {
		name string
		read func() ([]map[string]any, error)
	}{
		{
			name: "members",
			read: func() ([]map[string]any, error) { return table.ListSnapshotMembers(ctx, a.DB, tid, admin, u.ID) },
		},
		{
			name: "projects",
			read: func() ([]map[string]any, error) { return table.ListSnapshotProjects(ctx, a.DB, tid, admin, u.ID) },
		},
		{
			name: "grants",
			read: func() ([]map[string]any, error) { return table.ListSnapshotGrants(ctx, a.DB, tid, admin, u.ID) },
		},
	}
	if admin {
		queries = append(queries, struct {
			name string
			read func() ([]map[string]any, error)
		}{name: "teams", read: func() ([]map[string]any, error) { return table.ListSnapshotTeams(ctx, a.DB, tid) }})
		queries = append(queries, struct {
			name string
			read func() ([]map[string]any, error)
		}{name: "operations", read: func() ([]map[string]any, error) { return table.ListSnapshotOperations(ctx, a.DB, tid) }})
		queries = append(queries, struct {
			name string
			read func() ([]map[string]any, error)
		}{name: "audit", read: func() ([]map[string]any, error) { return table.ListSnapshotAudit(ctx, a.DB, tid) }})
	}

	for _, q := range queries {
		rows, e := q.read()
		if e != nil {
			return nil, e
		}
		if q.name == "projects" {
			for _, p := range rows {
				access, err := a.access(ctx, u.ID, tid, p["id"].(string))
				if err != nil {
					return nil, err
				}
				level, _ := access["level"].(int)
				p["permissions"] = projectPermissions(level, admin, ts[0]["status"] == "active")
			}
		}
		if q.name == "grants" {
			for _, g := range rows {
				g["direct_role"] = grantRole(grantLevel(g["direct_level"]))
				sources := []Object{}
				if g["direct_role"] != "none" {
					sources = append(
						sources,
						Object{
							"kind": "direct",
							"id":   g["membership_id"],
							"name": "直接分配",
							"role": g["direct_role"],
						},
					)
				}
				if teams, ok := g["team_sources"].([]any); ok {
					for _, v := range teams {
						t := v.(map[string]any)
						sources = append(
							sources,
							Object{
								"kind": "team",
								"id":   t["id"],
								"name": t["name"],
								"role": grantRole(grantLevel(t["level"])),
							},
						)
					}
				}
				g["sources"] = sources
				delete(g, "team_sources")
				delete(g, "direct_level")
				g["desired_role"] = grantRole(grantLevel(g["desired_level"]))
				g["observed_role"] = grantRole(grantLevel(g["observed_level"]))
				level := min(
					grantLevel(g["desired_level"]),
					grantLevel(g["observed_level"]),
					grantLevel(g["effective_level"]),
				)
				if ts[0]["status"] != "active" || g["member_status"] != "active" ||
					g["user_status"] != "active" {
					level = 0
				}
				g["effective_role"] = grantRole(level)
			}
		}
		result[q.name] = rows
	}
	return result, nil
}

func (a *Service) grantCandidates(ctx context.Context, u User, tid, pid string) (Object, error) {
	allowed, e := grantManager(ctx, a.DB, u, tid, pid)
	if e != nil {
		return nil, e
	}
	if !allowed {
		return nil, problem(404, "NOT_FOUND", "项目不存在或无授权管理权限")
	}
	rows, e := table.ListGrantCandidates(ctx, a.DB, tid)
	if e != nil {
		return nil, e
	}
	return Object{"items": rows}, nil
}

func (a *Service) projectPeople(ctx context.Context, u User, tid, pid string) (Object, error) {
	level, e := a.projectRead(ctx, u, tid, pid)
	if e != nil {
		return nil, e
	}
	rows, e := table.ListProjectPeople(ctx, a.DB, tid, pid)
	if e != nil {
		return nil, e
	}
	return Object{"items": rows, "canAssign": level >= 30}, nil
}

func (a *Service) projectTeamGrants(ctx context.Context, u User, tid, pid string) (Object, error) {
	ok, err := grantManager(ctx, a.DB, u, tid, pid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, problem(404, "NOT_FOUND", "没有项目授权管理权限")
	}
	rows, err := table.ListProjectTeamGrants(ctx, a.DB, tid, pid)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		row["role"] = grantRole(grantLevel(row["level"]))
		delete(row, "level")
	}
	return Object{"items": rows}, nil
}

func (a *Service) memberPermissions(ctx context.Context, u User, tid, mid string) (Object, error) {
	admin, err := canManage(ctx, a.DB, u, tid)
	if err != nil {
		return nil, err
	}
	var actorMember bool
	if err = table.MembershipExists(ctx, a.DB, tid, u.ID).
		Scan(&actorMember); err != nil {
		return nil, err
	}
	if !actorMember {
		return nil, problem(404, "NOT_FOUND", "未加入该租户")
	}
	members, err := table.ListPermissionMember(ctx, a.DB, tid, mid, u.ID, admin)
	if err != nil {
		return nil, err
	}
	if len(members) != 1 {
		return nil, problem(404, "NOT_FOUND", "成员不存在或不可查看")
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
		s["role"] = grantRole(grantLevel(s["level"]))
		delete(s, "level")
	}
	projects, err := table.ListMemberProjects(ctx, a.DB, tid, mid, tenantAdmin)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		desired := grantLevel(p["desired_level"])
		level := min(desired, grantLevel(p["observed_level"]), grantLevel(p["effective_level"]))
		reason := "已生效"
		if !active {
			level = 0
			reason = "租户、成员或账号未激活"
		} else if desired == 0 {
			reason = "未分配项目代码角色"
		} else if level < desired {
			reason = "等待 GitLab 确认，暂按当前有效角色执行"
		}
		p["desiredRole"] = grantRole(desired)
		p["effectiveRole"] = grantRole(level)
		p["permissions"] = projectPermissions(level, tenantAdmin, active)
		p["reason"] = reason
		own := []Object{}
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
	return Object{
		"member":            m,
		"teams":             teams,
		"tenantPermissions": tenantPermissions(tenantAdmin, active),
		"projects":          projects,
	}, nil
}

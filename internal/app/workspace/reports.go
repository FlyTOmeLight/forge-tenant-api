package workspace

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) ListTenants(ctx context.Context, u identity.User) (Object, error) {
	rows, e := table.ListUserTenants(ctx, a.DB, u.ID)
	if e != nil {
		return nil, e
	}
	return Object{"items": rows}, nil
}

func (a *Service) ListPlatformTenants(ctx context.Context, u identity.User) (Object, error) {
	if !u.Operator {
		return nil, apperror.New(403, "FORBIDDEN", "需要平台管理权限")
	}
	rows, e := table.ListPlatformTenants(ctx, a.DB)
	if e != nil {
		return nil, e
	}
	return Object{"items": rows}, nil
}

func (a *Service) Snapshot(ctx context.Context, u identity.User, tid string) (Object, error) {
	admin, e := authorization.CanManage(ctx, a.DB, u, tid)
	if e != nil {
		return nil, e
	}
	ts, e := table.VisibleTenant(ctx, a.DB, tid, admin, u.ID)
	if e != nil {
		return nil, e
	}
	if len(ts) != 1 {
		return nil, apperror.New(404, "NOT_FOUND", "企业不存在或不可访问")
	}
	result := Object{
		"tenant":      ts[0],
		"permissions": authorization.TenantPermissions(admin, ts[0]["status"] == "active"),
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
				access, err := a.Authorization.Access(ctx, u.ID, tid, p["id"].(string))
				if err != nil {
					return nil, err
				}
				level := access.Level
				p["permissions"] = authorization.ProjectCapabilities(
					level,
					admin,
					ts[0]["status"] == "active",
				)
			}
		}
		if q.name == "grants" {
			for _, g := range rows {
				g["direct_role"] = authorization.GrantRole(authorization.GrantLevel(g["direct_level"]))
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
								"role": authorization.GrantRole(
									authorization.GrantLevel(t["level"]),
								),
							},
						)
					}
				}
				g["sources"] = sources
				delete(g, "team_sources")
				delete(g, "direct_level")
				g["desired_role"] = authorization.GrantRole(
					authorization.GrantLevel(g["desired_level"]),
				)
				g["observed_role"] = authorization.GrantRole(
					authorization.GrantLevel(g["observed_level"]),
				)
				level := min(
					authorization.GrantLevel(g["desired_level"]),
					authorization.GrantLevel(g["observed_level"]),
					authorization.GrantLevel(g["effective_level"]),
				)
				if ts[0]["status"] != "active" || g["member_status"] != "active" ||
					g["user_status"] != "active" {
					level = 0
				}
				g["effective_role"] = authorization.GrantRole(level)
			}
		}
		result[q.name] = rows
	}
	return result, nil
}

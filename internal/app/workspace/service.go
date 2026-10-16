package workspace

import (
	"context"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

type Service struct {
	DB            *gorm.DB
	Authorization *authorization.Service
}

func New(db *gorm.DB, permissions *authorization.Service) *Service {
	return &Service{DB: db, Authorization: permissions}
}

func (a *Service) Snapshot(ctx context.Context, u identity.User, tid string) (map[string]any, error) {
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
	result := map[string]any{
		"tenant":      ts[0],
		"permissions": authorization.TenantPermissions(admin, ts[0]["status"] == "active"),
		"teams":       []map[string]any{},
		"operations":  []map[string]any{},
		"audit":       []map[string]any{},
	}
	members, err := table.ListSnapshotMembers(ctx, a.DB, tid, admin, u.ID)
	if err != nil {
		return nil, err
	}
	projects, err := table.ListSnapshotProjects(ctx, a.DB, tid, admin, u.ID)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
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
	grants, err := table.ListSnapshotGrants(ctx, a.DB, tid, admin, u.ID)
	if err != nil {
		return nil, err
	}
	for _, g := range grants {
		g["direct_role"] = authorization.GrantRole(authorization.GrantLevel(g["direct_level"]))
		sources := []map[string]any{}
		if g["direct_role"] != "none" {
			sources = append(
				sources,
				map[string]any{
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
					map[string]any{
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
	result["members"] = members
	result["projects"] = projects
	result["grants"] = grants
	if admin {
		teams, err := table.ListSnapshotTeams(ctx, a.DB, tid)
		if err != nil {
			return nil, err
		}
		operations, err := table.ListSnapshotOperations(ctx, a.DB, tid)
		if err != nil {
			return nil, err
		}
		audit, err := table.ListSnapshotAudit(ctx, a.DB, tid)
		if err != nil {
			return nil, err
		}
		result["teams"] = teams
		result["operations"] = operations
		result["audit"] = audit
	}
	return result, nil
}

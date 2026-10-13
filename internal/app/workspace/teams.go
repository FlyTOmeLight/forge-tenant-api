package workspace

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) SetTeamMember(ctx context.Context, u identity.User, r Change, req EmptyRequest) (Receipt, error) {
	action := "add_team_member"
	if r.Remove {
		action = "remove_team_member"
	}
	return a.mutate(ctx, u, action, func(tx *gorm.DB) (Object, error) {
		tid, team, mid := r.TenantID, r.TeamID, r.MemberID
		var ok bool
		err := table.TeamMemberTargetExists(ctx, tx, tid, team, mid, r.Remove).
			Scan(&ok)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, apperror.New(404, "NOT_FOUND", "团队或已激活成员不存在")
		}
		if r.Remove {
			_, err = table.DeleteTeamMember(ctx, tx, tid, team, mid)
		} else {
			_, err = table.InsertTeamMember(ctx, tx, tid, team, mid)
		}
		if err != nil {
			return nil, err
		}
		if err = authorization.RecomputeGrants(ctx, tx, tid); err != nil {
			return nil, err
		}
		return Object{"team_id": team, "membership_id": mid, "removed": r.Remove}, nil
	}, r)
}

func (a *Service) SetTeamGrant(ctx context.Context, u identity.User, r Change, req RoleRequest) (Receipt, error) {
	action := "set_team_grant"
	if r.Remove {
		action = "revoke_team_grant"
	}
	return a.mutate(ctx, u, action, func(tx *gorm.DB) (Object, error) {
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
		return Object{
			"project_id": pid,
			"team_id":    team,
			"role":       authorization.GrantRole(level),
			"removed":    r.Remove,
		}, nil
	}, r)
}

func (a *Service) CreateTeam(ctx context.Context, u identity.User, r Change, req NameRequest) (Receipt, error) {
	return a.mutate(ctx, u, "create_team", func(tx *gorm.DB) (Object, error) {
		name := strings.TrimSpace(req.Name)
		if !validName(name) {
			return nil, apperror.New(422, "INVALID_NAME", "团队名称必填，最长 80 字")
		}
		id := identity.ID("team")
		_, e := table.InsertTeam(ctx, tx, id, r.TenantID, name)
		return Object{"team_id": id, "name": name}, e
	}, r)
}

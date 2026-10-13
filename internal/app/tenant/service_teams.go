package tenant

import (
	"context"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) setTeamMember(ctx context.Context, tx *gorm.DB, u User, r Change, b Object) (Object, error) {
	tid, team, mid := r.TenantID, r.TeamID, r.MemberID
	var ok bool
	err := table.TeamMemberTargetExists(ctx, tx, tid, team, mid, r.Remove).
		Scan(&ok)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, problem(404, "NOT_FOUND", "团队或已激活成员不存在")
	}
	if r.Remove {
		_, err = table.DeleteTeamMember(ctx, tx, tid, team, mid)
	} else {
		_, err = table.InsertTeamMember(ctx, tx, tid, team, mid)
	}
	if err != nil {
		return nil, err
	}
	if err = recomputeGrants(ctx, tx, tid); err != nil {
		return nil, err
	}
	return Object{"team_id": team, "membership_id": mid, "removed": r.Remove}, nil
}

func (a *Service) setTeamGrant(ctx context.Context, tx *gorm.DB, u User, r Change, b Object) (Object, error) {
	tid, pid, team := r.TenantID, r.ProjectID, r.TeamID
	level := projectRoleLevel(str(b, "role"))
	if !r.Remove && level == 0 {
		return nil, problem(422, "INVALID_ROLE", "项目角色无效")
	}
	var ok bool
	err := table.ProjectTeamExists(ctx, tx, tid, pid, team).
		Scan(&ok)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, problem(404, "NOT_FOUND", "项目或团队不存在")
	}
	if r.Remove {
		_, err = table.DeleteTeamGrant(ctx, tx, tid, pid, team)
	} else {
		_, err = table.UpsertTeamGrant(ctx, tx, tid, pid, team, level)
	}
	if err != nil {
		return nil, err
	}
	if err = recomputeGrants(ctx, tx, tid); err != nil {
		return nil, err
	}
	return Object{"project_id": pid, "team_id": team, "role": grantRole(level), "removed": r.Remove}, nil
}

func (a *Service) createTeam(ctx context.Context, tx *gorm.DB, u User, r Change, b Object) (Object, error) {
	name := str(b, "name")
	if !validName(name) {
		return nil, problem(422, "INVALID_NAME", "团队名称必填，最长 80 字")
	}
	id := ID("team")
	_, e := table.InsertTeam(ctx, tx, id, r.TenantID, name)
	return Object{"team_id": id, "name": name}, e
}

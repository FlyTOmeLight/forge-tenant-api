package project

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) GrantCandidates(ctx context.Context, u identity.User, tid, pid string) (map[string]any, error) {
	allowed, e := authorization.GrantManager(ctx, a.DB, u, tid, pid)
	if e != nil {
		return nil, e
	}
	if !allowed {
		return nil, apperror.New(404, "NOT_FOUND", "项目不存在或无授权管理权限")
	}
	rows, e := table.ListGrantCandidates(ctx, a.DB, tid)
	if e != nil {
		return nil, e
	}
	return map[string]any{"items": rows}, nil
}

func (a *Service) ProjectPeople(ctx context.Context, u identity.User, tid, pid string) (map[string]any, error) {
	level, e := a.authorization.ProjectRead(ctx, u, tid, pid)
	if e != nil {
		return nil, e
	}
	rows, e := table.ListProjectPeople(ctx, a.DB, tid, pid)
	if e != nil {
		return nil, e
	}
	return map[string]any{"items": rows, "canAssign": level >= 30}, nil
}

func (a *Service) ProjectTeamGrants(ctx context.Context, u identity.User, tid, pid string) (map[string]any, error) {
	ok, err := authorization.GrantManager(ctx, a.DB, u, tid, pid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.New(404, "NOT_FOUND", "没有项目授权管理权限")
	}
	rows, err := table.ListProjectTeamGrants(ctx, a.DB, tid, pid)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		row["role"] = authorization.GrantRole(authorization.GrantLevel(row["level"]))
		delete(row, "level")
	}
	return map[string]any{"items": rows}, nil
}

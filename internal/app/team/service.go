package team

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type Service struct {
	writes *tenantwrite.Writer
}

func New(writes *tenantwrite.Writer) *Service { return &Service{writes: writes} }

type NameRequest struct {
	Name string `json:"name"`
}

type EmptyRequest struct{}

func (a *Service) CreateTeam(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req NameRequest,
) (tenantwrite.Receipt, error) {
	return a.writes.Apply(ctx, u, "create_team", func(tx *gorm.DB) (map[string]any, error) {
		name := strings.TrimSpace(req.Name)
		if len([]rune(name)) == 0 || len([]rune(name)) > 80 {
			return nil, apperror.New(422, "INVALID_NAME", "团队名称必填，最长 80 字")
		}
		id := identity.ID("team")
		_, e := table.InsertTeam(ctx, tx, id, r.TenantID, name)
		return map[string]any{"team_id": id, "name": name}, e
	}, r)
}

func (a *Service) SetTeamMember(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req EmptyRequest,
) (tenantwrite.Receipt, error) {
	action := "add_team_member"
	if r.Remove {
		action = "remove_team_member"
	}
	return a.writes.Apply(ctx, u, action, func(tx *gorm.DB) (map[string]any, error) {
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
		return map[string]any{"team_id": team, "membership_id": mid, "removed": r.Remove}, nil
	}, r)
}

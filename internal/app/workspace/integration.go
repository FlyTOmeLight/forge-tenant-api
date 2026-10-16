package workspace

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func (a *Service) CheckWorkspace(
	ctx context.Context,
	in tenantv1.CheckWorkspaceRequest,
) (*tenantv1.CheckWorkspaceResponse, error) {
	var active bool
	e := table.ActiveMembershipExists(ctx, a.DB, in.TenantID, in.UserID).
		Scan(&active)
	if e != nil {
		return nil, e
	}
	if !active {
		return nil, apperror.New(403, "WORKSPACE_ACCESS_REQUIRED", "需要有效的企业成员身份")
	}
	projects, e := table.ListWorkspaceProjects(ctx, a.DB, in.TenantID, in.UserID)
	if e != nil {
		return nil, e
	}
	result := &tenantv1.CheckWorkspaceResponse{
		Allowed:  true,
		CanEdit:  true,
		Projects: make([]tenantv1.WorkspaceProject, 0, len(projects)),
	}
	for _, project := range projects {
		result.Projects = append(
			result.Projects,
			tenantv1.WorkspaceProject{
				ID:    project["id"].(string),
				Name:  project["name"].(string),
				Level: authorization.GrantLevel(project["level"]),
			},
		)
	}
	return result, nil
}

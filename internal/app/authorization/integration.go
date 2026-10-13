package authorization

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func (a *Service) ResolveProject(
	ctx context.Context,
	in tenantv1.ResolveProjectRequest,
) (*tenantv1.ResolveProjectResponse, error) {
	record, e := table.ResolveProject(ctx, a.DB, in.TenantID, in.ProjectID)

	if e != nil {
		return nil, apperror.New(404, "NOT_FOUND", "项目不存在或尚未同步")
	}
	return &tenantv1.ResolveProjectResponse{
		ID:       record.ID,
		TenantID: in.TenantID,
		Path:     record.Path,
		GitLabID: record.GitLabID,
	}, nil
}

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
				Level: GrantLevel(project["level"]),
			},
		)
	}
	return result, nil
}

func (a *Service) CheckAccess(
	ctx context.Context,
	in tenantv1.CheckAccessRequest,
) (*tenantv1.CheckAccessResponse, error) {
	if in.Action != "read" && in.Action != "write" && in.Action != "manage" {
		return nil, apperror.New(422, "INVALID_ACTION", "action 必须为 read、write 或 manage")
	}
	v, e := a.Access(ctx, in.UserID, in.TenantID, in.ProjectID)
	if e != nil {
		return nil, e
	}
	switch in.Action {
	case tenantv1.AccessRead:
		v.Allowed = v.Read
	case tenantv1.AccessWrite:
		v.Allowed = v.Write
	case tenantv1.AccessManage:
		v.Allowed = v.Manage
	}
	return v, nil
}

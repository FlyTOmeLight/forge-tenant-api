package tenant

import (
	"context"
	"crypto/subtle"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func (a *Service) checkSession(
	ctx context.Context,
	in tenantv1.CheckSessionRequest,
) (*tenantv1.CheckSessionResponse, error) {
	u, csrf, e := a.authenticate(ctx, in.Session)
	if e != nil {
		return nil, e
	}
	if in.Method != "GET" && in.Method != "HEAD" {
		if in.Origin != a.Config.PublicURL || subtle.ConstantTimeCompare([]byte(in.CSRF), []byte(csrf)) != 1 {
			return nil, problem(403, "CSRF_REJECTED", "请求来源校验失败")
		}
	}
	return &tenantv1.CheckSessionResponse{ID: u.ID, Name: u.Name, Email: u.Email, Operator: u.Operator}, nil
}

func (a *Service) resolveProject(
	ctx context.Context,
	in tenantv1.ResolveProjectRequest,
) (*tenantv1.ResolveProjectResponse, error) {
	record, e := table.ResolveProject(ctx, a.DB, in.TenantID, in.ProjectID)

	if e != nil {
		return nil, problem(404, "NOT_FOUND", "项目不存在或尚未同步")
	}
	return &tenantv1.ResolveProjectResponse{
		ID:       record.ID,
		TenantID: in.TenantID,
		Path:     record.Path,
		GitLabID: record.GitLabID,
	}, nil
}

func (a *Service) checkWorkspace(
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
		return nil, problem(403, "WORKSPACE_ACCESS_REQUIRED", "需要有效的企业成员身份")
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
				Level: grantLevel(project["level"]),
			},
		)
	}
	return result, nil
}

func (a *Service) checkAccess(
	ctx context.Context,
	in tenantv1.CheckAccessRequest,
) (*tenantv1.CheckAccessResponse, error) {
	if in.Action != "read" && in.Action != "write" && in.Action != "manage" {
		return nil, problem(422, "INVALID_ACTION", "action 必须为 read、write 或 manage")
	}
	v, e := a.access(ctx, in.UserID, in.TenantID, in.ProjectID)
	if e != nil {
		return nil, e
	}
	result := &tenantv1.CheckAccessResponse{
		Allowed: v[string(in.Action)] == true,
		Level:   grantLevel(v["level"]),
		Read:    v["read"] == true,
		Write:   v["write"] == true,
		Manage:  v["manage"] == true,
	}
	result.AuthorizationVersion, _ = v["authorization_version"].(int64)
	result.TenantID, _ = v["tenant_id"].(string)
	result.ProjectID, _ = v["project_id"].(string)
	result.Reason, _ = v["reason"].(string)
	return result, nil
}

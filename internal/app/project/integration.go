package project

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
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

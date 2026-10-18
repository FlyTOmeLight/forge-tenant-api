package authentication

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func (a *Service) ResolveScope(
	ctx context.Context,
	in tenantv1.ResolveScopeRequest,
) (*tenantv1.ResolveScopeResponse, error) {
	if strings.TrimSpace(in.TenantID) == "" || in.TenantID != strings.TrimSpace(in.TenantID) ||
		(in.ProjectID != nil && (strings.TrimSpace(*in.ProjectID) == "" || *in.ProjectID != strings.TrimSpace(*in.ProjectID))) {
		return nil, apperror.New(422, "INVALID_SCOPE", "需要明确的租户和可选项目作用域")
	}
	// Require the original HTTP method; never turn an omitted write method into GET.
	if in.Method == "" || strings.IndexFunc(in.Method, func(r rune) bool { return r < 'A' || r > 'Z' }) >= 0 {
		return nil, apperror.New(422, "INVALID_SCOPE", "需要原始 HTTP 请求方法")
	}
	user, err := a.authenticateSessionProof(ctx, in.Session, in.Method, in.Origin, in.CSRF)
	if err != nil {
		return nil, err
	}
	var status string
	err = table.TenantStatus(ctx, a.DB, in.TenantID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.New(404, "SCOPE_NOT_FOUND", "租户或项目不存在")
	}
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, apperror.New(403, "SCOPE_INACTIVE", "租户当前不可用")
	}
	var member bool
	if err = table.ActiveMembershipExists(ctx, a.DB, in.TenantID, user.ID).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		return nil, apperror.New(403, "SCOPE_ACCESS_DENIED", "需要有效租户成员身份")
	}
	if in.ProjectID != nil {
		exists, err := table.ProjectScopeExists(ctx, a.DB, in.TenantID, *in.ProjectID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, apperror.New(404, "SCOPE_NOT_FOUND", "租户或项目不存在")
		}
	}
	return &tenantv1.ResolveScopeResponse{
		UserID:         user.ID,
		TenantID:       in.TenantID,
		ProjectID:      in.ProjectID,
		OwnershipState: "active",
	}, nil
}

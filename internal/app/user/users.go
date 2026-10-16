package user

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) ListUsers(ctx context.Context, u identity.User) (map[string]any, error) {
	if !u.Operator {
		return nil, apperror.New(403, "FORBIDDEN", "需要平台运营权限")
	}
	rows, e := table.ListActiveUsers(ctx, a.DB)
	if e != nil {
		return nil, e
	}
	return map[string]any{"items": rows}, nil
}

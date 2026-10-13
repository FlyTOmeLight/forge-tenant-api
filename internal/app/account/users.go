package account

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) ListUsers(ctx context.Context, u identity.User) (Object, error) {
	if !u.Operator {
		return nil, apperror.New(403, "FORBIDDEN", "需要平台运营权限")
	}
	rows, e := table.ListActiveUsers(ctx, a.DB)
	if e != nil {
		return nil, e
	}
	return Object{"items": rows}, nil
}

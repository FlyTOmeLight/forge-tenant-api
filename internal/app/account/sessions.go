package account

import (
	"context"
	"database/sql"
	"errors"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) Authenticate(ctx context.Context, session string) (identity.User, string, error) {
	var u identity.User
	if session == "" {
		return u, "", apperror.New(401, "AUTH_REQUIRED", "请先登录")
	}
	record, e := table.SessionUser(ctx, a.DB, identity.Hash(session))
	u = identity.User{
		ID:       record.ID,
		Name:     record.Name,
		Email:    record.Email,
		Operator: record.Operator,
		Subject:  record.Subject,
	}

	if errors.Is(e, sql.ErrNoRows) {
		return u, "", apperror.New(401, "AUTH_REQUIRED", "会话已过期，请重新登录")
	}
	return u, identity.Hash(session + ":csrf"), e
}

func (a *Service) Logout(ctx context.Context, session string) error {
	_, err := table.DeleteSession(ctx, a.DB, identity.Hash(session))
	return err
}

package tenant

import (
	"context"
	"database/sql"
	"errors"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) authenticate(ctx context.Context, session string) (User, string, error) {
	var u User
	if session == "" {
		return u, "", problem(401, "AUTH_REQUIRED", "请先登录")
	}
	record, e := table.SessionUser(ctx, a.DB, Hash(session))
	u = User{
		ID:       record.ID,
		Name:     record.Name,
		Email:    record.Email,
		Operator: record.Operator,
		Subject:  record.Subject,
	}

	if errors.Is(e, sql.ErrNoRows) {
		return u, "", problem(401, "AUTH_REQUIRED", "会话已过期，请重新登录")
	}
	return u, Hash(session + ":csrf"), e
}

func (a *Service) logout(ctx context.Context, session string) error {
	_, err := table.DeleteSession(ctx, a.DB, Hash(session))
	return err
}

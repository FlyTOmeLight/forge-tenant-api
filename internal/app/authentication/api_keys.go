package authentication

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) AuthenticateAPIKey(ctx context.Context, token string) (identity.User, error) {
	if !strings.HasPrefix(token, "ftk_") {
		return identity.User{}, apperror.New(401, "AUTH_REQUIRED", "API key 无效")
	}
	user, err := table.UserByCredentialHash(ctx, a.DB, identity.Hash(token))
	if errors.Is(err, sql.ErrNoRows) {
		return identity.User{}, apperror.New(401, "AUTH_REQUIRED", "API key 无效或已失效")
	}
	return user, err
}

func (a *Service) AuthenticateService(ctx context.Context, token string) (string, error) {
	if !strings.HasPrefix(token, "ftk_") {
		return "", apperror.New(401, "SERVICE_AUTH_REQUIRED", "服务身份校验失败")
	}
	service, err := table.ServiceByCredentialHash(ctx, a.DB, identity.Hash(token))
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperror.New(401, "SERVICE_AUTH_REQUIRED", "服务身份校验失败")
	}
	return service, err
}

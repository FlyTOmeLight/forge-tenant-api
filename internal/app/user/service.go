package user

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

type Service struct {
	DB      *gorm.DB
	Config  config.Config
	Casdoor *client.Casdoor
}

func New(db *gorm.DB, c config.Config, casdoor *client.Casdoor) *Service {
	return &Service{DB: db, Config: c, Casdoor: casdoor}
}

func (a *Service) Bootstrap(ctx context.Context) error {
	if a.Config.BootstrapUser == "" {
		return nil
	}
	u, e := a.Casdoor.User(ctx, a.Config.BootstrapUser)
	if e != nil {
		return e
	}
	if u == nil || u.ID == "" || u.Forbidden {
		return errors.New("bootstrap identity missing or disabled")
	}
	_, e = table.UpsertOperator(
		ctx,
		a.DB,
		identity.ID("u"),
		a.Config.Casdoor.Issuer,
		u.ID,
		u.Name,
		u.DisplayName,
		strings.ToLower(u.Email),
	)
	return e
}

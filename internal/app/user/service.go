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

// Bootstrap registers one Casdoor user as the Forge operator. The username
// comes from the bootstrap maintenance command, never from service config.
func (a *Service) Bootstrap(ctx context.Context, username string) error {
	if username == "" {
		return errors.New("bootstrap requires BOOTSTRAP_USERNAME")
	}
	u, e := a.Casdoor.User(ctx, username)
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

package authentication

import (
	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

type Service struct {
	DB       *gorm.DB
	Config   config.Config
	Casdoor  *client.Casdoor
	Identity *client.OIDC
}

func New(db *gorm.DB, c config.Config, casdoor *client.Casdoor, oidc *client.OIDC) *Service {
	return &Service{DB: db, Config: c, Casdoor: casdoor, Identity: oidc}
}

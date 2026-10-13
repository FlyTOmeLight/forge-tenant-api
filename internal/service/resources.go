package service

import (
	"context"
	"net/http"

	"github.com/FlyTOmeLight/server-go/db"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/tenant"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

// Resources constructs and owns infrastructure; tenant services borrow it.
type Resources struct {
	Config     config.Config
	Mode       string
	Tenant     *tenant.Service
	Controller *tenant.Controller
	DB         *gorm.DB
	transport  *http.Client
	database   db.Module
}

func (s *Resources) Initialize(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			_ = s.Close(context.WithoutCancel(ctx))
		}
	}()
	s.database.Config = &db.Config{
		Driver:       db.DriverPostgreSQL,
		DSN:          s.Config.DatabaseURL,
		MaxOpenConns: 12,
		MaxIdleConns: 12,
	}
	err = s.database.Initialize(ctx)
	if err != nil {
		return err
	}
	s.DB = s.database.DB
	s.DB.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	s.transport, err = client.NewHTTP(s.Config.CAFile)
	if err != nil {
		return err
	}
	c := s.Config
	casdoor := &client.Casdoor{
		URL:          c.CasdoorURL,
		ClientID:     c.CasdoorClient,
		Secret:       c.CasdoorSecret,
		Organization: c.Organization,
		HTTP:         s.transport,
	}
	gitlab := &client.GitLab{
		URL:       c.GitLabURL,
		Token:     c.GitLabToken,
		TokenFile: c.GitLabTokenFile,
		HTTP:      s.transport,
	}
	identity := &client.OIDC{HTTP: s.transport}
	if s.Mode == "api" {
		if err = identity.Initialize(ctx, c); err != nil {
			return err
		}
	}
	s.Tenant, err = tenant.New(s.DB, c, casdoor, gitlab, identity)
	if err != nil {
		return err
	}
	s.Controller = tenant.NewController(s.Tenant)
	return nil
}

func (s *Resources) Close(ctx context.Context) error {
	if s.transport != nil {
		s.transport.CloseIdleConnections()
		s.transport = nil
	}
	err := s.database.Close(ctx)
	s.DB = nil
	s.Tenant = nil
	s.Controller = nil
	return err
}

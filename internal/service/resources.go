package service

import (
	"context"
	"net/http"

	"github.com/FlyTOmeLight/server-go/db"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/account"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/synchronization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/workspace"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

// Resources constructs and owns infrastructure; tenant services borrow it.
type Resources struct {
	Config        config.Config
	Mode          string
	Accounts      *account.Service
	Workspaces    *workspace.Service
	Authorization *authorization.Service
	API           *API
	Worker        *Worker
	DB            *gorm.DB
	transport     *http.Client
	database      db.Module
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
	s.Accounts = account.New(s.DB, c, casdoor, identity)
	s.Authorization = authorization.New(s.DB)
	s.Workspaces, err = workspace.New(s.DB, c, casdoor, s.Accounts, s.Authorization)
	if err != nil {
		return err
	}
	s.API = NewAPI(s.DB, c, s.Accounts, s.Workspaces, s.Authorization)
	s.Worker = NewWorker(synchronization.New(s.DB, gitlab))
	return nil
}

func (s *Resources) Close(ctx context.Context) error {
	if s.Worker != nil {
		if err := s.Worker.Stop(ctx); err != nil {
			return err
		}
		s.Worker = nil
	}
	if s.transport != nil {
		s.transport.CloseIdleConnections()
		s.transport = nil
	}
	err := s.database.Close(ctx)
	s.DB = nil
	s.Accounts = nil
	s.Workspaces = nil
	s.Authorization = nil
	s.API = nil
	return err
}

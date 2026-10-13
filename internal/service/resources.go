package service

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/tenant"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

// Resources constructs and owns infrastructure; tenant services borrow it.
type Resources struct {
	Config     config.Config
	Mode       string
	Tenant     *tenant.Service
	Controller *tenant.Controller
	DB         *pgxpool.Pool
	transport  *http.Client
}

func (s *Resources) Initialize(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			_ = s.Close(context.WithoutCancel(ctx))
		}
	}()
	s.DB, err = table.Open(ctx, s.Config.DatabaseURL)
	if err != nil {
		return err
	}
	if err = s.DB.Ping(ctx); err != nil {
		return err
	}
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

func (s *Resources) Close(context.Context) error {
	if s.transport != nil {
		s.transport.CloseIdleConnections()
		s.transport = nil
	}
	if s.DB != nil {
		s.DB.Close()
		s.DB = nil
	}
	s.Tenant = nil
	s.Controller = nil
	return nil
}

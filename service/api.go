package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/FlyTOmeLight/server-go/host"
	core "github.com/FlyTOmeLight/server-go/pkg"
	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authentication"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/credential"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/invitation"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/member"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/project"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/synchronization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/team"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/tenant"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/user"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/workspace"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

// API owns HTTP assembly. Domain controllers own handlers, not process lifecycle.
type API struct {
	transport       *http.Client
	database        *gorm.DB
	config          config.Config
	accounts        *authentication.Controller
	users           *user.Controller
	tenants         *tenant.Controller
	members         *member.Controller
	teams           *team.Controller
	projects        *project.Controller
	invitations     *invitation.Controller
	synchronization *synchronization.Controller
	workspaces      *workspace.Controller
	authorization   *authorization.Controller
	credentials     *credential.Controller
	session         core.HandlerFunc
	identity        core.HandlerFunc
	serviceAuth     core.HandlerFunc
}

var _ host.Service = (*API)(nil)

func newAPI(database *gorm.DB, cfg config.Config, accounts *authentication.Service, writes *tenantwrite.Writer) *API {
	permissions := authorization.New(database)
	invitations := invitation.New(database, cfg, accounts.Casdoor, accounts)
	return &API{
		database: database, config: cfg,
		accounts:        authentication.NewController(accounts),
		users:           user.NewController(user.New(database, cfg, accounts.Casdoor)),
		tenants:         tenant.NewController(tenant.New(database, writes)),
		members:         member.NewController(member.New(writes, invitations, cfg.Casdoor.Issuer)),
		teams:           team.NewController(team.New(writes)),
		projects:        project.NewController(project.New(database, writes)),
		invitations:     invitation.NewController(invitations),
		workspaces:      workspace.NewController(workspace.New(database, permissions)),
		authorization:   authorization.NewController(permissions),
		synchronization: synchronization.NewController(synchronization.NewRetryService(writes)),
		credentials:     credential.NewController(credential.New(database)),
		session:         httpapi.Session(accounts.Authenticate, cfg.PublicURL),
		identity:        httpapi.Identity(accounts.Authenticate, accounts.AuthenticateAPIKey, cfg.PublicURL),
		serviceAuth:     httpapi.ServiceAuth(accounts.AuthenticateService),
	}
}

func (s *API) Start(context.Context) error { return nil }

func (s *API) Stop(context.Context) error {
	if s.transport != nil {
		s.transport.CloseIdleConnections()
	}
	return nil
}

func (s *API) readiness(c *core.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	connection, err := s.database.DB()
	if err == nil {
		err = connection.PingContext(ctx)
	}
	if err != nil {
		c.JSON(503, map[string]any{"status": "unavailable"})
		return
	}
	c.JSON(200, map[string]any{"status": "ok"})
}

// NewAPI constructs the HTTP module using a borrowed database. Stop closes its
// owned HTTP transport; the caller remains responsible for the database.
func NewAPI(ctx context.Context, database *gorm.DB, cfg Config) (*API, error) {
	if database == nil {
		return nil, errors.New("API requires a database")
	}
	c, err := cfg.Normalize()
	if err != nil {
		return nil, err
	}
	if err := c.RequireAPI(); err != nil {
		return nil, err
	}
	transport, err := client.NewHTTP(c.CAFile)
	if err != nil {
		return nil, err
	}
	oidc := &client.OIDC{HTTP: transport}
	if err := oidc.Initialize(ctx, c); err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	accounts := authentication.New(database, c, newCasdoor(c, transport), oidc)
	writes, err := tenantwrite.New(database, c.EncryptionKey)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	api := newAPI(database, c, accounts, writes)
	api.transport = transport
	return api, nil
}

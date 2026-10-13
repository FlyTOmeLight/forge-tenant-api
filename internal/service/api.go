package service

import (
	"context"
	"time"

	"github.com/FlyTOmeLight/server-go/conf"
	"github.com/FlyTOmeLight/server-go/host"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/account"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/workspace"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

// API owns HTTP assembly. Domain controllers own handlers, not process lifecycle.
type API struct {
	database      *gorm.DB
	config        config.Config
	accounts      *account.Controller
	workspaces    *workspace.Controller
	authorization *authorization.Controller
	session       gin.HandlerFunc
}

var _ host.Service = (*API)(nil)

func NewAPI(
	database *gorm.DB,
	cfg config.Config,
	accounts *account.Service,
	workspaces *workspace.Service,
	permissions *authorization.Service,
) *API {
	return &API{
		database:      database,
		config:        cfg,
		accounts:      account.NewController(accounts),
		workspaces:    workspace.NewController(workspaces),
		authorization: authorization.NewController(permissions),
		session:       httpapi.Session(accounts.Authenticate, cfg.PublicURL),
	}
}

func (s *API) SetupHTTP(engine *gin.Engine, _ *conf.Config) error {
	httpapi.Install(engine)
	return nil
}

func (s *API) Start(context.Context) error { return nil }

func (s *API) Stop(context.Context) error { return nil }

func (s *API) readiness(c *gin.Context) {
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

package service

import (
	"context"
	"net/http"

	"github.com/FlyTOmeLight/server-go/app"
	"github.com/FlyTOmeLight/server-go/db"
	gormlogger "gorm.io/gorm/logger"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

// Config mirrors the Forge, Gitlab and Casdoor TOML sections. The public aliases
// let combined applications construct it without importing private packages.
type Config = config.File

type ForgeConfig = config.Config

type GitlabConfig = config.GitlabConfig

type CasdoorConfig = config.CasdoorConfig

type StorageConfig = config.StorageConfig

// Build owns the standalone database and transfers the HTTP module to Run.
// Combined applications call NewAPI with their shared database instead.
func Build(ctx context.Context, runtime *app.Runtime, cfg *Config) error {
	c, err := cfg.Normalize()
	if err != nil {
		return err
	}
	database := newDatabase(c.DatabaseURL)
	if err := runtime.Initialize(ctx, database); err != nil {
		return err
	}
	database.DB.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	api, err := NewAPI(ctx, database.DB, *cfg)
	if err != nil {
		return err
	}
	return runtime.AddService(api)
}

func newDatabase(dsn string) *db.Module {
	return &db.Module{Config: &db.Config{
		Driver: db.DriverPostgreSQL, DSN: dsn, MaxOpenConns: 12, MaxIdleConns: 12,
	}}
}

func newCasdoor(c config.Config, transport *http.Client) *client.Casdoor {
	return &client.Casdoor{
		URL: c.Casdoor.URL, ClientID: c.Casdoor.ClientID,
		Secret: c.Casdoor.Secret, Organization: c.Casdoor.Organization, HTTP: transport,
	}
}

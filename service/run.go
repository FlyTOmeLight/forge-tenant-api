package service

import (
	"context"
	"errors"
	"strings"
	"time"

	server "github.com/FlyTOmeLight/server-go"
	"github.com/FlyTOmeLight/server-go/app"
	"github.com/FlyTOmeLight/server-go/conf"
	"github.com/FlyTOmeLight/server-go/logger"
	core "github.com/FlyTOmeLight/server-go/pkg"
	gormlogger "gorm.io/gorm/logger"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/user"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

// Run preserves the deployed api/worker/migrate/bootstrap command contract.
// Only api starts HTTP; maintenance commands and the worker own no listener.
func Run(ctx context.Context, mode string) error { return run(ctx, mode, "") }

// RunBucketMaintenance exposes explicit database maintenance without starting HTTP.
func RunBucketMaintenance(ctx context.Context, mode, tenantID string) error {
	if mode != "bucket-backfill" && mode != "bucket-retry" {
		return errors.New("unknown bucket maintenance mode")
	}
	if mode == "bucket-retry" && strings.TrimSpace(tenantID) == "" {
		return errors.New("bucket-retry requires a tenant ID")
	}
	return run(ctx, mode, tenantID)
}

func run(ctx context.Context, mode, tenantID string) (result error) {
	if mode != "api" && mode != "worker" && mode != "migrate" && mode != "bootstrap" && mode != "bucket-backfill" &&
		mode != "bucket-retry" {
		return errors.New(
			"mode must be api, worker, migrate, bootstrap, bucket-backfill, bucket-retry or gitlab-credential-status",
		)
	}
	snapshot, err := conf.Load("")
	if err != nil {
		return err
	}
	var logConfig logger.Config
	if err := snapshot.UnmarshalKey("Log", &logConfig); err != nil {
		return err
	}
	if mode == "api" {
		// app.Run v0.9.0 does not read [Log]. Pass the existing log configuration
		// explicitly while leaving business config decoding and ownership to Run.
		return app.Run(ctx, app.Options{
			LogConfig: &logConfig, Setup: SetupHTTP,
			InitTimeout: 60 * time.Second, CloseTimeout: 5 * time.Second,
		}, Build)
	}
	var cfg Config
	if err := snapshot.Unmarshal(&cfg); err != nil {
		return err
	}
	c, err := cfg.Normalize()
	if err != nil {
		return err
	}
	log, err := logger.NewLogger(&logConfig)
	if err != nil {
		return err
	}
	defer log.Close()
	ctx = logger.WithContext(ctx, log)
	database := newDatabase(c.DatabaseURL)
	resources := server.NewBootstrap(database)
	releaseDatabase := true
	defer func() {
		if !releaseDatabase {
			return
		}
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		result = errors.Join(result, resources.Close(closeCtx))
	}()
	startup, cancel := context.WithTimeout(ctx, 60*time.Second)
	err = resources.Initialize(startup)
	cancel()
	if err != nil {
		return err
	}
	database.DB.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	if mode == "worker" {
		worker, err := NewWorker(database.DB, cfg)
		if err != nil {
			return err
		}
		err = worker.Run(ctx)
		select {
		case <-worker.done:
		default:
			// A timed-out stop is not confirmation that the worker has exited.
			// Keep its database open until the process exits.
			releaseDatabase = false
		}
		return err
	}
	operation, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if mode == "bucket-backfill" {
		_, err := table.BackfillTenantBuckets(operation, database.DB)
		return err
	}
	if mode == "bucket-retry" {
		changed, err := table.RetryFailedTenantBucket(operation, database.DB, tenantID)
		if err != nil {
			return err
		}
		if !changed {
			return errors.New("tenant bucket provision is not failed or does not exist")
		}
		return nil
	}
	if mode == "migrate" {
		return table.Migrate(operation, database.DB)
	}
	transport, err := client.NewHTTP(c.CAFile)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	return user.New(database.DB, c, newCasdoor(c, transport)).Bootstrap(operation)
}

// SetupHTTP installs Forge's existing response contract before routes are built.
func SetupHTTP(engine *core.Engine, _ *conf.Config) error {
	httpapi.Install(engine)
	return nil
}

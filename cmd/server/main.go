package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	server "github.com/FlyTOmeLight/server-go"
	"github.com/FlyTOmeLight/server-go/conf"
	"github.com/FlyTOmeLight/server-go/host"
	"github.com/FlyTOmeLight/server-go/logger"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/service"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (result error) {
	mode := "api"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "gitlab-credential-status" {
		return credentialStatus()
	}
	if mode != "api" && mode != "worker" && mode != "migrate" && mode != "bootstrap" {
		return errors.New("mode must be api, worker, migrate, bootstrap or gitlab-credential-status")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	manager, err := conf.NewManager("")
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result = errors.Join(result, manager.Close(closeCtx))
	}()
	cfg, err := config.Load(manager.Snapshot())
	if err != nil {
		return err
	}
	var logConfig logger.Config
	if err = manager.Snapshot().UnmarshalKey("Log", &logConfig); err != nil {
		return err
	}
	log, err := logger.NewLogger(&logConfig)
	if err != nil {
		return err
	}
	defer log.Close()
	ctx = logger.WithContext(ctx, log)
	resources := &service.Resources{Config: cfg, Mode: mode}
	bootstrap := server.NewBootstrap(resources)
	startup, cancel := context.WithTimeout(ctx, 60*time.Second)
	err = bootstrap.Initialize(startup)
	cancel()
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result = errors.Join(result, bootstrap.Close(closeCtx))
	}()

	switch mode {
	case "migrate":
		operation, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		return table.Migrate(operation, resources.DB)
	case "bootstrap":
		operation, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		return resources.Accounts.Bootstrap(operation)
	case "worker":
		return resources.Worker.Run(ctx)
	default:
		h, err := host.NewHTTPHost(manager, log, resources.API.SetupHTTP, resources.API)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, h.Close()) }()
		return h.Run(ctx)
	}
}

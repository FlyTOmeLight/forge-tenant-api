package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/platform"
)

func main() {
	if err := run(); err != nil {
		slog.Error("forge stopped", "error", err.Error())
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	app, err := platform.New(startup, cfg)
	if err != nil {
		return err
	}
	defer app.DB.Close()
	mode := "api"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "migrate" {
		return app.Migrate(startup)
	}
	if err = app.DB.Ping(startup); err != nil {
		return err
	}
	switch mode {
	case "bootstrap":
		return app.Bootstrap(startup)
	case "worker":
		return app.RunWorker(ctx)
	case "api":
		if err = app.InitAuth(startup); err != nil {
			return err
		}
		server := &http.Server{Addr: cfg.Listen, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second}
		done := make(chan error, 1)
		go func() { done <- server.ListenAndServe() }()
		slog.Info("forge API ready", "listen", cfg.Listen)
		select {
		case err = <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdown, close := context.WithTimeout(context.Background(), 15*time.Second)
			defer close()
			err = server.Shutdown(shutdown)
			if err != nil {
				_ = server.Close()
			}
			<-done
			return err
		}
	default:
		return errors.New("mode must be api, worker, migrate or bootstrap")
	}
}

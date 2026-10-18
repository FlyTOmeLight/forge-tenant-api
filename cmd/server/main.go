package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/FlyTOmeLight/forge-tenant-api/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mode := "api"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if mode == "gitlab-credential-status" {
		return service.CredentialStatus(ctx, os.Stdout)
	}
	if mode == "service-credential" {
		return service.ServiceCredentialAdmin(ctx, os.Stdout, os.Args[2:])
	}
	if mode == "bucket-backfill" {
		return service.RunBucketMaintenance(ctx, mode, "")
	}
	if mode == "bucket-retry" {
		if len(os.Args) != 3 {
			return errors.New("usage: server bucket-retry TENANT_ID")
		}
		return service.RunBucketMaintenance(ctx, mode, os.Args[2])
	}
	return service.Run(ctx, mode)
}

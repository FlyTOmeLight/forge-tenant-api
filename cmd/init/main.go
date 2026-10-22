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
	if len(os.Args) < 2 {
		return errors.New("usage: /forge-init external-bootstrap|wait-external")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch os.Args[1] {
	case "external-bootstrap":
		return service.ExternalBootstrap(ctx, os.Args[2:])
	case "wait-external":
		return service.WaitExternal(ctx, os.Args[2:])
	default:
		return errors.New("usage: /forge-init external-bootstrap|wait-external")
	}
}

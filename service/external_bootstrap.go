package service

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/bootstrap"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
)

// ExternalBootstrap is an ordinary Job command; no database or shell is required.
func ExternalBootstrap(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("external-bootstrap", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configFile := flags.String("config", "", "public bootstrap JSON configuration")
	if err := flags.Parse(args); err != nil {
		return errors.New("usage: /forge external-bootstrap --config FILE")
	}
	if *configFile == "" || flags.NArg() != 0 {
		return errors.New("usage: /forge external-bootstrap --config FILE")
	}
	f, err := os.Open(*configFile)
	if err != nil {
		return errors.New("cannot open bootstrap config")
	}
	defer f.Close()
	c := bootstrap.Config{TimeoutSeconds: 300}
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return errors.New("invalid bootstrap config")
	}
	if err := c.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSeconds)*time.Second)
	defer cancel()
	transport, err := client.NewHTTP(c.CAFile)
	if err != nil {
		return errors.New("cannot load bootstrap CA")
	}
	defer transport.CloseIdleConnections()
	store, err := bootstrap.InClusterStore(c.Namespace, c.OutputSecret)
	if err != nil {
		return err
	}
	defer store.HTTP.CloseIdleConnections()
	return bootstrap.Run(ctx, c, store, transport)
}

// WaitExternal reads a projected Secret directory on every attempt, never subPath
// or environment values captured when Kubernetes created the init container.
func WaitExternal(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("wait-external", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("directory", "/external", "projected output Secret directory")
	timeout := flags.Duration("timeout", 5*time.Minute, "maximum wait")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *timeout <= 0 || *timeout > 10*time.Minute {
		return errors.New("usage: /forge wait-external --directory DIR --timeout 5m (maximum 10m)")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		if _, err := readExternal(*directory); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("timed out waiting for complete external credentials")
		case <-timer.C:
		}
	}
}

func readExternal(directory string) (map[string]string, error) {
	// Resolve Kubernetes' current ..data symlink once so marker and fields come
	// from one atomic projection. Plain directories are also useful for CLI use.
	snapshot := directory
	if resolved, err := filepath.EvalSymlinks(filepath.Join(directory, "..data")); err == nil {
		snapshot = resolved
	}
	complete, err := bootstrap.ReadCredential(snapshot, "BOOTSTRAP_COMPLETE")
	if err != nil || complete != "true" {
		return nil, errors.New("external bootstrap is incomplete")
	}
	values := map[string]string{}
	for _, key := range bootstrap.RequiredKeys {
		value, err := bootstrap.ReadCredential(snapshot, key)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

// LoadExternalEnvironment is used only by the bootstrap process, before config
// loading. Main containers obtain the completed values through envFrom.
func LoadExternalEnvironment(directory string) error {
	if strings.TrimSpace(directory) == "" {
		return nil
	}
	values, err := readExternal(directory)
	if err != nil {
		return err
	}
	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

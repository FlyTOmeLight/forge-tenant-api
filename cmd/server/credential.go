package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/FlyTOmeLight/server-go/conf"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

// Invoked only through the platform's authenticated Kubernetes exec. It emits
// credential metadata, never token values, using the same client as the service.
func credentialStatus() (result error) {
	manager, err := conf.NewManager("")
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Close(ctx); result == nil {
			result = err
		}
	}()
	var gitlab config.GitlabConfig
	if err := manager.Snapshot().UnmarshalKey("Gitlab", &gitlab); err != nil {
		return err
	}
	var forge struct{ CAFile string }
	if err := manager.Snapshot().UnmarshalKey("Forge", &forge); err != nil {
		return err
	}
	transport, err := client.NewHTTP(forge.CAFile)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	transport.Timeout = 15 * time.Second

	g := client.GitLab{
		URL:       gitlab.URL,
		Token:     gitlab.Token,
		TokenFile: gitlab.TokenFile,
		HTTP:      transport,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, err := g.CredentialStatus(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(status)
}

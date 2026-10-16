package service

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/FlyTOmeLight/server-go/conf"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

// CredentialStatus is invoked through the platform's authenticated Kubernetes exec. It emits
// credential metadata, never token values, using the same client as the service.
func CredentialStatus(ctx context.Context, output io.Writer) error {
	snapshot, err := conf.Load("")
	if err != nil {
		return err
	}
	var gitlab config.GitlabConfig
	if err := snapshot.UnmarshalKey("Gitlab", &gitlab); err != nil {
		return err
	}
	var forge struct{ CAFile string }
	if err := snapshot.UnmarshalKey("Forge", &forge); err != nil {
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
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	status, err := g.CredentialStatus(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(status)
}

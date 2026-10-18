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
	c, err := config.Load(snapshot)
	if err != nil {
		return err
	}
	transport, err := client.NewHTTP(c.CAFile)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	transport.Timeout = 15 * time.Second

	g := client.GitLab{
		URL:       c.Gitlab.URL,
		Token:     c.Gitlab.Token,
		TokenFile: c.Gitlab.TokenFile,
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

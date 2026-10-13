package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
)

// Invoked only through the platform's authenticated Kubernetes exec. It emits
// credential metadata, never token values, using the same client as the service.
func credentialStatus() error {
	transport, err := client.NewHTTP(os.Getenv("CA_FILE"))
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	transport.Timeout = 15 * time.Second

	g := client.GitLab{
		URL:       os.Getenv("GITLAB_URL"),
		Token:     os.Getenv("GITLAB_TOKEN"),
		TokenFile: os.Getenv("GITLAB_TOKEN_FILE"),
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

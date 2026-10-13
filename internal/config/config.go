package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/FlyTOmeLight/server-go/conf"
)

type Config struct{ DatabaseURL, PublicURL, Issuer, ClientID, ClientSecret, CasdoorURL, CasdoorClient, CasdoorSecret, Organization, GitLabURL, GitLabPublicURL, GitLabClientID, GitLabToken, GitLabTokenFile, BootstrapUser, AppKey, ServiceToken, CAFile, ModelURL string }

func Load(snapshot *conf.Config) (Config, error) {
	var c Config
	if err := snapshot.UnmarshalKey("Forge", &c); err != nil {
		return c, err
	}
	for _, value := range []*string{&c.PublicURL, &c.CasdoorURL, &c.GitLabURL, &c.ModelURL, &c.GitLabPublicURL} {
		*value = strings.TrimRight(*value, "/")
	}
	if c.GitLabPublicURL == "" {
		c.GitLabPublicURL = c.GitLabURL
	}

	for name, value := range map[string]string{"DATABASE_URL": c.DatabaseURL, "PUBLIC_URL": c.PublicURL, "OIDC_ISSUER": c.Issuer, "OIDC_CLIENT_ID": c.ClientID, "OIDC_CLIENT_SECRET": c.ClientSecret, "CASDOOR_URL": c.CasdoorURL, "CASDOOR_CLIENT_ID": c.CasdoorClient, "CASDOOR_SECRET": c.CasdoorSecret, "CASDOOR_ORGANIZATION": c.Organization, "GITLAB_URL": c.GitLabURL, "APP_KEY": c.AppKey, "SERVICE_TOKEN": c.ServiceToken} {
		if value == "" {
			return c, fmt.Errorf("missing %s", name)
		}
	}
	if c.GitLabToken == "" && c.GitLabTokenFile == "" {
		return c, errors.New("GITLAB_TOKEN or GITLAB_TOKEN_FILE is required")
	}
	u, e := url.Parse(c.PublicURL)
	if e != nil || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return c, errors.New("PUBLIC_URL must be an HTTPS origin or loopback HTTP origin")
	}
	if len(c.ServiceToken) < 32 {
		return c, errors.New("SERVICE_TOKEN must contain at least 32 characters")
	}
	return c, nil
}

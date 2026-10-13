package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/FlyTOmeLight/server-go/conf"
)

type Config struct {
	DatabaseURL, PublicURL, BootstrapUser, AppKey, ServiceToken, CAFile, ModelURL string
	Gitlab                                                                        GitlabConfig
	Casdoor                                                                       CasdoorConfig
}

type GitlabConfig struct {
	URL, PublicURL, ClientID, Token, TokenFile string
}

type CasdoorConfig struct {
	URL, ClientID, Secret, Organization    string
	Issuer, OIDCClientID, OIDCClientSecret string
}

func Load(snapshot *conf.Config) (Config, error) {
	var c Config
	if err := snapshot.UnmarshalKey("Forge", &c); err != nil {
		return c, err
	}
	if err := snapshot.UnmarshalKey("Gitlab", &c.Gitlab); err != nil {
		return c, err
	}
	if err := snapshot.UnmarshalKey("Casdoor", &c.Casdoor); err != nil {
		return c, err
	}
	for _, value := range []*string{&c.PublicURL, &c.Casdoor.URL, &c.Gitlab.URL, &c.ModelURL, &c.Gitlab.PublicURL} {
		*value = strings.TrimRight(*value, "/")
	}
	if c.Gitlab.PublicURL == "" {
		c.Gitlab.PublicURL = c.Gitlab.URL
	}

	for name, value := range map[string]string{"DATABASE_URL": c.DatabaseURL, "PUBLIC_URL": c.PublicURL, "OIDC_ISSUER": c.Casdoor.Issuer, "OIDC_CLIENT_ID": c.Casdoor.OIDCClientID, "OIDC_CLIENT_SECRET": c.Casdoor.OIDCClientSecret, "CASDOOR_URL": c.Casdoor.URL, "CASDOOR_CLIENT_ID": c.Casdoor.ClientID, "CASDOOR_SECRET": c.Casdoor.Secret, "CASDOOR_ORGANIZATION": c.Casdoor.Organization, "GITLAB_URL": c.Gitlab.URL, "APP_KEY": c.AppKey, "SERVICE_TOKEN": c.ServiceToken} {
		if value == "" {
			return c, fmt.Errorf("missing %s", name)
		}
	}
	if c.Gitlab.Token == "" && c.Gitlab.TokenFile == "" {
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

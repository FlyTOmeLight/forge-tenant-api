package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/FlyTOmeLight/server-go/conf"
)

type Config struct {
	DatabaseURL, PublicURL, BootstrapUser, AppKey, ServiceToken, CAFile, ModelURL string
	Storage                                                                       StorageConfig
	Gitlab                                                                        GitlabConfig
	Casdoor                                                                       CasdoorConfig
}

type StorageConfig struct {
	URL, ManagementToken string
	Timeout              time.Duration
}

type GitlabConfig struct {
	URL, PublicURL, ClientID, Token, TokenFile string
}

type CasdoorConfig struct {
	URL, ClientID, Secret, Organization    string
	Issuer, OIDCClientID, OIDCClientSecret string
}

// File mirrors the existing top-level TOML sections.
type File struct {
	Forge   Config
	Gitlab  GitlabConfig
	Casdoor CasdoorConfig
	Storage StorageConfig
}

func Load(snapshot *conf.Config) (Config, error) {
	var file File
	if err := snapshot.Unmarshal(&file); err != nil {
		return Config{}, err
	}
	return file.Normalize()
}

// Normalize validates a copy, leaving caller-owned configuration unchanged.
func (file File) Normalize() (Config, error) {
	c := file.Forge
	c.Gitlab = file.Gitlab
	c.Casdoor = file.Casdoor
	c.Storage = file.Storage
	c.Storage.URL = strings.TrimRight(c.Storage.URL, "/")
	if c.Storage.URL != "" || c.Storage.ManagementToken != "" {
		u, err := url.Parse(c.Storage.URL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
			u.Path != "" ||
			(u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
			return c, errors.New("Storage.URL must be HTTPS or loopback HTTP origin")
		}
		if len(c.Storage.ManagementToken) < 32 || strings.ContainsAny(c.Storage.ManagementToken, " \t\r\n") {
			return c, errors.New(
				"Storage.ManagementToken must contain at least 32 characters without whitespace",
			)
		}
		if c.Storage.ManagementToken == c.ServiceToken {
			return c, errors.New("storage management credential must differ from tenant service credential")
		}
		if c.Storage.Timeout == 0 {
			c.Storage.Timeout = 15 * time.Second
		}
		if c.Storage.Timeout < time.Second || c.Storage.Timeout > time.Minute {
			return c, errors.New("Storage.Timeout must be between 1s and 1m")
		}
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

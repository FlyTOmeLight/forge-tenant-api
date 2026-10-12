package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct{ DatabaseURL, PublicURL, Issuer, ClientID, ClientSecret, CasdoorURL, CasdoorClient, CasdoorSecret, Organization, GitLabURL, GitLabPublicURL, GitLabClientID, GitLabToken, GitLabTokenFile, BootstrapUser, AppKey, ServiceToken, CAFile, Listen, ModelURL string }

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), PublicURL: strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"), Issuer: os.Getenv("OIDC_ISSUER"), ClientID: os.Getenv("OIDC_CLIENT_ID"), ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"), CasdoorURL: strings.TrimRight(os.Getenv("CASDOOR_URL"), "/"), CasdoorClient: os.Getenv("CASDOOR_CLIENT_ID"), CasdoorSecret: os.Getenv("CASDOOR_SECRET"), Organization: os.Getenv("CASDOOR_ORGANIZATION"), GitLabURL: strings.TrimRight(os.Getenv("GITLAB_URL"), "/"), GitLabToken: os.Getenv("GITLAB_TOKEN"), GitLabTokenFile: os.Getenv("GITLAB_TOKEN_FILE"), BootstrapUser: os.Getenv("BOOTSTRAP_USERNAME"), AppKey: os.Getenv("APP_KEY"), ServiceToken: os.Getenv("SERVICE_TOKEN"), CAFile: os.Getenv("CA_FILE"), Listen: os.Getenv("LISTEN_ADDR")}
	c.ModelURL = strings.TrimRight(os.Getenv("MODEL_URL"), "/")
	c.GitLabClientID = os.Getenv("GITLAB_OIDC_CLIENT_ID")
	c.GitLabPublicURL = strings.TrimRight(os.Getenv("GITLAB_PUBLIC_URL"), "/")
	if c.GitLabPublicURL == "" {
		c.GitLabPublicURL = c.GitLabURL
	}

	if c.Listen == "" {
		c.Listen = ":8080"
	}
	for name, value := range map[string]string{"DATABASE_URL": c.DatabaseURL, "PUBLIC_URL": c.PublicURL, "OIDC_ISSUER": c.Issuer, "OIDC_CLIENT_ID": c.ClientID, "OIDC_CLIENT_SECRET": c.ClientSecret, "CASDOOR_URL": c.CasdoorURL, "CASDOOR_CLIENT_ID": c.CasdoorClient, "CASDOOR_SECRET": c.CasdoorSecret, "CASDOOR_ORGANIZATION": c.Organization, "GITLAB_URL": c.GitLabURL, "GITLAB_TOKEN": c.GitLabToken, "APP_KEY": c.AppKey, "SERVICE_TOKEN": c.ServiceToken} {
		if value == "" {
			return c, fmt.Errorf("missing %s", name)
		}
	}
	u, e := url.Parse(c.PublicURL)
	if e != nil || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return c, errors.New("PUBLIC_URL must be an HTTPS origin or loopback HTTP origin")
	}
	if len(c.ServiceToken) < 32 {
		return c, errors.New("SERVICE_TOKEN must contain at least 32 characters")
	}
	return c, nil
}

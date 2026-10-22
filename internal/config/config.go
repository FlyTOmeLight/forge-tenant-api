package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/FlyTOmeLight/server-go/conf"
	"github.com/FlyTOmeLight/server-go/db"
)

// Config is the normalized runtime configuration shared by domain services.
// DB is only consumed by assemblies that own a database connection.
type Config struct {
	DB                               db.Config
	PublicURL, CAFile, EncryptionKey string
	Storage                          StorageConfig
	Gitlab                           GitlabConfig
	Casdoor                          CasdoorConfig
}

type TenantConfig struct {
	PublicURL, CAFile, EncryptionKey string
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

type BootstrapConfig struct {
	Certificate, Username, Email, GitLabTokenExpiresAt string
}

// File mirrors the top-level TOML sections.
type File struct {
	DB        db.Config
	Tenant    TenantConfig
	Gitlab    GitlabConfig
	Casdoor   CasdoorConfig
	Storage   StorageConfig
	Bootstrap BootstrapConfig
}

func Load(snapshot *conf.Config) (Config, error) {
	file, err := Decode(snapshot)
	if err != nil {
		return Config{}, err
	}
	return file.Normalize()
}

func Decode(snapshot *conf.Config) (File, error) {
	var file File
	if err := snapshot.Unmarshal(&file); err != nil {
		return File{}, err
	}
	return file, nil
}

// Normalize validates shape and defaults without requiring every command's
// dependencies. Callers apply RequireDatabase, RequireAPI, RequireWorker or
// RequireBootstrap according to what they actually assemble.
func (file File) Normalize() (Config, error) {
	c := Config{
		DB: file.DB, PublicURL: file.Tenant.PublicURL, CAFile: file.Tenant.CAFile,
		EncryptionKey: file.Tenant.EncryptionKey, Gitlab: file.Gitlab, Casdoor: file.Casdoor,
		Storage: file.Storage,
	}
	if c.DB.Driver == "" {
		c.DB.Driver = db.DriverPostgreSQL
	}
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
		if c.Storage.Timeout == 0 {
			c.Storage.Timeout = 15 * time.Second
		}
		if c.Storage.Timeout < time.Second || c.Storage.Timeout > time.Minute {
			return c, errors.New("Storage.Timeout must be between 1s and 1m")
		}
	}
	for _, value := range []*string{&c.PublicURL, &c.Casdoor.URL, &c.Gitlab.URL, &c.Gitlab.PublicURL} {
		*value = strings.TrimRight(*value, "/")
	}
	if c.Gitlab.URL != "" && c.Gitlab.PublicURL == "" {
		c.Gitlab.PublicURL = c.Gitlab.URL
	}
	return c, nil
}

func (c Config) RequireDatabase() error {
	if c.DB.DSN == "" && c.DB.Host == "" {
		return errors.New("missing database connection: set DB.DSN")
	}
	return nil
}

func (c Config) RequireAPI() error {
	for name, value := range map[string]string{
		"PUBLIC_URL": c.PublicURL, "TENANT_ENCRYPTION_KEY": c.EncryptionKey,
		"CASDOOR_URL": c.Casdoor.URL, "CASDOOR_CLIENT_ID": c.Casdoor.ClientID,
		"CASDOOR_SECRET": c.Casdoor.Secret, "CASDOOR_ORGANIZATION": c.Casdoor.Organization,
		"OIDC_ISSUER": c.Casdoor.Issuer, "OIDC_CLIENT_ID": c.Casdoor.OIDCClientID,
		"OIDC_CLIENT_SECRET": c.Casdoor.OIDCClientSecret,
	} {
		if value == "" {
			return fmt.Errorf("missing %s", name)
		}
	}
	if err := validateOrigin(c.PublicURL); err != nil {
		return err
	}
	key, err := hex.DecodeString(c.EncryptionKey)
	if err != nil || len(key) != 32 {
		return errors.New("TENANT_ENCRYPTION_KEY must be a 32-byte hex key")
	}
	return nil
}

func (c Config) RequireWorker() error {
	if c.Gitlab.URL == "" {
		return errors.New("missing GITLAB_URL")
	}
	if c.Gitlab.Token == "" && c.Gitlab.TokenFile == "" {
		return errors.New("GITLAB_TOKEN or GITLAB_TOKEN_FILE is required")
	}
	return nil
}

func (c Config) RequireBootstrap() error {
	for name, value := range map[string]string{
		"CASDOOR_URL": c.Casdoor.URL, "CASDOOR_CLIENT_ID": c.Casdoor.ClientID,
		"CASDOOR_SECRET": c.Casdoor.Secret, "CASDOOR_ORGANIZATION": c.Casdoor.Organization,
		"OIDC_ISSUER": c.Casdoor.Issuer,
	} {
		if value == "" {
			return fmt.Errorf("missing %s", name)
		}
	}
	return nil
}

func validateOrigin(origin string) error {
	u, e := url.Parse(origin)
	if e != nil || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return errors.New("PUBLIC_URL must be an HTTPS origin or loopback HTTP origin")
	}
	return nil
}

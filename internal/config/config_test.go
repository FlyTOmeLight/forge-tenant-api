package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FlyTOmeLight/server-go/conf"
	"github.com/FlyTOmeLight/server-go/db"
)

func TestConfigurationSnapshot(t *testing.T) {
	base := map[string]map[string]any{
		"DB": {
			"DSN": "postgres://example",
		},
		"Tenant": {
			"PublicURL":     "http://127.0.0.1:8483/",
			"CAFile":        "/run/ca.pem",
			"EncryptionKey": strings.Repeat("ab", 32),
		},
		"Casdoor": {
			"Issuer":           "https://identity.test",
			"OIDCClientID":     "console",
			"OIDCClientSecret": "secret",
			"URL":              "http://identity.test/",
			"ClientID":         "management",
			"Secret":           "secret",
			"Organization":     "identities",
		},
		"Gitlab": {
			"URL":       "https://gitlab.test/",
			"TokenFile": "/run/token",
		},
	}
	for _, tc := range []struct {
		name, section, field, value string
		require                     func(Config) error
		invalid                     bool
	}{
		{name: "API configuration and normalized origins", require: Config.RequireAPI},
		{name: "worker file credential", require: Config.RequireWorker},
		{name: "bootstrap configuration", require: Config.RequireBootstrap},
		{name: "database configuration", require: Config.RequireDatabase},
		{name: "missing identity client", section: "Casdoor", field: "OIDCClientID", require: Config.RequireAPI, invalid: true},
		{name: "unsafe public origin", section: "Tenant", field: "PublicURL", value: "http://example.test", require: Config.RequireAPI, invalid: true},
		{name: "malformed tenant encryption key", section: "Tenant", field: "EncryptionKey", value: "not-hex", require: Config.RequireAPI, invalid: true},
		{name: "short tenant encryption key", section: "Tenant", field: "EncryptionKey", value: strings.Repeat("ab", 31), require: Config.RequireAPI, invalid: true},
		{name: "missing GitLab credential", section: "Gitlab", field: "TokenFile", require: Config.RequireWorker, invalid: true},
		{name: "missing GitLab URL", section: "Gitlab", field: "URL", require: Config.RequireWorker, invalid: true},
		{name: "missing database", section: "DB", field: "DSN", require: Config.RequireDatabase, invalid: true},
		{name: "missing bootstrap issuer", section: "Casdoor", field: "Issuer", require: Config.RequireBootstrap, invalid: true},
		{name: "missing bootstrap credential", section: "Casdoor", field: "Secret", require: Config.RequireBootstrap, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]any{"AppName": "forge", "AppCode": 101, "RunMode": "release"}
			for section, fields := range base {
				copy := make(map[string]any)
				for key, value := range fields {
					copy[key] = value
				}
				if section == tc.section {
					copy[tc.field] = tc.value
				}
				values[section] = copy
			}
			snapshot, err := conf.FromSettings(values)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.DB.Driver != db.DriverPostgreSQL {
				t.Fatalf("default database driver = %q", cfg.DB.Driver)
			}
			if err := tc.require(cfg); (err != nil) != tc.invalid {
				t.Fatalf("invalid=%v error=%v", tc.invalid, err)
			}
			if !tc.invalid &&
				(cfg.DB.DSN != "postgres://example" || cfg.PublicURL != "http://127.0.0.1:8483" ||
					cfg.CAFile != "/run/ca.pem" || cfg.EncryptionKey != strings.Repeat("ab", 32) ||
					cfg.Casdoor.URL != "http://identity.test" || cfg.Gitlab.URL != "https://gitlab.test" ||
					cfg.Gitlab.PublicURL != "https://gitlab.test") {
				t.Fatal("section decoding or origin normalization failed")
			}
		})
	}
	t.Run("database host", func(t *testing.T) {
		cfg := Config{DB: db.Config{Host: "postgres.internal"}}
		if err := cfg.RequireDatabase(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestServerTOMLExternalServices(t *testing.T) {
	for name, value := range map[string]string{
		"DATABASE_URL":             "postgres://example",
		"PUBLIC_URL":               "https://forge.test/",
		"TENANT_ENCRYPTION_KEY":    strings.Repeat("ab", 32),
		"CA_FILE":                  "/run/ca.pem",
		"GITLAB_URL":               "https://gitlab-api.test/",
		"GITLAB_PUBLIC_URL":        "https://gitlab.test/",
		"GITLAB_OIDC_CLIENT_ID":    "gitlab-login",
		"GITLAB_TOKEN":             "gitlab-token",
		"GITLAB_TOKEN_FILE":        "/run/gitlab-token",
		"CASDOOR_URL":              "https://casdoor-api.test/",
		"CASDOOR_CLIENT_ID":        "management",
		"CASDOOR_SECRET":           "management-secret",
		"CASDOOR_ORGANIZATION":     "identities",
		"OIDC_ISSUER":              "https://identity.test",
		"OIDC_CLIENT_ID":           "forge-login",
		"OIDC_CLIENT_SECRET":       "login-secret",
		"STORAGE_URL":              "",
		"STORAGE_MANAGEMENT_TOKEN": "",
	} {
		t.Setenv(name, value)
	}
	snapshot, err := conf.Load("../../conf/server/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		DB: db.Config{
			Driver:       db.DriverPostgreSQL,
			DSN:          "postgres://example",
			MaxOpenConns: 12,
			MaxIdleConns: 12,
		},
		PublicURL:     "https://forge.test",
		CAFile:        "/run/ca.pem",
		EncryptionKey: strings.Repeat("ab", 32),
		Gitlab: GitlabConfig{
			URL:       "https://gitlab-api.test",
			PublicURL: "https://gitlab.test",
			ClientID:  "gitlab-login",
			Token:     "gitlab-token",
			TokenFile: "/run/gitlab-token",
		},
		Casdoor: CasdoorConfig{
			URL:              "https://casdoor-api.test",
			ClientID:         "management",
			Secret:           "management-secret",
			Organization:     "identities",
			Issuer:           "https://identity.test",
			OIDCClientID:     "forge-login",
			OIDCClientSecret: "login-secret",
		},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf(
			"server TOML did not map environment variables to typed sections\ngot:  %#v\nwant: %#v",
			cfg,
			want,
		)
	}
}

func TestLiteralExternalServiceTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	data := `AppName = "forge"
AppCode = 101
RunMode = "test"
[DB]
DSN = "postgres://example"
[Tenant]
PublicURL = "https://forge.test"
CAFile = "/run/ca.pem"
EncryptionKey = "abababababababababababababababababababababababababababababababab"
[Gitlab]
URL = "https://gitlab.test"
Token = "token"
[Casdoor]
URL = "https://identity.test"
ClientID = "management"
Secret = "management-secret"
Organization = "identities"
Issuer = "https://identity.test"
OIDCClientID = "console"
OIDCClientSecret = "login-secret"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := conf.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.DSN != "postgres://example" || cfg.PublicURL != "https://forge.test" ||
		cfg.CAFile != "/run/ca.pem" || cfg.EncryptionKey != strings.Repeat("ab", 32) {
		t.Fatal("database and tenant settings were not loaded from their sections")
	}
	if cfg.Gitlab.URL != "https://gitlab.test" || cfg.Casdoor.URL != "https://identity.test" ||
		cfg.Casdoor.OIDCClientID != "console" {
		t.Fatal("external services were not loaded from top-level sections")
	}
}

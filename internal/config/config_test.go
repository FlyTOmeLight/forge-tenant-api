package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FlyTOmeLight/server-go/conf"
)

func TestConfigurationSnapshot(t *testing.T) {
	base := map[string]map[string]any{
		"Forge": {
			"DatabaseURL":  "postgres://example",
			"PublicURL":    "http://127.0.0.1:8483/",
			"AppKey":       strings.Repeat("ab", 32),
			"ServiceToken": strings.Repeat("x", 32),
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
		invalid                     bool
	}{
		{name: "file credential and normalized origins"},
		{name: "missing identity client", section: "Casdoor", field: "OIDCClientID", invalid: true},
		{name: "unsafe public origin", section: "Forge", field: "PublicURL", value: "http://example.test", invalid: true},
		{name: "short service credential", section: "Forge", field: "ServiceToken", value: "short", invalid: true},
		{name: "missing GitLab credential", section: "Gitlab", field: "TokenFile", invalid: true},
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
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid=%v error=%v", tc.invalid, err)
			}
			if !tc.invalid &&
				(cfg.PublicURL != "http://127.0.0.1:8483" || cfg.Gitlab.PublicURL != "https://gitlab.test") {
				t.Fatal("origin normalization failed")
			}
		})
	}
}

func TestServerTOMLExternalServices(t *testing.T) {
	for name, value := range map[string]string{
		"DATABASE_URL":          "postgres://example",
		"PUBLIC_URL":            "https://forge.test/",
		"APP_KEY":               strings.Repeat("ab", 32),
		"SERVICE_TOKEN":         strings.Repeat("s", 32),
		"BOOTSTRAP_USERNAME":    "operator",
		"CA_FILE":               "/run/ca.pem",
		"MODEL_URL":             "https://model.test/",
		"GITLAB_URL":            "https://gitlab-api.test/",
		"GITLAB_PUBLIC_URL":     "https://gitlab.test/",
		"GITLAB_OIDC_CLIENT_ID": "gitlab-login",
		"GITLAB_TOKEN":          "gitlab-token",
		"GITLAB_TOKEN_FILE":     "/run/gitlab-token",
		"CASDOOR_URL":           "https://casdoor-api.test/",
		"CASDOOR_CLIENT_ID":     "management",
		"CASDOOR_SECRET":        "management-secret",
		"CASDOOR_ORGANIZATION":  "identities",
		"OIDC_ISSUER":           "https://identity.test",
		"OIDC_CLIENT_ID":        "forge-login",
		"OIDC_CLIENT_SECRET":    "login-secret",
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
		DatabaseURL:   "postgres://example",
		PublicURL:     "https://forge.test",
		AppKey:        strings.Repeat("ab", 32),
		ServiceToken:  strings.Repeat("s", 32),
		BootstrapUser: "operator",
		CAFile:        "/run/ca.pem",
		ModelURL:      "https://model.test",
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
		t.Fatal("server TOML did not map existing environment variables to typed sections")
	}
}

func TestLiteralExternalServiceTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	data := `AppName = "forge"
AppCode = 101
RunMode = "test"
[Forge]
DatabaseURL = "postgres://example"
PublicURL = "https://forge.test"
AppKey = "key"
ServiceToken = "01234567890123456789012345678901"
# Former Forge fields must not override the dedicated sections.
GitLabURL = "https://wrong.test"
CasdoorURL = "https://wrong.test"
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
	if cfg.Gitlab.URL != "https://gitlab.test" || cfg.Casdoor.URL != "https://identity.test" ||
		cfg.Casdoor.OIDCClientID != "console" {
		t.Fatal("external services were not loaded from top-level sections")
	}
}

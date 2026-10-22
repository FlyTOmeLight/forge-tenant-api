package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/bootstrap"
)

func externalBootstrapEnvironment(t *testing.T) string {
	t.Helper()
	expires := time.Now().Add(90 * 24 * time.Hour).Format("2006-01-02")
	for key, value := range map[string]string{
		"SERVER_CONFIG_PATH":      "../conf/server/config.toml",
		"CASDOOR_URL":             "http://casdoor.test:8000/",
		"CASDOOR_ORGANIZATION":    "forge-org",
		"GITLAB_URL":              "http://gitlab.test:8181/",
		"PUBLIC_URL":              "https://forge.test/",
		"CA_FILE":                 "/external-ca/ca.crt",
		"BOOTSTRAP_CERTIFICATE":   "existing-cert",
		"BOOTSTRAP_USERNAME":      "forge-admin",
		"BOOTSTRAP_EMAIL":         "forge@example.test",
		"GITLAB_TOKEN_EXPIRES_AT": expires,
		"POD_NAMESPACE":           "team-space",
		"HELM_RELEASE_NAME":       "demo",
		"DATABASE_URL":            "", "TENANT_ENCRYPTION_KEY": "",
		"CASDOOR_CLIENT_ID": "", "CASDOOR_SECRET": "",
		"OIDC_ISSUER": "", "OIDC_CLIENT_ID": "", "OIDC_CLIENT_SECRET": "",
		"GITLAB_TOKEN": "", "GITLAB_TOKEN_FILE": "",
		"STORAGE_URL": "", "STORAGE_MANAGEMENT_TOKEN": "",
	} {
		t.Setenv(key, value)
	}
	return expires
}

func TestExternalBootstrapTOMLMapping(t *testing.T) {
	expires := externalBootstrapEnvironment(t)
	for _, pair := range [][2]string{{"team-space", "demo"}, {"forge", "forge"}} {
		t.Run(pair[0]+"/"+pair[1], func(t *testing.T) {
			t.Setenv("POD_NAMESPACE", pair[0])
			t.Setenv("HELM_RELEASE_NAME", pair[1])
			got, err := loadExternalBootstrapConfig()
			if err != nil {
				t.Fatal(err)
			}
			identity := pair[0] + "-" + pair[1]
			want := bootstrap.Config{
				CasdoorURL:            "http://casdoor.test:8000",
				CasdoorOrganization:   "forge-org",
				CasdoorCertificate:    "existing-cert",
				OIDCApplication:       identity + "-console",
				ManagementApplication: identity + "-management",
				PublicURL:             "https://forge.test",
				BootstrapUsername:     "forge-admin",
				BootstrapEmail:        "forge@example.test",
				GitLabURL:             "http://gitlab.test:8181",
				GitLabTokenName:       identity + "-tenant",
				GitLabTokenExpiresAt:  expires,
				Namespace:             pair[0],
				OutputSecret:          pair[1] + "-external-credentials",
				AdminDirectory:        "/bootstrap-admin",
				SeedDirectory:         "/identity-seed",
				CAFile:                "/external-ca/ca.crt",
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("main TOML inputs did not map to expected external initialization parameters")
			}
		})
	}
}

func TestExternalBootstrapRequiresOwnInputs(t *testing.T) {
	externalBootstrapEnvironment(t)
	for _, key := range []string{
		"CASDOOR_URL", "CASDOOR_ORGANIZATION", "GITLAB_URL", "PUBLIC_URL",
		"BOOTSTRAP_CERTIFICATE", "BOOTSTRAP_USERNAME", "BOOTSTRAP_EMAIL",
		"GITLAB_TOKEN_EXPIRES_AT", "POD_NAMESPACE", "HELM_RELEASE_NAME",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")
			if _, err := loadExternalBootstrapConfig(); err == nil {
				t.Fatal("missing required bootstrap input accepted")
			}
		})
	}
	t.Run("derived identity exceeds name limit", func(t *testing.T) {
		t.Setenv("HELM_RELEASE_NAME", strings.Repeat("a", 53))
		if _, err := loadExternalBootstrapConfig(); err == nil {
			t.Fatal("oversized derived name accepted")
		}
	})
}

func TestExternalBootstrapLiteralTOMLIgnoresRuntimeSections(t *testing.T) {
	expires := externalBootstrapEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	data := `AppName = "forge"
AppCode = 101
RunMode = "test"
[Tenant]
PublicURL = "https://literal.test"
[Casdoor]
URL = "http://identity.test"
Organization = "literal-org"
[Gitlab]
URL = "http://source.test"
[Bootstrap]
Certificate = "literal-cert"
Username = "literal-admin"
Email = "literal@example.test"
GitLabTokenExpiresAt = "` + expires + `"
[DB]
MaxOpenConns = "unused-invalid-value"
[Storage]
URL = "not-an-origin"
Timeout = "unused-invalid-value"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SERVER_CONFIG_PATH", path)
	got, err := loadExternalBootstrapConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.CasdoorURL != "http://identity.test" || got.CasdoorOrganization != "literal-org" ||
		got.GitLabURL != "http://source.test" || got.PublicURL != "https://literal.test" ||
		got.CasdoorCertificate != "literal-cert" || got.BootstrapUsername != "literal-admin" ||
		got.BootstrapEmail != "literal@example.test" || got.GitLabTokenExpiresAt != expires {
		t.Fatal("literal TOML values were not used")
	}
}

func TestWaitExternalProjection(t *testing.T) {
	directory := t.TempDir()
	snapshot := filepath.Join(directory, "snapshot")
	if err := os.Mkdir(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(snapshot, filepath.Join(directory, "..data")); err != nil {
		t.Fatal(err)
	}
	write := func(key, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(snapshot, key), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(bootstrap.CompleteKey, "true")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WaitExternal(ctx, []string{"--directory", directory}); err == nil {
		t.Fatal("completion marker without credentials was accepted")
	}
	for _, key := range bootstrap.RequiredKeys {
		write(key, "test-value")
	}
	if err := WaitExternal(context.Background(), []string{"--directory", directory}); err != nil {
		t.Fatal(err)
	}
	write(bootstrap.CompleteKey, "false")
	if _, err := readExternal(directory); err == nil {
		t.Fatal("incomplete projection accepted")
	}
}

func TestExternalInitializationCommandArguments(t *testing.T) {
	if err := ExternalBootstrap(context.Background(), []string{"--config", "old.json"}); err == nil {
		t.Fatal("obsolete bootstrap JSON argument accepted")
	}
	if err := WaitExternal(context.Background(), []string{"--timeout", "5m"}); err == nil {
		t.Fatal("configurable initialization timeout accepted")
	}
}

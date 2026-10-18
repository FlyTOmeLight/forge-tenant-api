package config

import (
	"strings"
	"testing"
	"time"

	"github.com/FlyTOmeLight/server-go/db"
)

func TestStorageProvisionConfiguration(t *testing.T) {
	base := File{
		DB: db.Config{DSN: "postgres://example"},
		Tenant: TenantConfig{
			PublicURL:     "https://forge.test",
			EncryptionKey: strings.Repeat("ab", 32),
		},
		Gitlab: GitlabConfig{URL: "https://gitlab.test", Token: "token"},
		Casdoor: CasdoorConfig{
			URL:              "https://casdoor.test",
			ClientID:         "management",
			Secret:           "secret",
			Organization:     "org",
			Issuer:           "https://identity.test",
			OIDCClientID:     "oidc",
			OIDCClientSecret: "secret",
		},
	}
	for _, tc := range []struct {
		name    string
		storage StorageConfig
		invalid bool
	}{
		{name: "disabled", storage: StorageConfig{}, invalid: false},
		{name: "enabled", storage: StorageConfig{URL: "https://storage.test/", ManagementToken: strings.Repeat("m", 32)}, invalid: false},
		{name: "loopback test", storage: StorageConfig{URL: "http://127.0.0.1:1234", ManagementToken: strings.Repeat("m", 32), Timeout: time.Second}, invalid: false},
		{name: "missing URL", storage: StorageConfig{ManagementToken: strings.Repeat("m", 32)}, invalid: true},
		{name: "missing credential", storage: StorageConfig{URL: "https://storage.test"}, invalid: true},
		{name: "short credential", storage: StorageConfig{URL: "https://storage.test", ManagementToken: "short"}, invalid: true},
		{name: "credential header injection", storage: StorageConfig{URL: "https://storage.test", ManagementToken: strings.Repeat("m", 32) + "\n"}, invalid: true},
		{name: "plaintext remote", storage: StorageConfig{URL: "http://storage.test", ManagementToken: strings.Repeat("m", 32)}, invalid: true},
		{name: "url credentials", storage: StorageConfig{URL: "https://user:secret@storage.test", ManagementToken: strings.Repeat("m", 32)}, invalid: true},
		{name: "url path", storage: StorageConfig{URL: "https://storage.test/path", ManagementToken: strings.Repeat("m", 32)}, invalid: true},
		{name: "unbounded timeout", storage: StorageConfig{URL: "https://storage.test", ManagementToken: strings.Repeat("m", 32), Timeout: time.Hour}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Storage = tc.storage
			normalized, err := cfg.Normalize()
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid=%v error=%v", tc.invalid, err)
			}
			if !tc.invalid && normalized.Storage.URL != strings.TrimRight(tc.storage.URL, "/") {
				t.Fatalf("storage URL was not normalized: %q", normalized.Storage.URL)
			}
			if !tc.invalid && tc.storage.URL != "" && normalized.Storage.Timeout <= 0 {
				t.Fatal("missing HTTP timeout")
			}
		})
	}
}

package config

import (
	"strings"
	"testing"
	"time"
)

func TestStorageProvisionConfiguration(t *testing.T) {
	base := File{
		Forge: Config{
			DatabaseURL:  "postgres://example",
			PublicURL:    "https://forge.test",
			AppKey:       strings.Repeat("a", 64),
			ServiceToken: strings.Repeat("s", 32),
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
		{name: "missing credential", storage: StorageConfig{URL: "https://storage.test"}, invalid: true},
		{name: "short credential", storage: StorageConfig{URL: "https://storage.test", ManagementToken: "short"}, invalid: true},
		{name: "shared credential", storage: StorageConfig{URL: "https://storage.test", ManagementToken: strings.Repeat("s", 32)}, invalid: true},
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
				t.Fatal(err)
			}
			if !tc.invalid && tc.storage.URL != "" && normalized.Storage.Timeout <= 0 {
				t.Fatal("missing HTTP timeout")
			}
		})
	}
}

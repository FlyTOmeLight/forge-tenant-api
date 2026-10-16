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
		{"disabled", StorageConfig{}, false},
		{"enabled", StorageConfig{URL: "https://storage.test/", ManagementToken: strings.Repeat("m", 32)}, false},
		{"loopback test", StorageConfig{URL: "http://127.0.0.1:1234", ManagementToken: strings.Repeat("m", 32), Timeout: time.Second}, false},
		{"missing credential", StorageConfig{URL: "https://storage.test"}, true},
		{"short credential", StorageConfig{URL: "https://storage.test", ManagementToken: "short"}, true},
		{"shared credential", StorageConfig{URL: "https://storage.test", ManagementToken: strings.Repeat("s", 32)}, true},
		{"credential header injection", StorageConfig{URL: "https://storage.test", ManagementToken: strings.Repeat("m", 32) + "\n"}, true},
		{"plaintext remote", StorageConfig{URL: "http://storage.test", ManagementToken: strings.Repeat("m", 32)}, true},
		{"url credentials", StorageConfig{URL: "https://user:secret@storage.test", ManagementToken: strings.Repeat("m", 32)}, true},
		{"url path", StorageConfig{URL: "https://storage.test/path", ManagementToken: strings.Repeat("m", 32)}, true},
		{"unbounded timeout", StorageConfig{URL: "https://storage.test", ManagementToken: strings.Repeat("m", 32), Timeout: time.Hour}, true},
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

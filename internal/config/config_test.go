package config

import (
	"strings"
	"testing"

	"github.com/FlyTOmeLight/server-go/conf"
)

func TestConfigurationSnapshot(t *testing.T) {
	base := map[string]any{
		"DatabaseURL":     "postgres://example",
		"PublicURL":       "http://127.0.0.1:8483/",
		"Issuer":          "https://identity.test",
		"ClientID":        "console",
		"ClientSecret":    "secret",
		"CasdoorURL":      "http://identity.test/",
		"CasdoorClient":   "management",
		"CasdoorSecret":   "secret",
		"Organization":    "identities",
		"GitLabURL":       "https://gitlab.test/",
		"GitLabTokenFile": "/run/token",
		"AppKey":          strings.Repeat("ab", 32),
		"ServiceToken":    strings.Repeat("x", 32),
	}
	for _, tc := range []struct {
		name, field, value string
		invalid            bool
	}{
		{name: "file credential and normalized origins", field: "", value: "", invalid: false},
		{name: "missing identity client", field: "ClientID", value: "", invalid: true},
		{name: "unsafe public origin", field: "PublicURL", value: "http://example.test", invalid: true},
		{name: "short service credential", field: "ServiceToken", value: "short", invalid: true},
		{name: "missing GitLab credential", field: "GitLabTokenFile", value: "", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]any)
			for k, v := range base {
				values[k] = v
			}
			if tc.field != "" {
				values[tc.field] = tc.value
			}
			snapshot, err := conf.FromSettings(
				map[string]any{
					"AppName": "forge",
					"AppCode": 101,
					"RunMode": "release",
					"Forge":   values,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(snapshot)
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid=%v error=%v", tc.invalid, err)
			}
			if !tc.invalid &&
				(cfg.PublicURL != "http://127.0.0.1:8483" || cfg.GitLabPublicURL != "https://gitlab.test") {
				t.Fatal("origin normalization failed")
			}
		})
	}
}

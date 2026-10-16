package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
)

func TestCredentialStatusUsesTOML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/personal_access_tokens/self" ||
			r.Header.Get("PRIVATE-TOKEN") != "toml-token" {
			t.Error("credential command did not use TOML configuration")
		}
		fmt.Fprint(w, `{"id":42,"user_id":7,"name":"test","scopes":["api"],"revoked":false}`)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(
		path,
		[]byte(
			fmt.Sprintf(
				"AppName = 'forge-test'\nAppCode = 101\nRunMode = 'release'\n[Forge]\nCAFile = ''\n[Gitlab]\nURL = %q\nToken = 'toml-token'\n",
				server.URL,
			),
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SERVER_CONFIG_PATH", path)
	t.Setenv("GITLAB_URL", "http://invalid.example.test")
	t.Setenv("GITLAB_TOKEN", "wrong-env-token")
	var output bytes.Buffer
	if err := CredentialStatus(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	var status client.GitLabCredential
	if err := json.NewDecoder(&output).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.ID != 42 || status.UserID != 7 {
		t.Fatal("incorrect credential metadata")
	}
}

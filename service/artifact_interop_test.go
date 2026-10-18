package service

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
)

// TestArtifactInteropServer uses only setup(t)'s new isolated test identities.
// No existing configuration, business session or production credential is read.
func TestArtifactInteropServer(t *testing.T) {
	output := os.Getenv("FORGE_ARTIFACT_INTEROP_OUTPUT")
	if output == "" {
		t.Skip("requires an explicit disposable artifact interop caller")
	}
	dir := filepath.Dir(output)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("interop requires an existing private non-symlink 0700 directory")
	}
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal("cannot resolve temporary directory")
	}
	actualDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal("cannot resolve interop directory")
	}
	if filepath.Base(output) != "context.json" || filepath.Dir(actualDir) != tempRoot ||
		!strings.HasPrefix(filepath.Base(actualDir), "artifact-forge-interop-") {
		t.Fatal("interop output must be context.json in a private artifact-forge-interop-* temporary directory")
	}
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Fatal("interop requires a disposable PostgreSQL database")
	}
	f := setup(t)
	tid, _ := f.tenant(t)
	pid := identity.ID("p")
	if err := f.a.DB.Exec(
		`INSERT INTO projects(id,tenant_id,name,path,mode) VALUES(?,?,?,?,'bind')`,
		pid,
		tid,
		"Interop project",
		"interop/project",
	).Error; err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiHandler(t, f.a))
	t.Cleanup(server.Close)
	data, err := json.Marshal(map[string]string{
		"endpoint":      server.URL,
		"service_token": f.a.ServiceKey,
		"session":       f.operator,
		"csrf":          identity.Hash(f.operator + ":csrf"),
		"origin":        f.a.Account.Config.PublicURL,
		"user_id":       "operator",
		"tenant_id":     tid,
		"project_id":    pid,
		"other_session": f.other,
		"other_csrf":    identity.Hash(f.other + ":csrf"),
	})
	if err != nil {
		t.Fatal("cannot encode disposable interop fixture")
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal("cannot exclusively create private interop fixture")
	}
	t.Cleanup(func() { _ = os.Remove(output) })
	if _, err = file.Write(data); err != nil {
		file.Close()
		t.Fatal("cannot write disposable interop fixture")
	}
	if err = file.Close(); err != nil {
		t.Fatal("cannot close disposable interop fixture")
	}
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	stop := filepath.Join(dir, "stop")
	for {
		select {
		case <-t.Context().Done():
			t.Fatal("interop test context canceled")
		case <-deadline.C:
			t.Fatal("interop caller did not stop within two minutes")
		case <-ticker.C:
			if _, err := os.Stat(stop); err == nil {
				return
			} else if !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("cannot inspect interop stop path")
			}
		}
	}
}

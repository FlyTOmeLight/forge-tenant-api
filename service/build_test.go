package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlyTOmeLight/server-go/conf"
	"github.com/FlyTOmeLight/server-go/host"
	"github.com/FlyTOmeLight/server-go/logger"
	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
)

func assemblyConfig(dsn, issuer string) Config {
	return Config{
		Forge: ForgeConfig{
			DatabaseURL: dsn, PublicURL: "http://127.0.0.1:8383",
			AppKey: strings.Repeat("ab", 32), ServiceToken: strings.Repeat("s", 32),
		},
		Gitlab: GitlabConfig{URL: issuer, Token: "test-token"},
		Casdoor: CasdoorConfig{
			URL:              issuer,
			ClientID:         "casdoor-client",
			Secret:           "casdoor-secret",
			Organization:     "test",
			Issuer:           issuer,
			OIDCClientID:     "oidc-client",
			OIDCClientSecret: "oidc-secret",
		},
	}
}

func discoveryServer(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	closed := make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize",
			"token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	return server, closed
}

func awaitTransportClosed(t *testing.T, closed <-chan struct{}) {
	t.Helper()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("owned HTTP transport retained its idle connection")
	}
}

func assertBorrowedDBOpen(t *testing.T, database *gorm.DB) {
	t.Helper()
	connection, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := connection.PingContext(ctx); err != nil {
		t.Fatalf("borrowed database was closed: %v", err)
	}
}

func TestNewAPIHTTPHostAndBorrowedDatabase(t *testing.T) {
	f := setup(t)
	server, closed := discoveryServer(t)
	api, err := NewAPI(f.ctx, f.a.DB, assemblyConfig(f.a.Config.DatabaseURL, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = api.Stop(context.Background()) })
	manager, err := conf.NewManagerFromConfig(&conf.Config{AppName: "forge-test", AppCode: 101, RunMode: "release"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	log, err := logger.NewLogger(&logger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(log.Close)
	h, err := host.NewHTTPHost(manager, log, SetupHTTP, api)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	response := httptest.NewRecorder()
	h.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK || response.Body.String() != `{"status":"ok"}` {
		t.Fatalf("readiness returned %d: %s", response.Code, response.Body.String())
	}
	if err := api.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	awaitTransportClosed(t, closed)
	assertBorrowedDBOpen(t, f.a.DB)
}

func TestNewAPIInitializationErrors(t *testing.T) {
	for _, name := range []string{"missing database", "invalid config", "missing CA", "discovery failure", "invalid app key"} {
		t.Run(name, func(t *testing.T) {
			server, closed := discoveryServer(t)
			cfg := assemblyConfig("postgres://unused/test", server.URL)
			database := &gorm.DB{}
			want := ""
			switch name {
			case "missing database":
				database, want = nil, "requires a database"
			case "invalid config":
				cfg.Forge.ServiceToken, want = "short", "SERVICE_TOKEN"
			case "missing CA":
				cfg.Forge.CAFile, want = filepath.Join(t.TempDir(), "missing.pem"), "missing.pem"
			case "discovery failure":
				cfg.Casdoor.Issuer, want = server.URL+"/missing", "404"
			case "invalid app key":
				cfg.Forge.AppKey, want = "invalid", "APP_KEY"
			}
			api, err := NewAPI(t.Context(), database, cfg)
			if api != nil || err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("NewAPI returned %v, %v; want error containing %q", api, err, want)
			}
			if name == "discovery failure" || name == "invalid app key" {
				awaitTransportClosed(t, closed)
			}
		})
	}
}

func TestNewWorkerStopReleasesTransportAndBorrowsDatabase(t *testing.T) {
	f := setup(t)
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprintf("started=%t", started), func(t *testing.T) {
			server, closed := discoveryServer(t)
			worker, err := NewWorker(f.a.DB, assemblyConfig(f.a.Config.DatabaseURL, server.URL))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = worker.Stop(context.Background()) })
			// Give the owned transport a real idle connection to release on Stop.
			response, err := worker.transport.Get(server.URL + "/.well-known/openid-configuration")
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil {
				t.Fatalf("drain response: %v, %v", readErr, closeErr)
			}
			if started {
				if err := worker.Start(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			shutdown, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := worker.Stop(shutdown); err != nil {
				t.Fatal(err)
			}
			if started {
				select {
				case <-worker.done:
				default:
					t.Fatal("Stop returned before the worker exited")
				}
			}
			awaitTransportClosed(t, closed)
			assertBorrowedDBOpen(t, f.a.DB)
		})
	}
}

func TestRunMigrateDoesNotInitializeExternalClients(t *testing.T) {
	f := setup(t)
	schema := "migrate_" + identity.Token()[:16]
	if err := f.a.DB.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.a.DB.Exec("DROP SCHEMA " + schema + " CASCADE").Error })
	dsn, err := url.Parse(f.a.Config.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("search_path", schema)
	dsn.RawQuery = query.Encode()
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := fmt.Sprintf(`AppName = "forge-test"
AppCode = 101
RunMode = "release"
[Log]
Level = "Info"
[Forge]
DatabaseURL = %q
PublicURL = "http://127.0.0.1:8383"
AppKey = %q
ServiceToken = %q
CAFile = %q
[Gitlab]
URL = "http://127.0.0.1:1"
Token = "test-token"
[Casdoor]
URL = "http://127.0.0.1:1"
ClientID = "test"
Secret = "test"
Organization = "test"
Issuer = "http://127.0.0.1:1"
OIDCClientID = "test"
OIDCClientSecret = "test"
`, dsn.String(), strings.Repeat("ab", 32), strings.Repeat("s", 32), filepath.Join(t.TempDir(), "missing.pem"))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SERVER_CONFIG_PATH", path)
	if err := Run(f.ctx, "migrate"); err != nil {
		t.Fatal(err)
	}
	var tableExists bool
	if err := f.a.DB.Raw("SELECT to_regclass(?) IS NOT NULL", schema+".users").
		Scan(&tableExists).
		Error; err != nil ||
		!tableExists {
		t.Fatalf("migrate did not create users table: exists=%t, err=%v", tableExists, err)
	}
}

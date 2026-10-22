package bootstrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
)

type bootstrapFixture struct {
	mu                    sync.Mutex
	secret                map[string]string
	annotations           map[string]string
	version               int
	apps                  map[string]*casdoorsdk.Application
	user                  *casdoorsdk.User
	tokens                map[int]string
	next, issued, revoked int
	loseIssue             bool
	loseSave              bool
	denyManagement        bool
	denySecretPatch       bool
	server                *httptest.Server
	config                Config
	store                 *SecretStore
}

func newBootstrapFixture(t *testing.T) *bootstrapFixture {
	t.Helper()
	f := &bootstrapFixture{
		secret:      map[string]string{},
		annotations: map[string]string{},
		version:     1,
		apps:        map[string]*casdoorsdk.Application{},
		tokens:      map[int]string{},
		next:        10,
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	admin, seed := t.TempDir(), t.TempDir()
	for key, value := range map[string]string{"CASDOOR_ADMIN_CLIENT_ID": "external-admin", "CASDOOR_ADMIN_CLIENT_SECRET": "external-admin-secret", "GITLAB_ADMIN_TOKEN": "external-admin-token"} {
		if err := os.WriteFile(filepath.Join(admin, key), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"CASDOOR_CLIENT_ID": "forge-management-id", "CASDOOR_SECRET": strings.Repeat("m", 32), "OIDC_CLIENT_ID": "forge-oidc-id", "OIDC_CLIENT_SECRET": strings.Repeat("o", 32), "BOOTSTRAP_PASSWORD": strings.Repeat("p", 24)} {
		if err := os.WriteFile(filepath.Join(seed, key), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.config = Config{
		CasdoorURL:            f.server.URL,
		CasdoorOrganization:   "forge-org",
		CasdoorCertificate:    "cert-existing",
		OIDCApplication:       "forge-oidc",
		ManagementApplication: "forge-management",
		PublicURL:             "https://forge.test",
		BootstrapUsername:     "forge-admin",
		BootstrapEmail:        "forge@example.test",
		GitLabURL:             f.server.URL,
		GitLabTokenName:       "forge-tenant",
		GitLabTokenExpiresAt:  time.Now().Add(90 * 24 * time.Hour).Format("2006-01-02"),
		Namespace:             "forge",
		OutputSecret:          "forge-external",
		AdminDirectory:        admin,
		SeedDirectory:         seed,
		TimeoutSeconds:        30,
	}
	sa := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(sa, []byte("kubernetes-sa"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.store = &SecretStore{
		HTTP:      f.server.Client(),
		URL:       f.server.URL + "/api/v1/namespaces/forge/secrets/forge-external",
		TokenFile: sa,
	}
	return f
}

func (f *bootstrapFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	emit := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		if r.Header.Get("Authorization") != "Bearer kubernetes-sa" {
			w.WriteHeader(403)
			return
		}
		if r.Method == http.MethodPatch {
			if f.denySecretPatch {
				w.WriteHeader(403)
				return
			}
			var p struct {
				Metadata struct {
					ResourceVersion string             `json:"resourceVersion"`
					Annotations     map[string]*string `json:"annotations"`
				} `json:"metadata"`
				Data map[string]string `json:"data"`
			}
			_ = json.NewDecoder(r.Body).Decode(&p)
			if p.Metadata.ResourceVersion != strconv.Itoa(f.version) {
				w.WriteHeader(409)
				return
			}
			for k, v := range p.Metadata.Annotations {
				if v == nil {
					delete(f.annotations, k)
				} else {
					f.annotations[k] = *v
				}
			}
			for k, v := range p.Data {
				raw, _ := base64.StdEncoding.DecodeString(v)
				f.secret[k] = string(raw)
			}
			f.version++
			if f.loseSave && f.secret["GITLAB_TOKEN"] != "" {
				f.loseSave = false
				w.WriteHeader(500)
				return
			}
		} else if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		data := map[string]string{}
		for k, v := range f.secret {
			data[k] = base64.StdEncoding.EncodeToString([]byte(v))
		}
		emit(
			map[string]any{
				"metadata": map[string]any{
					"resourceVersion": strconv.Itoa(f.version),
					"annotations":     f.annotations,
				},
				"data": data,
			},
		)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v4/") {
		token := r.Header.Get("Private-Token")
		id := 0
		for k, v := range f.tokens {
			if v == token {
				id = k
			}
		}
		if token != "external-admin-token" && id == 0 {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/api/v4/user":
			emit(map[string]any{"id": 7, "is_admin": true, "state": "active"})
		case r.URL.Path == "/api/v4/personal_access_tokens/self":
			emit(f.tokenInfo(id))
		case r.URL.Path == "/api/v4/personal_access_tokens":
			items := []any{}
			for id := range f.tokens {
				item := f.tokenInfo(id).(map[string]any)
				if item["name"] == r.URL.Query().Get("search") {
					items = append(items, item)
				}
			}
			emit(items)
		case strings.HasPrefix(r.URL.Path, "/api/v4/personal_access_tokens/") && r.Method == http.MethodDelete:
			id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/v4/personal_access_tokens/"))
			delete(f.tokens, id)
			f.revoked++
			w.WriteHeader(204)
		case r.URL.Path == "/api/v4/users/7/personal_access_tokens" && r.Method == http.MethodPost:
			f.next++
			f.issued++
			f.tokens[f.next] = fmt.Sprintf("glpat-test-%d", f.next)
			if f.loseIssue {
				f.loseIssue = false
				w.WriteHeader(500)
				return
			}
			emit(f.tokenInfo(f.next))
		default:
			w.WriteHeader(404)
		}
		return
	}
	auth, _, _ := r.BasicAuth()
	if f.denyManagement && auth == "forge-management-id" {
		w.WriteHeader(403)
		return
	}
	var data any
	switch strings.TrimPrefix(r.URL.Path, "/api/") {
	case "get-application":
		data = f.apps[r.URL.Query().Get("id")]
	case "add-application":
		var app casdoorsdk.Application
		_ = json.NewDecoder(r.Body).Decode(&app)
		f.apps["admin/"+app.Name] = &app
		data = "Affected"
	case "get-user":
		data = f.user
	case "add-user":
		var user casdoorsdk.User
		_ = json.NewDecoder(r.Body).Decode(&user)
		f.user = &user
		data = "Affected"
	case "update-user":
		data = "Affected"
	default:
		w.WriteHeader(404)
		return
	}
	emit(map[string]any{"status": "ok", "data": data})
}

func (f *bootstrapFixture) tokenInfo(id int) any {
	return map[string]any{
		"id":          id,
		"name":        "forge-tenant",
		"user_id":     7,
		"scopes":      []string{"api"},
		"active":      true,
		"revoked":     false,
		"expires_at":  f.config.GitLabTokenExpiresAt,
		"token":       f.tokens[id],
		"description": "Forge tenant bootstrap namespace=" + f.config.Namespace,
	}
}

func TestExternalBootstrapFirstAndRepeat(t *testing.T) {
	f := newBootstrapFixture(t)
	for range 2 {
		if err := Run(context.Background(), f.config, f.store, f.server.Client()); err != nil {
			t.Fatal(err)
		}
	}
	if f.issued != 1 || f.revoked != 0 || len(f.apps) != 2 || f.secret["BOOTSTRAP_COMPLETE"] != "true" {
		t.Fatalf("unexpected state issued=%d revoked=%d applications=%d", f.issued, f.revoked, len(f.apps))
	}
	if _, exists := f.secret["GITLAB_ADMIN_TOKEN"]; exists {
		t.Fatal("administrator credential copied to output")
	}
}

func TestExternalBootstrapResetsIssuanceBudget(t *testing.T) {
	f := newBootstrapFixture(t)
	for round := range 4 {
		if err := Run(context.Background(), f.config, f.store, f.server.Client()); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		f.mu.Lock()
		attempts := f.secret["GITLAB_ISSUE_ATTEMPTS"]
		issued := f.issued
		clear(f.tokens)
		f.mu.Unlock()
		if attempts != "0" || issued != round+1 {
			t.Fatalf("round %d: attempts=%s issued=%d", round, attempts, issued)
		}
	}
}

func TestExternalBootstrapResetsBudgetForValidSavedToken(t *testing.T) {
	f := newBootstrapFixture(t)
	if err := Run(context.Background(), f.config, f.store, f.server.Client()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.secret["GITLAB_ISSUE_ATTEMPTS"] = "3"
	f.mu.Unlock()
	if err := Run(context.Background(), f.config, f.store, f.server.Client()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.secret["GITLAB_ISSUE_ATTEMPTS"] != "0" || f.issued != 1 || f.revoked != 0 {
		t.Fatal("valid saved token did not reset budget without replacement")
	}
}

func TestExternalBootstrapPreservesExhaustedBudget(t *testing.T) {
	f := newBootstrapFixture(t)
	f.secret["GITLAB_ISSUE_ATTEMPTS"] = "2"
	f.loseIssue = true
	for range 2 {
		if err := Run(context.Background(), f.config, f.store, f.server.Client()); err == nil {
			t.Fatal("expected exhausted issuance budget")
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.secret["GITLAB_ISSUE_ATTEMPTS"] != "3" || f.issued != 1 || f.revoked != 1 || len(f.tokens) != 0 {
		t.Fatal("rerun reset the exhausted budget or retained an orphan token")
	}
}

func TestExternalBootstrapRejectsClientSecretDrift(t *testing.T) {
	for _, tc := range []struct {
		name, application, key string
	}{
		{name: "oidc", application: "forge-oidc", key: "OIDC_CLIENT_SECRET"},
		{name: "management", application: "forge-management", key: "CASDOOR_SECRET"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBootstrapFixture(t)
			if err := Run(context.Background(), f.config, f.store, f.server.Client()); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			original := f.secret[tc.key]
			drifted := strings.Repeat("drifted-", 8)
			f.apps["admin/"+tc.application].ClientSecret = drifted
			f.secret["BOOTSTRAP_COMPLETE"] = "false"
			f.mu.Unlock()

			err := Run(context.Background(), f.config, f.store, f.server.Client())
			if err == nil {
				t.Fatal("client secret drift accepted")
			}
			if strings.Contains(err.Error(), original) || strings.Contains(err.Error(), drifted) {
				t.Fatal("client secret disclosed in error")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.secret["BOOTSTRAP_COMPLETE"] == "true" || f.secret[tc.key] != original {
				t.Fatal("conflicting credentials published")
			}
			if f.apps["admin/"+tc.application].ClientSecret != drifted {
				t.Fatal("remote client secret overwritten")
			}
			if f.issued != 1 || f.revoked != 0 {
				t.Fatal("GitLab credential changed after Casdoor conflict")
			}
		})
	}
}

func TestExternalBootstrapRecoversAmbiguousIssuance(t *testing.T) {
	f := newBootstrapFixture(t)
	f.loseIssue = true
	if err := Run(context.Background(), f.config, f.store, f.server.Client()); err != nil {
		t.Fatal(err)
	}
	if f.issued != 2 || f.revoked != 1 || len(f.tokens) != 1 {
		t.Fatalf(
			"orphan reconciliation failed issued=%d revoked=%d live=%d",
			f.issued,
			f.revoked,
			len(f.tokens),
		)
	}
}

func TestExternalBootstrapRecoversLostSecretResponse(t *testing.T) {
	f := newBootstrapFixture(t)
	f.loseSave = true
	if err := Run(context.Background(), f.config, f.store, f.server.Client()); err == nil {
		t.Fatal("expected ambiguous Secret write failure")
	}
	if err := Run(context.Background(), f.config, f.store, f.server.Client()); err != nil {
		t.Fatal(err)
	}
	if f.issued != 1 || f.revoked != 0 {
		t.Fatal("saved PAT was replaced")
	}
}

func TestExternalBootstrapValidation(t *testing.T) {
	f := newBootstrapFixture(t)
	valid := f.config
	valid.CasdoorURL = "http://casdoor.tenant-lab.svc.cluster.local:8000"
	valid.GitLabURL = "http://gitlab-webservice-default.tenant-lab.svc.cluster.local:8181"
	if err := valid.Validate(); err != nil {
		t.Fatalf("cluster HTTP service origins rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"credentials in URL": func(c *Config) { c.CasdoorURL = "http://user:pass@casdoor:8000" },
		"query in URL":       func(c *Config) { c.GitLabURL = "http://gitlab:8181?x=1" },
		"path in URL":        func(c *Config) { c.GitLabURL = "http://gitlab:8181/api" },
		"public plaintext":   func(c *Config) { c.PublicURL = "http://forge.example.test" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := valid
			mutate(&bad)
			if bad.Validate() == nil {
				t.Fatal("invalid URL accepted")
			}
		})
	}
}

func TestExternalBootstrapFailsClosed(t *testing.T) {
	for _, name := range []string{"management-permission", "secret-permission", "active-lease", "attempt-limit", "application-conflict"} {
		t.Run(name, func(t *testing.T) {
			f := newBootstrapFixture(t)
			switch name {
			case "management-permission":
				f.denyManagement = true
			case "secret-permission":
				f.denySecretPatch = true
			case "active-lease":
				f.annotations[leaseKey] = "another-job"
				f.annotations[leaseUntilKey] = time.Now().Add(time.Hour).Format(time.RFC3339)
			case "attempt-limit":
				f.secret["GITLAB_ISSUE_ATTEMPTS"] = "3"
			case "application-conflict":
				f.apps["admin/forge-oidc"] = &casdoorsdk.Application{Organization: "shared-org"}
			}
			if err := Run(context.Background(), f.config, f.store, f.server.Client()); err == nil {
				t.Fatal("expected failure")
			}
			if f.issued != 0 || f.secret["BOOTSTRAP_COMPLETE"] == "true" {
				t.Fatal("failed bootstrap issued PAT or marked complete")
			}
		})
	}
}

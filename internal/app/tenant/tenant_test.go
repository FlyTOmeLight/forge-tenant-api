package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	frameworkdb "github.com/FlyTOmeLight/server-go/db"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

type fixture struct {
	a                       *Service
	operator, member, other string
	ctx                     context.Context
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for real PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	conn, e := openTestDB(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "test_" + Token()[:16]
	if e = conn.WithContext(ctx).Exec(`CREATE SCHEMA ` + schema).Error; e != nil {
		t.Fatal(e)
	}
	parsed, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := parsed.Query()
	q.Set("search_path", schema)
	parsed.RawQuery = q.Encode()
	a, e := newTestService(
		ctx,
		config.Config{
			DatabaseURL:  parsed.String(),
			AppKey:       strings.Repeat("ab", 32),
			PublicURL:    "http://127.0.0.1:8383",
			Issuer:       "https://identity.test",
			ServiceToken: strings.Repeat("test", 16),
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		closeTestDB(a.DB)
		a.Casdoor.HTTP.CloseIdleConnections()
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = conn.WithContext(cleanup).Exec(`DROP SCHEMA ` + schema + ` CASCADE`).Error
		closeTestDB(conn)
	})
	if e = table.Migrate(ctx, a.DB); e != nil {
		t.Fatal(e)
	}
	f := &fixture{a: a, ctx: ctx, operator: Token(), member: Token(), other: Token()}
	for i, item := range []struct {
		id, token string
		op        bool
	}{{id: "operator", token: f.operator, op: true}, {id: "member", token: f.member, op: false}, {id: "other", token: f.other, op: false}} {
		e = a.DB.WithContext(ctx).
			Exec(`INSERT INTO users(id,issuer,subject,username,name,email,status,operator) VALUES(?,'https://identity.test',?,?,?,?,'active',?)`, item.id, item.id, item.id, item.id, fmt.Sprintf("u%d@example.test", i), item.op).
			Error
		if e != nil {
			t.Fatal(e)
		}
		e = a.DB.WithContext(ctx).
			Exec(`INSERT INTO sessions(token_hash,user_id,csrf_hash,expires_at) VALUES(?,?,?,now()+interval '1 hour')`, Hash(item.token), item.id, Hash(Hash(item.token+":csrf"))).
			Error
		if e != nil {
			t.Fatal(e)
		}
	}
	return f
}

func (f *fixture) call(t *testing.T, token, method, path string, b any, v int64, key string) (int, Object) {
	t.Helper()
	raw, _ := json.Marshal(b)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.AddCookie(&http.Cookie{Name: "forge_session", Value: token})
	r.Header.Set("Origin", f.a.Config.PublicURL)
	r.Header.Set("X-CSRF-Token", Hash(token+":csrf"))
	r.Header.Set("If-Match", fmt.Sprint(v))
	if key == "" {
		key = Token()
	}
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	apiHandler(t, f.a).ServeHTTP(w, r)
	out := Object{}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatalf("non-JSON response status %d", w.Code)
	}
	return w.Code, out
}

func (f *fixture) tenant(t *testing.T) (string, int64) {
	t.Helper()
	code, b := f.call(
		t,
		f.operator,
		"POST",
		"/api/v1/platform/tenants",
		Object{"admin_user_id": "operator", "name": "测试企业", "slug": "tenant-" + Token()[:8]},
		0,
		"",
	)
	if code != 202 {
		t.Fatalf("create status %d: %v", code, b)
	}
	return b["tenant_id"].(string), int64(b["version"].(float64))
}

func TestIdempotencyAndTenantIsolation(t *testing.T) {
	f := setup(t)
	key := Token()
	body := Object{"admin_user_id": "operator", "name": "Example", "slug": "example"}
	code, first := f.call(t, f.operator, "POST", "/api/v1/platform/tenants", body, 0, key)
	if code != 202 {
		t.Fatal(code, first)
	}
	code, second := f.call(t, f.operator, "POST", "/api/v1/platform/tenants", body, 0, key)
	if code != 202 || first["tenant_id"] != second["tenant_id"] || first["operation_id"] != second["operation_id"] {
		t.Fatal("idempotency did not preserve result")
	}
	code, _ = f.call(
		t,
		f.operator,
		"POST",
		"/api/v1/platform/tenants",
		Object{"admin_user_id": "operator", "name": "Other", "slug": "other"},
		0,
		key,
	)
	if code != 409 {
		t.Fatal("reused key must reject different content", code)
	}
	code, _ = f.call(t, f.member, "GET", "/api/v1/tenants/"+first["tenant_id"].(string), nil, 0, "")
	if code != 404 {
		t.Fatal("non-member could read tenant", code)
	}
	code, _ = f.call(
		t,
		f.member,
		"POST",
		"/api/v1/tenants/"+first["tenant_id"].(string)+"/teams",
		Object{"name": "x"},
		1,
		"",
	)
	if code != 404 {
		t.Fatal("non-member could mutate tenant", code)
	}
}

func TestInvitationRequiresIntendedIdentityAndSingleUse(t *testing.T) {
	f := setup(t)
	tid, v := f.tenant(t)
	code, b := f.call(
		t,
		f.operator,
		"POST",
		"/api/v1/tenants/"+tid+"/members",
		Object{"name": "member", "email": "u1@example.test", "role": "member"},
		v,
		"",
	)
	if code != 202 {
		t.Fatal(code, b)
	}
	link, _ := url.Parse(b["invitation_url"].(string))
	body := Object{"token": link.Fragment}
	code, _ = f.call(t, f.other, "POST", "/api/v1/invitations/accept", body, 0, "")
	if code != 403 {
		t.Fatal("wrong identity accepted invite", code)
	}
	code, _ = f.call(t, f.member, "POST", "/api/v1/invitations/accept", body, 0, "")
	if code != 202 {
		t.Fatal("intended identity cannot accept", code)
	}
	code, _ = f.call(t, f.member, "POST", "/api/v1/invitations/accept", body, 0, "")
	if code != 410 {
		t.Fatal("invite replay accepted", code)
	}
}

func TestAuthorizationIncreaseWaitsForCurrentConfirmation(t *testing.T) {
	f := setup(t)
	tid, v := f.tenant(t)
	e := f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('m',?,'member','member','active');`, tid).
		Error
	if e != nil {
		t.Fatal(e)
	}
	e = f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO projects(id,tenant_id,name,path,mode) VALUES('p',?,'p','x/p','create')`, tid).
		Error
	if e != nil {
		t.Fatal(e)
	}
	e = f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO grants(tenant_id,project_id,membership_id,desired_level,observed_level,effective_level) VALUES(?,'p','m',20,20,20)`, tid).
		Error
	if e != nil {
		t.Fatal(e)
	}
	if err := f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO direct_grants SELECT tenant_id,project_id,membership_id,desired_level FROM grants WHERE desired_level>0 ON CONFLICT DO NOTHING`).
		Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/tenants/" + tid + "/projects/p/grants/m"
	code, b := f.call(t, f.operator, "PUT", path, Object{"role": "developer"}, v, "")
	if code != 202 {
		t.Fatal(code, b)
	}
	v = int64(b["version"].(float64))
	access, e := f.a.access(f.ctx, "member", tid, "p")
	if e != nil || access["write"] != false {
		t.Fatal("unconfirmed increase permitted write", access, e)
	}
	if e = f.a.recordAccess(f.ctx, tid, v-1, "p", "m", 30, true); e != nil {
		t.Fatal(e)
	}
	access, _ = f.a.access(f.ctx, "member", tid, "p")
	if access["write"] != false {
		t.Fatal("stale confirmation restored write")
	}
	if e = f.a.recordAccess(f.ctx, tid, v, "p", "m", 30, true); e != nil {
		t.Fatal(e)
	}
	access, _ = f.a.access(f.ctx, "member", tid, "p")
	if access["write"] != true {
		t.Fatal("current confirmation did not enable write")
	}
	code, b = f.call(t, f.operator, "PUT", path, Object{"role": "reviewer"}, v, "")
	if code != 202 {
		t.Fatal(code, b)
	}
	v = int64(b["version"].(float64))
	access, _ = f.a.access(f.ctx, "member", tid, "p")
	if access["write"] != false {
		t.Fatal("downgrade not immediate")
	}
	code, b = f.call(t, f.operator, "PUT", path, Object{"role": "developer"}, v, "")
	if code != 202 {
		t.Fatal(code, b)
	}
	access, _ = f.a.access(f.ctx, "member", tid, "p")
	if access["write"] != false {
		t.Fatal("re-upgrade trusted old observation")
	}
	v = int64(b["version"].(float64))
	code, b = f.call(t, f.operator, "DELETE", path, nil, v, "")
	if code != 202 {
		t.Fatal(code, b)
	}
	access, _ = f.a.access(f.ctx, "member", tid, "p")
	if access["allowed"] != false {
		t.Fatal("revoke did not take immediate effect")
	}
}

func TestConcurrentAdminRemovalPreservesLastAdmin(t *testing.T) {
	f := setup(t)
	tid, v := f.tenant(t)
	e := f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('second',?,'member','admin','active')`, tid).
		Error
	if e != nil {
		t.Fatal(e)
	}
	var original string
	if e = f.a.DB.WithContext(f.ctx).
		Raw(`SELECT id FROM memberships WHERE tenant_id=? AND user_id='operator'`, tid).
		Row().
		Scan(&original); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, mid := range []string{original, "second"} {
		wg.Add(1)
		go func(mid string) {
			defer wg.Done()
			code, _ := f.call(t, f.operator, "DELETE", "/api/v1/tenants/"+tid+"/members/"+mid, nil, v, "")
			codes <- code
		}(mid)
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for c := range codes {
		if c == 202 {
			success++
		} else if c == 412 || c == 404 {
			conflict++
		} else {
			t.Fatal("unexpected response", c)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent mutations were not serialized")
	}
	var remaining string
	if e = f.a.DB.WithContext(f.ctx).
		Raw(`SELECT id FROM memberships WHERE tenant_id=? AND status='active' AND role='admin'`, tid).
		Row().
		Scan(&remaining); e != nil {
		t.Fatal(e)
	}
	session := f.operator
	if remaining == "second" {
		session = f.member
	}
	code, _ := f.call(t, session, "DELETE", "/api/v1/tenants/"+tid+"/members/"+remaining, nil, v+1, "")
	if code != 409 {
		t.Fatal("last admin removed", code)
	}
}

func TestCSRFAndDisabledSession(t *testing.T) {
	f := setup(t)
	r := httptest.NewRequest("POST", "/api/v1/platform/tenants", strings.NewReader(`{"name":"bad","slug":"bad"}`))
	r.AddCookie(&http.Cookie{Name: "forge_session", Value: f.operator})
	w := httptest.NewRecorder()
	apiHandler(t, f.a).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("CSRF not rejected", w.Code)
	}
	e := f.a.DB.WithContext(f.ctx).Exec(`UPDATE users SET status='disabled' WHERE id='member'`).Error
	if e != nil {
		t.Fatal(e)
	}
	code, _ := f.call(t, f.member, "GET", "/api/v1/me", nil, 0, "")
	if code != 401 {
		t.Fatal("disabled session accepted", code)
	}
}

func TestRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		attempts int
		status   string
	}{{name: "success", err: nil, attempts: 1, status: "succeeded"}, {name: "stale", err: errSuperseded, attempts: 1, status: "superseded"}, {name: "credential", err: &client.Error{Status: 401}, attempts: 1, status: "failed"}, {name: "effective conflict", err: &client.Error{Status: 409}, attempts: 1, status: "conflict"}, {name: "limited", err: &client.Error{Status: 429, RetryAfter: time.Minute}, attempts: 1, status: "retrying"}, {name: "exhausted", err: &client.Error{Status: 503}, attempts: 8, status: "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			status, _, _, delay := syncResult(tc.err, tc.attempts)
			if status != tc.status {
				t.Fatalf("status %s", status)
			}
			if tc.name == "limited" && delay != time.Minute {
				t.Fatal("Retry-After not respected")
			}
		})
	}
}

func TestProjectAdminCannotManageOtherProjectsOrEnterprise(t *testing.T) {
	f := setup(t)
	tid, v := f.tenant(t)
	e := f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('m',?,'member','member','active'),('otherm',?,'other','member','active')`, tid, tid).
		Error
	if e != nil {
		t.Fatal(e)
	}
	e = f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO projects(id,tenant_id,name,path,mode) VALUES('p',?,'p','scope/p','create'),('otherp',?,'other','scope/other','create')`, tid, tid).
		Error
	if e != nil {
		t.Fatal(e)
	}
	e = f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO grants(tenant_id,project_id,membership_id,desired_level,observed_level,effective_level) VALUES(?,'p','m',40,40,40)`, tid).
		Error
	if e != nil {
		t.Fatal(e)
	}
	if err := f.a.DB.WithContext(f.ctx).
		Exec(`INSERT INTO direct_grants SELECT tenant_id,project_id,membership_id,desired_level FROM grants WHERE desired_level>0 ON CONFLICT DO NOTHING`).
		Error; err != nil {
		t.Fatal(err)
	}
	code, b := f.call(t, f.member, "GET", "/api/v1/tenants/"+tid+"/projects/p/grant-candidates", nil, 0, "")
	if code != 200 || len(b["items"].([]any)) != 3 {
		t.Fatal("project admin cannot find candidates", code)
	}
	code, _ = f.call(t, f.member, "GET", "/api/v1/tenants/"+tid+"/projects/otherp/grant-candidates", nil, 0, "")
	if code != 404 {
		t.Fatal("other project candidates leaked", code)
	}
	code, b = f.call(
		t,
		f.member,
		"PUT",
		"/api/v1/tenants/"+tid+"/projects/p/grants/otherm",
		Object{"role": "developer"},
		v,
		"",
	)
	if code != 202 {
		t.Fatal("project admin cannot assign scoped grant", code, b)
	}
	code, _ = f.call(
		t,
		f.member,
		"POST",
		"/api/v1/tenants/"+tid+"/members",
		Object{"name": "x", "email": "new@example.test", "role": "admin"},
		v+1,
		"",
	)
	if code != 404 {
		t.Fatal("project admin can invite enterprise admin", code)
	}
}

func newTestService(ctx context.Context, c config.Config) (*Service, error) {
	db, err := openTestDB(ctx, c.DatabaseURL)
	if err != nil {
		return nil, err
	}
	transport, err := client.NewHTTP(c.CAFile)
	if err != nil {
		closeTestDB(db)
		return nil, err
	}
	service, err := New(
		db,
		c,
		&client.Casdoor{
			URL:          c.CasdoorURL,
			ClientID:     c.CasdoorClient,
			Secret:       c.CasdoorSecret,
			Organization: c.Organization,
			HTTP:         transport,
		},
		&client.GitLab{URL: c.GitLabURL, Token: c.GitLabToken, TokenFile: c.GitLabTokenFile, HTTP: transport},
		&client.OIDC{HTTP: transport},
	)
	if err != nil {
		closeTestDB(db)
		transport.CloseIdleConnections()
	}
	return service, err
}

func openTestDB(ctx context.Context, dsn string) (*gorm.DB, error) {
	database, err := frameworkdb.NewDB(
		ctx,
		&frameworkdb.Config{Driver: frameworkdb.DriverPostgreSQL, DSN: dsn, MaxOpenConns: 12, MaxIdleConns: 12},
	)
	if err == nil {
		database.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	}
	return database, err
}

func closeTestDB(db *gorm.DB) {
	connection, err := db.DB()
	if err == nil {
		_ = connection.Close()
	}
}

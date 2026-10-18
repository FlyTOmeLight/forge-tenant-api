package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/credential"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

// internal posts a service-authenticated internal request.
func (f *fixture) internal(t *testing.T, path string, body Object) (int, Object) {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+f.a.ServiceKey)
	w := httptest.NewRecorder()
	apiHandler(t, f.a).ServeHTTP(w, r)
	out := Object{}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatalf("non-JSON internal response status %d", w.Code)
	}
	return w.Code, out
}

// read presents either a session cookie or a user API key on a business route.
func (f *fixture) read(t *testing.T, credential, path string) (int, Object) {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	if strings.HasPrefix(credential, "ftk_") {
		r.Header.Set("Authorization", "Bearer "+credential)
	} else if credential != "" {
		r.AddCookie(&http.Cookie{Name: "forge_session", Value: credential})
	}
	w := httptest.NewRecorder()
	apiHandler(t, f.a).ServeHTTP(w, r)
	out := Object{}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatalf("non-JSON response status %d", w.Code)
	}
	return w.Code, out
}

func code(t *testing.T, body Object) string {
	t.Helper()
	failure, ok := body["error"].(Object)
	if !ok {
		t.Fatalf("missing error envelope: %v", body)
	}
	value, _ := failure["code"].(string)
	return value
}

func TestIdentityResolveAcceptsOneProofOnly(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		name string
		body Object
		want int
		code string
		user string
	}{
		{
			name: "session read",
			body: Object{"type": "session", "session": f.member, "method": "GET"},
			want: 200, user: "member",
		},
		{
			name: "session write with csrf",
			body: Object{
				"type": "session", "session": f.member, "method": "POST",
				"origin": f.a.Config.PublicURL, "csrf": identity.Hash(f.member + ":csrf"),
			},
			want: 200, user: "member",
		},
		{
			name: "session write with wrong csrf",
			body: Object{
				"type": "session", "session": f.member, "method": "POST",
				"origin": f.a.Config.PublicURL, "csrf": "wrong",
			},
			want: 403, code: "CSRF_REJECTED",
		},
		{
			name: "session write from foreign origin",
			body: Object{
				"type": "session", "session": f.member, "method": "POST",
				"origin": "https://evil.test", "csrf": identity.Hash(f.member + ":csrf"),
			},
			want: 403, code: "CSRF_REJECTED",
		},
		{
			name: "user api key",
			body: Object{"type": "api_key", "api_key": f.a.userKey},
			want: 200, user: "member",
		},
		{
			name: "unknown api key",
			body: Object{"type": "api_key", "api_key": "ftk_" + identity.Token()},
			want: 401, code: "AUTH_REQUIRED",
		},
		{
			name: "service key is not a user",
			body: Object{"type": "api_key", "api_key": f.a.ServiceKey},
			want: 401, code: "AUTH_REQUIRED",
		},
		{
			name: "two proofs",
			body: Object{"type": "session", "session": f.member, "method": "GET", "api_key": f.a.userKey},
			want: 400, code: "AMBIGUOUS_IDENTITY_PROOF",
		},
		{
			name: "unknown proof type",
			body: Object{"type": "cookie", "session": f.member},
			want: 400, code: "INVALID_IDENTITY_PROOF",
		},
		{
			name: "self reported user",
			body: Object{"type": "session", "session": f.member, "method": "GET", "user_id": "operator"},
			want: 400, code: "INVALID_JSON",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := f.internal(t, "/internal/v1/identity/resolve", tc.body)
			if status != tc.want {
				t.Fatalf("status %d: %v", status, body)
			}
			if tc.code != "" {
				if got := code(t, body); got != tc.code {
					t.Fatalf("error code %q: %v", got, body)
				}
				return
			}
			if id, _ := body["id"].(string); id != tc.user {
				t.Fatalf("resolved %q: %v", id, body)
			}
		})
	}
}

func TestIdentityMiddlewareResolvesCookieAndAPIKey(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		name       string
		credential string
		want       int
	}{
		{name: "session cookie", credential: f.member, want: 200},
		{name: "user api key", credential: f.a.userKey, want: 200},
		{name: "no credential", credential: "", want: 401},
		{name: "service key on business route", credential: f.a.ServiceKey, want: 401},
		{name: "revoked api key", credential: "ftk_" + identity.Token(), want: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := f.read(t, tc.credential, "/api/v1/tenants")
			if status != tc.want {
				t.Fatalf("status %d: %v", status, body)
			}
			if tc.want == 401 && code(t, body) != "AUTH_REQUIRED" {
				t.Fatalf("unexpected error: %v", body)
			}
		})
	}
	r := httptest.NewRequest("GET", "/api/v1/tenants", nil)
	r.AddCookie(&http.Cookie{Name: "forge_session", Value: f.member})
	r.Header.Set("Authorization", "Bearer "+f.a.userKey)
	w := httptest.NewRecorder()
	apiHandler(t, f.a).ServeHTTP(w, r)
	var body Object
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Code != 400 ||
		code(t, body) != "AMBIGUOUS_IDENTITY_PROOF" {
		t.Fatalf("two credentials must be rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestCredentialLifecycleIsSessionOnly(t *testing.T) {
	f := setup(t)
	status, created := f.call(t, f.member, "POST", "/api/v1/credentials", Object{"name": "cli"}, 0, "")
	if status != 201 {
		t.Fatalf("create status %d: %v", status, created)
	}
	token, _ := created["token"].(string)
	if !strings.HasPrefix(token, "ftk_") {
		t.Fatalf("token is not a bearer API key: %v", created)
	}
	id, _ := created["id"].(string)
	if got, _ := f.read(t, token, "/api/v1/tenants"); got != 200 {
		t.Fatalf("issued key rejected: %d", got)
	}
	status, listed := f.call(t, f.member, "GET", "/api/v1/credentials", nil, 0, "")
	if status != 200 {
		t.Fatalf("list status %d: %v", status, listed)
	}
	items, _ := listed["items"].([]any)
	if len(items) == 0 {
		t.Fatal("created credential missing from the list")
	}
	raw, _ := json.Marshal(listed)
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), "secret_hash") {
		t.Fatal("credential listing must never return the token")
	}
	if status, other := f.call(t, f.operator, "DELETE", "/api/v1/credentials/"+id, nil, 0, ""); status != 404 {
		t.Fatalf("another user revoked the credential: %d %v", status, other)
	}
	if status, _ = f.call(t, f.member, "DELETE", "/api/v1/credentials/"+id, nil, 0, ""); status != 200 {
		t.Fatalf("revoke status %d", status)
	}
	if got, _ := f.read(t, token, "/api/v1/tenants"); got != 401 {
		t.Fatalf("revoked key still accepted: %d", got)
	}
	if status, _ = f.call(t, f.member, "DELETE", "/api/v1/credentials/"+id, nil, 0, ""); status != 200 {
		t.Fatalf("repeated revoke must succeed, got %d", status)
	}
	r := httptest.NewRequest("POST", "/api/v1/credentials", strings.NewReader(`{"name":"cli"}`))
	r.Header.Set("Authorization", "Bearer "+f.a.userKey)
	w := httptest.NewRecorder()
	apiHandler(t, f.a).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("api key must not manage credentials: %d %s", w.Code, w.Body.String())
	}
	if status, body := f.call(
		t,
		f.member,
		"POST",
		"/api/v1/credentials",
		Object{"name": "cli", "expires_at": "2030-01-01T00:00:00Z"},
		0,
		"",
	); status != 422 || code(t, body) != "INVALID_EXPIRY" {
		t.Fatalf("far future expiry accepted: %d %v", status, body)
	}
}

func TestCredentialRevocationDoesNotDependOnListLimit(t *testing.T) {
	f := setup(t)
	var id string
	if err := f.a.DB.WithContext(f.ctx).Raw(
		`UPDATE credentials SET created_at=now()-interval '1 day' WHERE owner_kind='user' AND user_id='member' RETURNING id`,
	).
		Row().Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := f.a.DB.WithContext(f.ctx).Exec(
		`INSERT INTO credentials(id,owner_kind,user_id,name,secret_hash,expires_at) SELECT 'extra_'||n,'user','member','extra','extra_hash_'||n,now()+interval '1 day' FROM generate_series(1,201) n`,
	).
		Error; err != nil {
		t.Fatal(err)
	}
	items, err := credential.New(f.a.DB).ListUser(f.ctx, "member")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == id {
			t.Fatal("old credential should be outside the list page")
		}
	}
	path := "/api/v1/credentials/" + id
	if status, body := f.call(t, f.member, "DELETE", path, nil, 0, ""); status != 200 {
		t.Fatalf("old credential cannot be revoked: %d %v", status, body)
	}
	if status, _ := f.read(t, f.a.userKey, "/api/v1/tenants"); status != 401 {
		t.Fatalf("old credential still accepted: %d", status)
	}
	var revokedAt time.Time
	if err := f.a.DB.WithContext(f.ctx).
		Raw(`SELECT revoked_at FROM credentials WHERE id=?`, id).
		Row().
		Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	if status, body := f.call(t, f.member, "DELETE", path, nil, 0, ""); status != 200 {
		t.Fatalf("repeated revoke failed: %d %v", status, body)
	}
	var repeatedAt time.Time
	if err := f.a.DB.WithContext(f.ctx).
		Raw(`SELECT revoked_at FROM credentials WHERE id=?`, id).
		Row().
		Scan(&repeatedAt); err != nil {
		t.Fatal(err)
	}
	if !revokedAt.Equal(repeatedAt) {
		t.Fatal("repeated revoke changed the original revocation time")
	}
}

func TestServiceCredentialRevocationStaysWithinServiceOwners(t *testing.T) {
	f := setup(t)
	credentials := credential.New(f.a.DB)
	var userID, serviceID string
	if err := f.a.DB.WithContext(f.ctx).
		Raw(`SELECT id FROM credentials WHERE secret_hash=?`, identity.Hash(f.a.userKey)).
		Row().
		Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if revoked, err := credentials.RevokeService(f.ctx, userID); err != nil || revoked {
		t.Fatalf("service command revoked a user credential: %v %v", revoked, err)
	}
	if status, _ := f.read(t, f.a.userKey, "/api/v1/tenants"); status != 200 {
		t.Fatalf("user credential affected by service revoke: %d", status)
	}
	if err := f.a.DB.WithContext(f.ctx).
		Raw(`SELECT id FROM credentials WHERE secret_hash=?`, identity.Hash(f.a.ServiceKey)).
		Row().
		Scan(&serviceID); err != nil {
		t.Fatal(err)
	}
	if revoked, err := credentials.RevokeService(f.ctx, serviceID); err != nil || !revoked {
		t.Fatalf("service revoke failed: %v %v", revoked, err)
	}
	if status, body := f.internal(
		t,
		"/internal/v1/identity/resolve",
		Object{"type": "session", "session": f.member, "method": "GET"},
	); status != 401 ||
		code(t, body) != "SERVICE_AUTH_REQUIRED" {
		t.Fatalf("revoked service credential accepted: %d %v", status, body)
	}
}

func TestCredentialOwnerConstraints(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		name, kind, user, service string
	}{
		{name: "missing user", kind: "user", user: "missing"},
		{name: "user kind with service owner", kind: "user", service: "test-service"},
		{name: "service kind with user owner", kind: "service", user: "member"},
		{name: "two owners", kind: "user", user: "member", service: "test-service"},
		{name: "no owner", kind: "service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := table.InsertCredential(
				f.ctx,
				f.a.DB,
				identity.ID("cr"),
				tc.kind,
				tc.user,
				tc.service,
				"invalid",
				identity.Hash(identity.Token()),
				time.Now().Add(time.Hour),
			)
			if err == nil {
				t.Fatal("invalid credential owner accepted")
			}
		})
	}
}

func TestLogoutAndRevocationStayIndependent(t *testing.T) {
	f := setup(t)
	status, created := f.call(t, f.operator, "POST", "/api/v1/credentials", Object{"name": "cli"}, 0, "")
	if status != 201 {
		t.Fatalf("create status %d: %v", status, created)
	}
	token, _ := created["token"].(string)
	id, _ := created["id"].(string)
	if status, _ = f.call(t, f.operator, "DELETE", "/api/v1/credentials/"+id, nil, 0, ""); status != 200 {
		t.Fatalf("revoke status %d", status)
	}
	if got, _ := f.read(t, f.operator, "/api/v1/tenants"); got != 200 {
		t.Fatalf("revoking an api key ended the session: %d", got)
	}
	if got, _ := f.read(t, token, "/api/v1/tenants"); got != 401 {
		t.Fatalf("revoked key still accepted: %d", got)
	}
	if status, _ = f.call(t, f.member, "POST", "/auth/logout", nil, 0, ""); status != 200 {
		t.Fatalf("logout status %d", status)
	}
	if got, _ := f.read(t, f.member, "/api/v1/tenants"); got != 401 {
		t.Fatalf("logged out session still accepted: %d", got)
	}
	if got, _ := f.read(t, f.a.userKey, "/api/v1/tenants"); got != 200 {
		t.Fatalf("logout revoked the api key: %d", got)
	}
}

func TestIdentityRejectsInactiveOwners(t *testing.T) {
	f := setup(t)
	if e := f.a.DB.Exec(`UPDATE users SET status='disabled' WHERE id='member'`).Error; e != nil {
		t.Fatal(e)
	}
	if status, _ := f.read(t, f.member, "/api/v1/tenants"); status != 401 {
		t.Fatalf("disabled user session accepted: %d", status)
	}
	if status, _ := f.read(t, f.a.userKey, "/api/v1/tenants"); status != 401 {
		t.Fatalf("disabled user api key accepted: %d", status)
	}
}

func TestExpiredCredentialIsRejected(t *testing.T) {
	f := setup(t)
	token := "ftk_" + identity.Token()
	if e := f.a.DB.Exec(
		`INSERT INTO credentials(id,owner_kind,user_id,name,secret_hash,created_at,expires_at) VALUES(?,'user','member','expired',?,now(),now()-interval '1 hour')`,
		identity.ID("cr"),
		identity.Hash(token),
	).Error; e != nil {
		t.Fatal(e)
	}
	if status, _ := f.read(t, token, "/api/v1/tenants"); status != 401 {
		t.Fatalf("expired key accepted: %d", status)
	}
}

// TestDownstreamMiddlewareResolvesIdentity drives a simulated business service
// that forwards the caller's credential to forge-tenant through the SDK.
func TestDownstreamMiddlewareResolvesIdentity(t *testing.T) {
	f := setup(t)
	server := httptest.NewServer(apiHandler(t, f.a))
	t.Cleanup(server.Close)
	client, err := tenantv1.New(tenantv1.Config{Endpoint: server.URL, ServiceAPIKey: f.a.ServiceKey})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	reached := false
	downstream := tenantv1.Identity(tenantv1.MiddlewareConfig{Client: client})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = true
			principal, ok := tenantv1.PrincipalFromContext(r.Context())
			if !ok {
				t.Error("middleware did not publish the principal")
				w.WriteHeader(500)
				return
			}
			_, _ = w.Write([]byte(principal.ID))
		}),
	)
	for _, tc := range []struct {
		name       string
		credential string
		method     string
		csrf       string
		want       int
		body       string
		code       string
	}{
		{name: "session cookie", credential: f.member, method: "GET", want: 200, body: "member"},
		{name: "user api key", credential: f.a.userKey, method: "GET", want: 200, body: "member"},
		{name: "no credential", method: "GET", want: 401, code: "AUTH_REQUIRED"},
		{name: "write without csrf", credential: f.member, method: "POST", want: 403, code: "CSRF_REJECTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reached = false
			r := httptest.NewRequest(tc.method, "/documents", nil)
			r.Header.Set("Origin", f.a.Config.PublicURL)
			if tc.csrf != "" {
				r.Header.Set("X-CSRF-Token", tc.csrf)
			}
			if strings.HasPrefix(tc.credential, "ftk_") {
				r.Header.Set("Authorization", "Bearer "+tc.credential)
			} else if tc.credential != "" {
				r.AddCookie(&http.Cookie{Name: "forge_session", Value: tc.credential})
			}
			w := httptest.NewRecorder()
			downstream.ServeHTTP(w, r)
			if w.Code != tc.want || (tc.body != "" && w.Body.String() != tc.body) {
				t.Fatalf("response %d: %s", w.Code, w.Body.String())
			}
			if tc.code != "" {
				var failure Object
				if json.Unmarshal(w.Body.Bytes(), &failure) != nil || code(t, failure) != tc.code {
					t.Fatalf("error code mismatch: %s", w.Body.String())
				}
				if reached {
					t.Fatal("rejected request reached the downstream handler")
				}
			}
		})
	}
	unavailable := httptest.NewServer(http.NotFoundHandler())
	unavailable.Close()
	dead, err := tenantv1.New(tenantv1.Config{Endpoint: unavailable.URL, ServiceAPIKey: f.a.ServiceKey})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dead.Close() })
	reached = false
	handler := tenantv1.Identity(tenantv1.MiddlewareConfig{Client: dead})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }),
	)
	r := httptest.NewRequest("GET", "/documents", nil)
	r.AddCookie(&http.Cookie{Name: "forge_session", Value: f.member})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 503 || reached {
		t.Fatalf("unavailable tenant must answer 503 without running the handler: %d", w.Code)
	}
}

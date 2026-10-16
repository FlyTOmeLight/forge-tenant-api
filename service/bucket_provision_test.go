package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/bucketprovision"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func bucketJob(t *testing.T, f *fixture, tid string) table.TenantBucketProvision {
	t.Helper()
	var j table.TenantBucketProvision
	if err := f.a.DB.WithContext(f.ctx).First(&j, "tenant_id=?", tid).Error; err != nil {
		t.Fatal(err)
	}
	return j
}

func TestBucketProvisionDurabilityAndRecovery(t *testing.T) {
	f := setup(t)
	tid, _ := f.tenant(t)
	if j := bucketJob(t, f, tid); j.Status != "pending" || j.Attempts != 0 {
		t.Fatal(j)
	}
	// A replay of the creation request cannot create multiple obligations.
	if err := table.EnqueueTenantBucket(f.ctx, f.a.DB, tid); err != nil {
		t.Fatal(err)
	}
	if count, err := table.BackfillTenantBuckets(f.ctx, f.a.DB); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			TenantID string `json:"tenantID"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.TenantID != tid || r.Header.Get("Authorization") != "Bearer storage-only" {
			t.Error("wrong tenant management request")
		}
		fmt.Fprintf(w, `{"tenantID":%q,"backendID":"jfs","bucket":"fixed"}`, tid)
	}))
	defer server.Close()
	c := &bucketprovision.Client{
		URL:     server.URL,
		Token:   "storage-only",
		HTTP:    server.Client(),
		Timeout: time.Second,
	}
	// Simulate a crash after claim and before remote acknowledgement.
	if err := f.a.DB.Exec(
		`UPDATE tenant_bucket_provisions SET status='running',attempts=1,lease_id='dead',lease_until=now()-interval '1 second' WHERE tenant_id=?`,
		tid,
	).Error; err != nil {
		t.Fatal(err)
	}
	restarted := &bucketprovision.Service{DB: f.a.DB, Client: c}
	if err := restarted.RunNext(f.ctx); err != nil {
		t.Fatal(err)
	}
	if j := bucketJob(t, f, tid); j.Status != "succeeded" || j.Attempts != 2 || j.Bucket != "fixed" {
		t.Fatal(j)
	}
	if err := restarted.RunNext(f.ctx); err != nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
	// Versioned GitLab operations never confirm or supersede the independent obligation.
	if err := f.a.DB.Exec(`UPDATE operations SET status='failed' WHERE tenant_id=?`, tid).Error; err != nil {
		t.Fatal(err)
	}
	if bucketJob(t, f, tid).Status != "succeeded" {
		t.Fatal("GitLab status changed bucket result")
	}
	changed, err := table.RetryFailedTenantBucket(f.ctx, f.a.DB, tid)
	if err != nil || changed {
		t.Fatal(changed, err)
	}
	// Existing tenant backfill is idempotent and does not need GitLab success.
	if err := f.a.DB.Exec(`DELETE FROM tenant_bucket_provisions WHERE tenant_id=?`, tid).Error; err != nil {
		t.Fatal(err)
	}
	for i, want := range []int64{1, 0} {
		n, err := table.BackfillTenantBuckets(f.ctx, f.a.DB)
		if err != nil || n != want {
			t.Fatal(i, n, err)
		}
	}
}

func TestBucketProvisionBoundedRetries(t *testing.T) {
	f := setup(t)
	tid, _ := f.tenant(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "private backend details", 503)
	}))
	defer server.Close()
	s := &bucketprovision.Service{
		DB: f.a.DB,
		Client: &bucketprovision.Client{
			URL:     server.URL,
			Token:   "storage-only",
			HTTP:    server.Client(),
			Timeout: time.Second,
		},
	}
	for attempt := 1; attempt <= bucketprovision.MaxAttempts; attempt++ {
		if err := s.RunNext(f.ctx); err == nil {
			t.Fatal("expected storage outage")
		}
		j := bucketJob(t, f, tid)
		if j.Attempts != attempt || strings.Contains(j.LastError, "private") {
			t.Fatal(j)
		}
		// No hot loop: next execution before its due time does not call remote again.
		if err := s.RunNext(f.ctx); err != nil || calls.Load() != int32(attempt) {
			t.Fatal(err, calls.Load())
		}
		if err := f.a.DB.Exec(
			`UPDATE tenant_bucket_provisions SET next_attempt_at=now() WHERE tenant_id=?`,
			tid,
		).Error; err != nil {
			t.Fatal(err)
		}
	}
	if bucketJob(t, f, tid).Status != "failed" {
		t.Fatal("failure budget not enforced")
	}
	changed, err := table.RetryFailedTenantBucket(f.ctx, f.a.DB, tid)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if j := bucketJob(t, f, tid); j.Status != "pending" || j.Attempts != 0 {
		t.Fatal(j)
	}
	// A final crashed lease is also bounded, and may be manually requeued.
	if err := f.a.DB.Exec(
		`UPDATE tenant_bucket_provisions SET status='running',attempts=?,lease_until=now()-interval '1 second' WHERE tenant_id=?`,
		bucketprovision.MaxAttempts,
		tid,
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.RunNext(f.ctx); err != nil {
		t.Fatal(err)
	}
	if bucketJob(t, f, tid).Status != "failed" || calls.Load() != bucketprovision.MaxAttempts {
		t.Fatal("expired final lease escaped budget")
	}
}

func TestBucketCreationRollback(t *testing.T) {
	f := setup(t)
	// A registration failure rolls back tenant, administrator and GitLab obligation.
	if err := f.a.DB.Exec(
		`ALTER TABLE tenant_bucket_provisions ADD CONSTRAINT reject_registration CHECK (tenant_id='rejected')`,
	).Error; err != nil {
		t.Fatal(err)
	}
	code, _ := f.call(
		t,
		f.operator,
		"POST",
		"/api/v1/platform/tenants",
		Object{"name": "rollback", "slug": "rollback", "admin_user_id": "operator"},
		0,
		identity.Token(),
	)
	if code != 409 {
		t.Fatal(code)
	}
	for _, name := range []string{"tenants", "memberships", "operations", "tenant_bucket_provisions"} {
		var n int64
		if err := f.a.DB.Table(name).Count(&n).Error; err != nil || n != 0 {
			t.Fatal(name, n, err)
		}
	}
}

func TestBucketWorkerIndependentOfGitLab(t *testing.T) {
	f := setup(t)
	tid, _ := f.tenant(t)
	complete := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/storage/tenant-buckets" {
			fmt.Fprintf(w, `{"tenantID":%q,"backendID":"jfs","bucket":"fixed"}`, tid)
			select {
			case complete <- struct{}{}:
			default:
			}
			return
		}
		http.Error(w, "gitlab unavailable", 503)
	}))
	defer server.Close()
	cfg := assemblyConfig(f.a.Config.DatabaseURL, server.URL)
	cfg.Storage = StorageConfig{URL: server.URL, ManagementToken: strings.Repeat("m", 32), Timeout: time.Second}
	worker, err := NewWorker(f.a.DB, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Start(f.ctx); err != nil {
		t.Fatal(err)
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() {
		if err := worker.Stop(shutdown); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-complete:
	case <-time.After(4 * time.Second):
		t.Fatal("GitLab failure blocked storage worker")
	}
}

func TestBucketLeasePreventsDuplicateClaimAndStaleAcknowledgement(t *testing.T) {
	f := setup(t)
	tid, _ := f.tenant(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var unblock sync.Once
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprintf(w, `{"tenantID":%q,"backendID":"jfs","bucket":"stale"}`, tid)
	}))
	defer func() { unblock.Do(func() { close(release) }); slow.Close() }()
	first := &bucketprovision.Service{
		DB:     f.a.DB,
		Client: &bucketprovision.Client{URL: slow.URL, HTTP: slow.Client(), Timeout: 5 * time.Second},
	}
	finished := make(chan error, 1)
	go func() { finished <- first.RunNext(f.ctx) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first claim did not reach HTTP")
	}
	// A second process observes the active lease and makes no duplicate request.
	if err := first.RunNext(f.ctx); err != nil {
		t.Fatal(err)
	}
	if bucketJob(t, f, tid).Attempts != 1 {
		t.Fatal("active lease claimed twice")
	}
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tenantID":%q,"backendID":"jfs","bucket":"confirmed"}`, tid)
	}))
	defer fast.Close()
	if err := f.a.DB.Exec(
		`UPDATE tenant_bucket_provisions SET lease_until=now()-interval '1 second' WHERE tenant_id=?`,
		tid,
	).Error; err != nil {
		t.Fatal(err)
	}
	second := &bucketprovision.Service{
		DB:     f.a.DB,
		Client: &bucketprovision.Client{URL: fast.URL, HTTP: fast.Client(), Timeout: time.Second},
	}
	if err := second.RunNext(f.ctx); err != nil {
		t.Fatal(err)
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("stale acknowledgement accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first worker did not finish")
	}
	if j := bucketJob(t, f, tid); j.Status != "succeeded" || j.Bucket != "confirmed" || j.Attempts != 2 {
		t.Fatal(j)
	}
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/tenant"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

func TestTenantServiceOwnsAuthorizationAndTransaction(t *testing.T) {
	f := setup(t)
	request := tenant.CreateTenantRequest{
		Name:        "Service tenant",
		Slug:        "service-tenant",
		AdminUserID: "operator",
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	change := tenantwrite.Change{Key: "direct-service", Route: "create-tenant", Target: "tenant", Raw: raw}
	member, _, err := f.a.Account.Authenticate(f.ctx, f.member)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.a.Tenant.CreateTenant(f.ctx, member, change, request)
	var denied *apperror.Error
	if !errors.As(err, &denied) || denied.Status != 403 {
		t.Fatalf("service authorization: %v", err)
	}
	operator, _, err := f.a.Account.Authenticate(f.ctx, f.operator)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.a.Tenant.CreateTenant(f.ctx, operator, change, request)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.a.Tenant.CreateTenant(f.ctx, operator, change, request)
	if err != nil || string(result.Data) != string(replay.Data) {
		t.Fatalf("service replay: %v", err)
	}
	var count int
	if err = f.a.DB.WithContext(f.ctx).
		Raw(`SELECT count(*) FROM tenants WHERE slug='service-tenant'`).
		Row().
		Scan(&count); err != nil ||
		count != 1 {
		t.Fatalf("duplicate tenant count=%d err=%v", count, err)
	}
	change.Key = identity.Token()
	change.Raw = []byte(`{"name":"","slug":"rejected"}`)
	request.Name = ""
	_, err = f.a.Tenant.CreateTenant(f.ctx, operator, change, request)
	if !errors.As(err, &denied) || denied.Status != 422 {
		t.Fatalf("validation: %v", err)
	}
	if err = f.a.DB.WithContext(f.ctx).
		Raw(`SELECT count(*) FROM operations`).
		Row().
		Scan(&count); err != nil ||
		count != 1 {
		t.Fatalf("failed change created outbox: count=%d err=%v", count, err)
	}
}

func TestWorkerStopWaitsForOwnedLoop(t *testing.T) {
	f := setup(t)
	worker := newWorker(f.a.Sync)
	if err := worker.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Stop(shutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.done:
	default:
		t.Fatal("stop returned before worker exited")
	}
	if err := worker.Stop(shutdown); err != nil {
		t.Fatal("second stop", err)
	}
}

func TestTypedWorkspaceRequestsRejectInvalidFields(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		name string
		body Object
	}{
		{name: "unknown", body: Object{"name": "Typed tenant", "slug": "typed-tenant", "admin_user_id": "operator", "extra": "ignored before"}},
		{name: "wrong type", body: Object{"name": 42, "slug": "typed-tenant", "admin_user_id": "operator"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, response := f.call(
				t,
				f.operator,
				"POST",
				"/api/v1/platform/tenants",
				tc.body,
				0,
				identity.Token(),
			)
			if status != 400 || response["error"].(map[string]any)["code"] != "INVALID_JSON" {
				t.Fatalf("typed request accepted: %d %+v", status, response)
			}
		})
	}
	var count int
	if err := f.a.DB.WithContext(f.ctx).
		Raw(`SELECT count(*) FROM tenants`).
		Row().
		Scan(&count); err != nil ||
		count != 0 {
		t.Fatalf("binding failure wrote tenant: count=%d err=%v", count, err)
	}
}

package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/credential"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func serviceToken(t *testing.T) string {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return "ftk_" + base64.RawURLEncoding.EncodeToString(secret)
}

func TestEnsureServiceCredentialPostgres(t *testing.T) {
	f := setup(t)
	svc := credential.New(f.a.DB)
	token := serviceToken(t)
	first, err := svc.EnsureService(f.ctx, "artifact", "helm", token, "")
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.EnsureService(f.ctx, "artifact", "helm", token, "")
	if err != nil || again.ID != first.ID || !again.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("idempotency failed: %v", err)
	}
	owner, err := table.ServiceByCredentialHash(f.ctx, f.a.DB, identity.Hash(token))
	if err != nil || owner != "artifact" {
		t.Fatalf("registered token cannot authenticate: %v", err)
	}
	for _, tc := range []struct{ name, service, credentialName, token string }{
		{"different token", "artifact", "helm", serviceToken(t)},
		{"different service", "other-service", "helm", token},
		{"different name", "artifact", "different", token},
		{"malformed", "artifact", "helm", "bad-token"},
		{"user credential", "artifact", "user-key", f.a.userKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.EnsureService(f.ctx, tc.service, tc.credentialName, tc.token, ""); err == nil {
				t.Fatal("expected conflict")
			}
		})
	}
	if _, err := svc.RevokeService(f.ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsureService(f.ctx, "artifact", "helm", token, ""); err == nil {
		t.Fatal("revoked credential resurrected")
	}
	expired := serviceToken(t)
	if _, err := table.InsertCredential(f.ctx, f.a.DB, identity.ID("cr"), "service", "", "artifact", "expired", identity.Hash(expired), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsureService(f.ctx, "artifact", "expired", expired, ""); err == nil {
		t.Fatal("expired credential resurrected")
	}
}

func TestEnsureServiceCredentialConcurrentPostgres(t *testing.T) {
	f := setup(t)
	svc := credential.New(f.a.DB)
	token := serviceToken(t)
	var wg sync.WaitGroup
	results := make(chan string, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			item, err := svc.EnsureService(context.Background(), "artifact", "concurrent", token, "")
			if err != nil {
				errs <- err
			} else {
				results <- item.ID
			}
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var id string
	for got := range results {
		if id != "" && got != id {
			t.Fatal("concurrent registration created distinct credentials")
		}
		id = got
	}
	var count int64
	if err := f.a.DB.Model(&table.Credential{}).Where("secret_hash=?", identity.Hash(token)).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("credential count %d: %v", count, err)
	}
}

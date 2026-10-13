package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func TestGORMTransactionRollbackAndCancellation(t *testing.T) {
	f := setup(t)
	aborted := errors.New("abort change")
	err := f.a.DB.WithContext(f.ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := table.InsertTenant(f.ctx, tx, "rolled-back", "Rollback", "rolled-back"); err != nil {
			return err
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	var count int
	if err := f.a.DB.WithContext(f.ctx).
		Raw(`SELECT count(*) FROM tenants WHERE id='rolled-back'`).
		Row().
		Scan(&count); err != nil ||
		count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if err := f.a.DB.WithContext(ctx).
		Exec(`INSERT INTO tenants(id,name,slug) VALUES('cancelled','Cancelled','cancelled')`).
		Error; !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancel error: %v", err)
	}
	connection, err := f.a.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.PingContext(f.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGORMSessionLockUsesPinnedConnection(t *testing.T) {
	f := setup(t)
	key := "session-lock-" + identity.Token()
	err := f.a.DB.WithContext(f.ctx).Connection(func(owner *gorm.DB) error {
		if _, err := table.LockSession(f.ctx, owner, key); err != nil {
			return err
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := table.UnlockSession(cleanup, owner, key); err != nil {
				t.Error(err)
			}
		}()
		var ownerPID int
		if err := owner.WithContext(f.ctx).Raw(`SELECT pg_backend_pid()`).Row().Scan(&ownerPID); err != nil {
			return err
		}
		return f.a.DB.WithContext(f.ctx).Connection(func(other *gorm.DB) error {
			var otherPID int
			if err := other.WithContext(f.ctx).
				Raw(`SELECT pg_backend_pid()`).
				Row().
				Scan(&otherPID); err != nil {
				return err
			}
			if otherPID == ownerPID {
				t.Fatal("connection was not pinned")
			}
			var acquired bool
			if err := table.TrySessionLock(f.ctx, other, key).Scan(&acquired); err != nil {
				return err
			}
			if acquired {
				_, _ = table.UnlockSession(f.ctx, other, key)
				t.Fatal("lock did not exclude another connection")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	// Closing/reusing a pool connection must not retain a session lock.
	err = f.a.DB.WithContext(f.ctx).Connection(func(other *gorm.DB) error {
		var acquired bool
		if err := table.TrySessionLock(f.ctx, other, key).Scan(&acquired); err != nil {
			return err
		}
		if !acquired {
			t.Fatal("session lock was not released")
		}
		_, err := table.UnlockSession(f.ctx, other, key)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

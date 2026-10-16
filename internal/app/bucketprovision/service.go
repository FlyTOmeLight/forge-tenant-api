// Package bucketprovision owns durable tenant storage preparation.
package bucketprovision

import (
	"context"
	"errors"
	"time"

	"github.com/FlyTOmeLight/server-go/logger"
	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

const MaxAttempts = 5

type Service struct {
	DB     *gorm.DB
	Client *Client
}

func (s *Service) Run(ctx context.Context) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if err := s.RunNext(ctx); err != nil && ctx.Err() == nil {
				logger.Errorf(ctx, "bucket provision: %v", err)
			}
		}
	}
}

// RunNext claims in a short transaction, then performs HTTP without a database lock.
func (s *Service) RunNext(ctx context.Context) error {
	leaseID := identity.ID("lease")
	job, err := table.ClaimTenantBucket(ctx, s.DB, leaseID, s.Client.Timeout+10*time.Second, MaxAttempts)
	if err != nil || job.TenantID == "" {
		return err
	}
	result, callErr := s.Client.Ensure(ctx, job.TenantID)
	// Persist even if shutdown cancelled HTTP; a bounded fresh context releases the lease.
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if callErr == nil {
		job.Status = "succeeded"
		job.BackendID = result.BackendID
		job.Bucket = result.Bucket
		job.LastError = ""
	} else {
		job.Status = "pending"
		if job.Attempts >= MaxAttempts {
			job.Status = "failed"
		}
		job.LastError = callErr.Error()
		job.NextAttemptAt = time.Now().Add(time.Duration(1<<job.Attempts) * time.Second)
	}
	changed, err := table.CompleteTenantBucket(save, s.DB, job)
	if err != nil {
		return err
	}
	if !changed {
		return errors.New("bucket provision lease no longer owned")
	}
	return callErr
}

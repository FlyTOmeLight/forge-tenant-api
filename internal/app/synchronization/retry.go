package synchronization

import (
	"context"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/tenantwrite"
)

type RetryService struct{ writes *tenantwrite.Writer }

func NewRetryService(writes *tenantwrite.Writer) *RetryService { return &RetryService{writes: writes} }

type EmptyRequest struct{}

func (a *RetryService) RetryOperation(
	ctx context.Context,
	u identity.User,
	r tenantwrite.Change,
	req EmptyRequest,
) (tenantwrite.Receipt, error) {
	return a.writes.Apply(ctx, u, "retry_sync", func(tx *gorm.DB) (map[string]any, error) {
		var ok bool
		e := table.RetryableOperationExists(ctx, tx, r.TenantID, r.OperationID).
			Scan(&ok)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, apperror.New(409, "OPERATION_NOT_RETRYABLE", "仅可重试当前版本的失败或冲突操作")
		}
		return map[string]any{"replaces_operation_id": r.OperationID}, nil
	}, r)
}

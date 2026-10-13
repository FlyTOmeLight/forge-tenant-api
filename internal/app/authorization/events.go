package authorization

import (
	"context"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func (a *Service) AccessEvents(ctx context.Context, after int64) (*tenantv1.ListAccessEventsResponse, error) {
	rows, err := table.ListAccessEvents(ctx, a.DB, after)
	if err != nil {
		return nil, err
	}
	result := &tenantv1.ListAccessEventsResponse{Items: make([]tenantv1.AccessEvent, 0, len(rows))}
	for _, row := range rows {
		result.Items = append(
			result.Items,
			tenantv1.AccessEvent{
				ID:        row["id"].(int64),
				TenantID:  row["tenant_id"].(string),
				Version:   row["version"].(int64),
				Reason:    row["reason"].(string),
				CreatedAt: row["created_at"].(time.Time),
			},
		)
	}
	return result, nil
}

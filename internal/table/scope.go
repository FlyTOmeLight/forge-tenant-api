package table

import (
	"context"

	"gorm.io/gorm"
)

// ProjectScopeExists checks tenant ownership without requiring GitLab synchronization or grants.
func ProjectScopeExists(ctx context.Context, db *gorm.DB, tenantID, projectID string) (bool, error) {
	var exists bool
	err := db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM projects WHERE tenant_id=? AND id=?)`, tenantID, projectID).
		Row().
		Scan(&exists)
	return exists, err
}

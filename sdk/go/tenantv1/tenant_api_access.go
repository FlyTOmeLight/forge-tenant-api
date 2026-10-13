package tenantv1

import "time"

type AccessAction string

const (
	AccessRead   AccessAction = "read"
	AccessWrite  AccessAction = "write"
	AccessManage AccessAction = "manage"
)

type CheckAccessRequest struct {
	UserID    string       `json:"user_id"`
	TenantID  string       `json:"tenant_id"`
	ProjectID string       `json:"project_id"`
	Action    AccessAction `json:"action"`
}

type CheckAccessResponse struct {
	Allowed              bool   `json:"allowed"`
	Level                int    `json:"level"`
	Read                 bool   `json:"read"`
	Write                bool   `json:"write"`
	Manage               bool   `json:"manage"`
	AuthorizationVersion int64  `json:"authorization_version"`
	TenantID             string `json:"tenant_id"`
	ProjectID            string `json:"project_id"`
	Reason               string `json:"reason,omitempty"`
}

// ListAccessEvents reads one page; the caller persists the last processed ID.
type ListAccessEventsRequest struct{ After int64 }

type AccessEvent struct {
	ID        int64     `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Version   int64     `json:"version"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type ListAccessEventsResponse struct {
	Items []AccessEvent `json:"items"`
}

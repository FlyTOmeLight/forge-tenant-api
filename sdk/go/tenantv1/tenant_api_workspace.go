package tenantv1

type CheckWorkspaceRequest struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

type WorkspaceProject struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Level int    `json:"level"`
}

type CheckWorkspaceResponse struct {
	Allowed  bool               `json:"allowed"`
	CanEdit  bool               `json:"canEdit"`
	Projects []WorkspaceProject `json:"projects"`
}

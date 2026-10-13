package tenantv1

type ResolveProjectRequest struct {
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`
}

type ResolveProjectResponse struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Path     string `json:"path"`
	GitLabID int64  `json:"gitlab_id"`
}

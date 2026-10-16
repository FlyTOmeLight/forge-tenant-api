package tenantv1

type ResolveScopeRequest struct {
	Session   string  `json:"session"`
	Method    string  `json:"method"`
	Origin    string  `json:"origin"`
	CSRF      string  `json:"csrf"`
	TenantID  string  `json:"tenant_id"`
	ProjectID *string `json:"project_id"`
}

type ResolveScopeResponse struct {
	UserID         string  `json:"user_id"`
	TenantID       string  `json:"tenant_id"`
	ProjectID      *string `json:"project_id"`
	OwnershipState string  `json:"ownership_state"`
}

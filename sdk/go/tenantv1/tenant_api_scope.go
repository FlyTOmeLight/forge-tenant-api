package tenantv1

type ResolveScopeRequest struct {
	// Type defaults to session for existing scope callers.
	Type string `json:"type,omitempty"`

	APIKey    string  `json:"api_key,omitempty"`
	Session   string  `json:"session"`
	Method    string  `json:"method"`
	Origin    string  `json:"origin"`
	CSRF      string  `json:"csrf"`
	TenantID  string  `json:"tenant_id"`
	ProjectID *string `json:"project_id"`
}

type ResolveScopeResponse struct {
	Principal      *Principal `json:"principal,omitempty"`
	UserID         string     `json:"user_id"`
	TenantID       string     `json:"tenant_id"`
	ProjectID      *string    `json:"project_id"`
	OwnershipState string     `json:"ownership_state"`
}

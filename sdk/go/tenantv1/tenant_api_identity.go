package tenantv1

const (
	IdentityProofSession = "session"
	IdentityProofAPIKey  = "api_key"
)

type ResolveIdentityRequest struct {
	Type    string `json:"type"`
	Session string `json:"session,omitempty"`
	Method  string `json:"method,omitempty"`
	Origin  string `json:"origin,omitempty"`
	CSRF    string `json:"csrf,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

type Principal struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Operator bool   `json:"operator"`
}

package tenantv1

type CheckSessionRequest struct {
	Session string `json:"session"`
	Method  string `json:"method"`
	Origin  string `json:"origin"`
	CSRF    string `json:"csrf"`
}

type CheckSessionResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Operator bool   `json:"operator"`
}

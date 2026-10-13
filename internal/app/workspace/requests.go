package workspace

type CreateTenantRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	AdminUserID string `json:"admin_user_id"`
}

type TenantStatusRequest struct {
	Status string `json:"status"`
}

type InviteMemberRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type RoleRequest struct {
	Role string `json:"role"`
}

type NameRequest struct {
	Name string `json:"name"`
}

type ProjectRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Mode string `json:"mode"`
}

type EmptyRequest struct{}

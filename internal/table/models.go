package table

type Idempotency struct {
	Route, Digest string
	Status        int
	Response      []byte
}

type User struct {
	ID, Name, Email, Subject string
	Operator                 bool
}

type Authorization struct {
	Desired, Observed, Effective           int
	Version                                int64
	TenantStatus, MemberStatus, UserStatus string
}

type Project struct {
	ID, Path string
	GitLabID int64
}

type TenantSyncState struct {
	Name, Slug, Status string
	GitLabID           int64
}

type Invitation struct{ ID, MembershipID, UserID, Name, Email, Username, UserStatus string }

type Operation struct {
	ID, TenantID string
	Version      int64
}

type MemberState struct{ Role, Status string }

type ProjectMember struct {
	Role, TenantStatus, MemberStatus, UserStatus string
	Desired, Observed, Effective                 int
}

type GrantSource struct {
	ProjectID    string `db:"project_id"`
	MembershipID string `db:"membership_id"`
	Level        int    `db:"level"`
	Status       string `db:"status"`
	Effective    int    `db:"effective_level"`
}

type ComputedGrant struct {
	ProjectID    string `json:"project_id"`
	MembershipID string `json:"membership_id"`
	Desired      int    `json:"desired_level"`
	Effective    int    `json:"effective_level"`
}

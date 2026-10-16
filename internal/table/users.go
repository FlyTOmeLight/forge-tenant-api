package table

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        string    `gorm:"column:id;type:text;primaryKey;autoIncrement:false"`
	Issuer    string    `gorm:"column:issuer;type:text;not null;uniqueIndex:users_issuer_subject_key,priority:1"`
	Subject   *string   `gorm:"column:subject;type:text;uniqueIndex:users_issuer_subject_key,priority:2"`
	Username  string    `gorm:"column:username;type:text;not null;unique"`
	Name      string    `gorm:"column:name;type:text;not null"`
	Email     string    `gorm:"column:email;type:text;not null;unique"`
	Status    string    `gorm:"column:status;type:text;not null;check:users_status_check,status IN ('invited','active','disabled')"`
	Operator  bool      `gorm:"column:operator;type:boolean;not null;default:false"`
	GitLabID  *int64    `gorm:"column:gitlab_id;type:bigint;unique"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false"`
}

func (User) TableName() string { return "users" }

type SessionIdentity struct {
	ID, Name, Email, Subject string
	Operator                 bool
}

func ListSyncUsers(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT DISTINCT u.id,u.subject,u.username,u.name,u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.status='active' AND u.status='active'`,
		tenantID,
	)
}

func UpsertOperator(
	ctx context.Context,
	db *gorm.DB,
	userID string,
	issuer string,
	userIDTarget string,
	username string,
	name string,
	email string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO users(id,issuer,subject,username,name,email,status,operator) VALUES(?,?,?,?,?,?,'active',true) ON CONFLICT(issuer,subject) DO UPDATE SET operator=true`, userID, issuer, userIDTarget, username, name, email)
	return result.RowsAffected, result.Error
}

func ActivateIdentity(ctx context.Context, db *gorm.DB, userID string, subject string) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE users SET subject=?,status='active' WHERE id=?`, subject, userID)
	return result.RowsAffected, result.Error
}

func ActiveUserBySubject(ctx context.Context, db *gorm.DB, issuer string, subject string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT id FROM users WHERE issuer=? AND subject=? AND status='active'`, issuer, subject).
		Row()
}

func UserByEmail(ctx context.Context, db *gorm.DB, email string) *sql.Row {
	return db.WithContext(ctx).Raw(`SELECT id,status FROM users WHERE email=?`, email).Row()
}

func InsertInvitedUser(
	ctx context.Context,
	db *gorm.DB,
	userID string,
	issuer string,
	username string,
	name string,
	email string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO users(id,issuer,username,name,email,status) VALUES(?,?,?,?,?,'invited')`, userID, issuer, username, name, email)
	return result.RowsAffected, result.Error
}

func ListActiveUsers(ctx context.Context, db *gorm.DB) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT id,name,email,status,operator,gitlab_id FROM users WHERE status='active' ORDER BY created_at LIMIT 200`,
	)
}

func SetUserGitLabID(ctx context.Context, db *gorm.DB, userID string, gitlabID int64) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE users SET gitlab_id=? WHERE id=?`, gitlabID, userID)
	return result.RowsAffected, result.Error
}

func ActiveUserExists(ctx context.Context, db *gorm.DB, admin string) *sql.Row {
	return db.WithContext(ctx).Raw(`SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND status='active')`, admin).Row()
}

func SessionUser(ctx context.Context, db *gorm.DB, sessionHash string) (SessionIdentity, error) {
	var record SessionIdentity
	err := db.WithContext(ctx).
		Raw(`SELECT u.id,u.name,u.email,u.operator,coalesce(u.subject,'') FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>now() AND u.status='active'`, sessionHash).
		Row().
		Scan(&record.ID, &record.Name, &record.Email, &record.Operator, &record.Subject)
	return record, err
}

func ListProjectPeople(ctx context.Context, db *gorm.DB, tenantID string, projectID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT u.id,u.name,(least(g.desired_level,g.observed_level,g.effective_level)>=20) AS "canReview",(least(g.desired_level,g.observed_level,g.effective_level)>=30) AS "canOwn" FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE g.tenant_id=? AND g.project_id=? AND t.status='active' AND m.status='active' AND u.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20 ORDER BY u.name`,
		tenantID,
		projectID,
	)
}

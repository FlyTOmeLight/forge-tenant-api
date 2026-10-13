package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

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

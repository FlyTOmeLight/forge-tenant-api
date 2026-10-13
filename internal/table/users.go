package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func UpsertOperator(
	ctx context.Context,
	db Writer,
	userID string,
	issuer string,
	userIDTarget string,
	username string,
	name string,
	email string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO users(id,issuer,subject,username,name,email,status,operator) VALUES($1,$2,$3,$4,$5,$6,'active',true) ON CONFLICT(issuer,subject) DO UPDATE SET operator=true`,
		userID,
		issuer,
		userIDTarget,
		username,
		name,
		email,
	)
}

func ActivateIdentity(ctx context.Context, db Writer, userID string, subject string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE users SET subject=$2,status='active' WHERE id=$1`, userID, subject)
}

func ActiveUserBySubject(ctx context.Context, db RowReader, issuer string, subject string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT id FROM users WHERE issuer=$1 AND subject=$2 AND status='active'`,
		issuer,
		subject,
	)
}

func UserByEmail(ctx context.Context, db RowReader, email string) pgx.Row {
	return db.QueryRow(ctx, `SELECT id,status FROM users WHERE email=$1`, email)
}

func InsertInvitedUser(
	ctx context.Context,
	db Writer,
	userID string,
	issuer string,
	username string,
	name string,
	email string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO users(id,issuer,username,name,email,status) VALUES($1,$2,$3,$4,$5,'invited')`,
		userID,
		issuer,
		username,
		name,
		email,
	)
}

func ListActiveUsers(ctx context.Context, db Reader) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT id,name,email,status,operator,gitlab_id FROM users WHERE status='active' ORDER BY created_at LIMIT 200`,
	)
}

func SetUserGitLabID(ctx context.Context, db Writer, userID string, gitlabID int64) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `UPDATE users SET gitlab_id=$2 WHERE id=$1`, userID, gitlabID)
}

func ActiveUserExists(ctx context.Context, db RowReader, admin string) pgx.Row {
	return db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active')`, admin)
}

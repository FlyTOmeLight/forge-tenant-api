package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func InsertLoginFlow(
	ctx context.Context,
	db Writer,
	flowHash string,
	stateHash string,
	nonce string,
	verifier string,
	back string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO login_flows(token_hash,state_hash,nonce,verifier,return_path,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '10 minutes')`,
		flowHash,
		stateHash,
		nonce,
		verifier,
		back,
	)
}

func ConsumeLoginFlow(ctx context.Context, db RowReader, flowHash string) pgx.Row {
	return db.QueryRow(
		ctx,
		`DELETE FROM login_flows WHERE token_hash=$1 AND expires_at>now() RETURNING state_hash,nonce,verifier,return_path`,
		flowHash,
	)
}

func InsertSession(
	ctx context.Context,
	db Writer,
	sessionHash string,
	userID string,
	csrfHash string,
) (pgconn.CommandTag, error) {
	return db.Exec(
		ctx,
		`INSERT INTO sessions(token_hash,user_id,csrf_hash,expires_at) VALUES($1,$2,$3,now()+interval '8 hours')`,
		sessionHash,
		userID,
		csrfHash,
	)
}

func LoginFlowExists(ctx context.Context, db RowReader, flowHash string, stateHash string) pgx.Row {
	return db.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM login_flows WHERE token_hash=$1 AND state_hash=$2 AND expires_at>now())`,
		flowHash,
		stateHash,
	)
}

func SessionUser(ctx context.Context, db RowReader, sessionHash string) (User, error) {
	var record User
	err := db.QueryRow(
		ctx,
		`SELECT u.id,u.name,u.email,u.operator,coalesce(u.subject,'') FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.status='active'`,
		sessionHash,
	).Scan(&record.ID, &record.Name, &record.Email, &record.Operator, &record.Subject)
	return record, err
}

func DeleteSession(ctx context.Context, db Writer, sessionHash string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, sessionHash)
}

func PruneSessions(ctx context.Context, db Writer) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `DELETE FROM sessions WHERE expires_at<now()`)
}

func PruneLoginFlows(ctx context.Context, db Writer) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `DELETE FROM login_flows WHERE expires_at<now()`)
}

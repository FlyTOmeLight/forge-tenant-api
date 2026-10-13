package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

func InsertLoginFlow(
	ctx context.Context,
	db *gorm.DB,
	flowHash string,
	stateHash string,
	nonce string,
	verifier string,
	back string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO login_flows(token_hash,state_hash,nonce,verifier,return_path,expires_at) VALUES(?,?,?,?,?,now()+interval '10 minutes')`, flowHash, stateHash, nonce, verifier, back)
	return result.RowsAffected, result.Error
}

func ConsumeLoginFlow(ctx context.Context, db *gorm.DB, flowHash string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`DELETE FROM login_flows WHERE token_hash=? AND expires_at>now() RETURNING state_hash,nonce,verifier,return_path`, flowHash).
		Row()
}

func InsertSession(
	ctx context.Context,
	db *gorm.DB,
	sessionHash string,
	userID string,
	csrfHash string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO sessions(token_hash,user_id,csrf_hash,expires_at) VALUES(?,?,?,now()+interval '8 hours')`, sessionHash, userID, csrfHash)
	return result.RowsAffected, result.Error
}

func LoginFlowExists(ctx context.Context, db *gorm.DB, flowHash string, stateHash string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM login_flows WHERE token_hash=? AND state_hash=? AND expires_at>now())`, flowHash, stateHash).
		Row()
}

func SessionUser(ctx context.Context, db *gorm.DB, sessionHash string) (User, error) {
	var record User
	err := db.WithContext(ctx).
		Raw(`SELECT u.id,u.name,u.email,u.operator,coalesce(u.subject,'') FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>now() AND u.status='active'`, sessionHash).
		Row().
		Scan(&record.ID, &record.Name, &record.Email, &record.Operator, &record.Subject)
	return record, err
}

func DeleteSession(ctx context.Context, db *gorm.DB, sessionHash string) (int64, error) {
	result := db.WithContext(ctx).Exec(`DELETE FROM sessions WHERE token_hash=?`, sessionHash)
	return result.RowsAffected, result.Error
}

func PruneSessions(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).Exec(`DELETE FROM sessions WHERE expires_at<now()`)
	return result.RowsAffected, result.Error
}

func PruneLoginFlows(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).Exec(`DELETE FROM login_flows WHERE expires_at<now()`)
	return result.RowsAffected, result.Error
}

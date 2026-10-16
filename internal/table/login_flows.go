package table

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
)

type LoginFlow struct {
	TokenHash  string    `gorm:"type:text;primaryKey"`
	StateHash  string    `gorm:"type:text;not null"`
	Nonce      string    `gorm:"type:text;not null"`
	Verifier   string    `gorm:"type:text;not null"`
	ReturnPath string    `gorm:"type:text;not null"`
	ExpiresAt  time.Time `gorm:"type:timestamptz;not null;autoCreateTime:false;autoUpdateTime:false"`
}

func (LoginFlow) TableName() string { return "login_flows" }

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

func LoginFlowExists(ctx context.Context, db *gorm.DB, flowHash string, stateHash string) *sql.Row {
	return db.WithContext(ctx).
		Raw(`SELECT EXISTS(SELECT 1 FROM login_flows WHERE token_hash=? AND state_hash=? AND expires_at>now())`, flowHash, stateHash).
		Row()
}

func PruneLoginFlows(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).Exec(`DELETE FROM login_flows WHERE expires_at<now()`)
	return result.RowsAffected, result.Error
}

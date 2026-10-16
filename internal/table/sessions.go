package table

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type Session struct {
	TokenHash string    `gorm:"type:text;primaryKey"`
	UserID    string    `gorm:"type:text;not null"`
	CSRFHash  string    `gorm:"type:text;not null"`
	ExpiresAt time.Time `gorm:"type:timestamptz;not null;autoCreateTime:false;autoUpdateTime:false"`
	User      User      `gorm:"foreignKey:UserID;references:ID;constraint:sessions_user_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION"`
}

func (Session) TableName() string { return "sessions" }

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

func DeleteSession(ctx context.Context, db *gorm.DB, sessionHash string) (int64, error) {
	result := db.WithContext(ctx).Exec(`DELETE FROM sessions WHERE token_hash=?`, sessionHash)
	return result.RowsAffected, result.Error
}

func PruneSessions(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).Exec(`DELETE FROM sessions WHERE expires_at<now()`)
	return result.RowsAffected, result.Error
}

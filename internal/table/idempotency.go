package table

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type Idempotency struct {
	ActorID    string    `gorm:"type:text;primaryKey"`
	RequestKey string    `gorm:"type:text;primaryKey"`
	Route      string    `gorm:"type:text;not null"`
	Digest     string    `gorm:"column:body_hash;type:text;not null"`
	Status     int       `gorm:"column:status_code;type:integer;not null"`
	Response   []byte    `gorm:"type:bytea;not null"`
	CreatedAt  time.Time `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false;autoUpdateTime:false"`
}

func (Idempotency) TableName() string { return "idempotency" }

func FindIdempotency(ctx context.Context, db *gorm.DB, userID string, key string) (Idempotency, error) {
	var record Idempotency
	err := db.WithContext(ctx).
		Raw(`SELECT route,body_hash,status_code,response FROM idempotency WHERE actor_id=? AND request_key=?`, userID, key).
		Row().
		Scan(&record.Route, &record.Digest, &record.Status, &record.Response)
	return record, err
}

func InsertIdempotency(
	ctx context.Context,
	db *gorm.DB,
	userID string,
	key string,
	route string,
	digest string,
	encryptedResult []byte,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO idempotency(actor_id,request_key,route,body_hash,status_code,response) VALUES(?,?,?,?,202,?)`, userID, key, route, digest, encryptedResult)
	return result.RowsAffected, result.Error
}

func PruneIdempotency(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).Exec(`DELETE FROM idempotency WHERE created_at<now()-interval '24 hours'`)
	return result.RowsAffected, result.Error
}

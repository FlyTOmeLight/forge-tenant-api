package table

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

func LockTransaction(ctx context.Context, db *gorm.DB, lockKey string) (int64, error) {
	result := db.WithContext(ctx).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, lockKey)
	return result.RowsAffected, result.Error
}

func UnlockSession(ctx context.Context, db *gorm.DB, lockKey string) (int64, error) {
	result := db.WithContext(ctx).Exec(`SELECT pg_advisory_unlock(hashtextextended(?,0))`, lockKey)
	return result.RowsAffected, result.Error
}

func LockSession(ctx context.Context, db *gorm.DB, lockKey string) (int64, error) {
	result := db.WithContext(ctx).Exec(`SELECT pg_advisory_lock(hashtextextended(?,0))`, lockKey)
	return result.RowsAffected, result.Error
}

func TrySessionLock(ctx context.Context, db *gorm.DB, lockKey string) *sql.Row {
	return db.WithContext(ctx).Raw(`SELECT pg_try_advisory_lock(hashtextextended(?,0))`, lockKey).Row()
}

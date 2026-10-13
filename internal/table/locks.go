package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func LockTransaction(ctx context.Context, db Writer, lockKey string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey)
}

func UnlockSession(ctx context.Context, db Writer, lockKey string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lockKey)
}

func LockSession(ctx context.Context, db Writer, lockKey string) (pgconn.CommandTag, error) {
	return db.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, lockKey)
}

func TrySessionLock(ctx context.Context, db RowReader, lockKey string) pgx.Row {
	return db.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, lockKey)
}

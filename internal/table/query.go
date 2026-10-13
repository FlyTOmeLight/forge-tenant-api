package table

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type RowReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Writer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type Reader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func collect(ctx context.Context, db Reader, sql string, args ...any) ([]map[string]any, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	result, err := pgx.CollectRows(rows, pgx.RowToMap)
	if result == nil {
		result = []map[string]any{}
	}
	return result, err
}

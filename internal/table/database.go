package table

import (
	"context"
	_ "embed"

	"gorm.io/gorm"
)

//go:embed schema.sql
var schema string

//go:embed roles.sql
var roleSchema string

func Migrate(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, statement := range []string{"SELECT pg_advisory_xact_lock(71477231)", schema, roleSchema} {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

package table

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
)

// collect preserves the API's column names, JSON values and empty array shape.
func collect(ctx context.Context, db *gorm.DB, query string, args ...any) ([]map[string]any, error) {
	rows, err := db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		record := make(map[string]any, len(columns))
		for i, column := range columns {
			value := values[i]
			if value != nil &&
				(column.DatabaseTypeName() == "JSONB" || column.DatabaseTypeName() == "JSON") {
				var data []byte
				switch v := value.(type) {
				case []byte:
					data = v
				case string:
					data = []byte(v)
				}
				if err := json.Unmarshal(data, &value); err != nil {
					return nil, err
				}
			}
			record[column.Name()] = value
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

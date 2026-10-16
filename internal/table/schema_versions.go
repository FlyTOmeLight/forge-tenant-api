package table

import "time"

// SchemaVersion records completed data upgrades separately from model migration.
type SchemaVersion struct {
	Version   int       `gorm:"type:integer;primaryKey;autoIncrement:false"`
	AppliedAt time.Time `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false"`
}

func (SchemaVersion) TableName() string { return "schema_versions" }

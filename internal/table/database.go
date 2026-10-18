package table

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// Migrate updates model-defined tables and applies historical data upgrades in
// one transaction. The database lock serializes concurrent deployment commands.
func Migrate(ctx context.Context, db *gorm.DB) error {
	migration := db.Session(&gorm.Session{NewDB: true})
	migration.NamingStrategy = databaseNames{Namer: db.NamingStrategy}
	return migration.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", 71477231).Error; err != nil {
			return err
		}
		if err := tx.AutoMigrate(
			&SchemaVersion{}, &User{}, &Tenant{}, &Team{}, &Membership{}, &Project{},
			&Grant{}, &Operation{}, &AuditEvent{}, &Invitation{}, &Session{},
			&LoginFlow{}, &Idempotency{}, &AccessEvent{}, &Credential{},
			&TeamMembership{}, &DirectGrant{}, &TeamGrant{}, &TenantBucketProvision{},
		); err != nil {
			return err
		}
		if err := migrateTeamRoles(tx); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&[]SchemaVersion{{Version: 1}, {Version: 2}, {Version: 5}}).
			Error
	})
}

// migrateTeamRoles replaces the former single-team membership field with team
// memberships and preserves direct authorization once, before recording v5.
func migrateTeamRoles(tx *gorm.DB) error {
	var applied int64
	if err := tx.Model(&SchemaVersion{}).Where("version = ?", 5).Count(&applied).Error; err != nil {
		return err
	}
	if applied != 0 {
		return nil
	}
	if tx.Migrator().HasColumn(&Membership{}, "team_id") {
		var memberships []struct{ ID, TenantID, TeamID string }
		if err := tx.Table("memberships").
			Select("id, tenant_id, team_id").
			Where("team_id IS NOT NULL AND status <> ?", "removed").
			Scan(&memberships).
			Error; err != nil {
			return err
		}
		teams := make([]TeamMembership, 0, len(memberships))
		for _, m := range memberships {
			teams = append(
				teams,
				TeamMembership{TenantID: m.TenantID, TeamID: m.TeamID, MembershipID: m.ID},
			)
		}
		if len(teams) > 0 {
			if err := tx.CreateInBatches(&teams, 100).Error; err != nil {
				return err
			}
		}
		if err := tx.Migrator().DropColumn(&Membership{}, "team_id"); err != nil {
			return err
		}
	}
	var grants []Grant
	if err := tx.Select("tenant_id, project_id, membership_id, desired_level").
		Where("desired_level > ?", 0).
		Find(&grants).
		Error; err != nil {
		return err
	}
	direct := make([]DirectGrant, 0, len(grants))
	for _, g := range grants {
		direct = append(
			direct,
			DirectGrant{
				TenantID:     g.TenantID,
				ProjectID:    g.ProjectID,
				MembershipID: g.MembershipID,
				Level:        g.DesiredLevel,
			},
		)
	}
	if len(direct) > 0 {
		if err := tx.CreateInBatches(&direct, 100).Error; err != nil {
			return err
		}
	}
	return nil
}

// PostgreSQL's existing UNIQUE constraints use table_column_key. Reusing those
// names lets AutoMigrate update the models without replacing live constraints.
type databaseNames struct{ schema.Namer }

func (databaseNames) UniqueName(table, column string) string { return table + "_" + column + "_key" }

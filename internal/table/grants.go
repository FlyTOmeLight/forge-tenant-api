package table

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type Grant struct {
	TenantID       string      `gorm:"column:tenant_id;type:text;not null"`
	ProjectID      string      `gorm:"column:project_id;type:text;primaryKey;autoIncrement:false"`
	MembershipID   string      `gorm:"column:membership_id;type:text;primaryKey;autoIncrement:false"`
	DesiredLevel   int         `gorm:"column:desired_level;type:integer;not null;check:grants_desired_level_check,desired_level IN (0,20,30,40)"`
	ObservedLevel  int         `gorm:"column:observed_level;type:integer;not null;default:0"`
	CheckedAt      *time.Time  `gorm:"column:checked_at;type:timestamptz"`
	EffectiveLevel int         `gorm:"column:effective_level;type:integer;not null;default:0;check:grants_effective_level_check,effective_level IN (0,20,30,40)"`
	Project        *Project    `gorm:"foreignKey:TenantID,ProjectID;references:TenantID,ID;constraint:grants_tenant_id_project_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
	Membership     *Membership `gorm:"foreignKey:TenantID,MembershipID;references:TenantID,ID;constraint:grants_tenant_id_membership_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
}

func (Grant) TableName() string { return "grants" }

type Authorization struct {
	Desired, Observed, Effective           int
	Version                                int64
	TenantStatus, MemberStatus, UserStatus string
}

type GrantSource struct {
	ProjectID    string `gorm:"column:project_id"`
	MembershipID string `gorm:"column:membership_id"`
	Level        int    `gorm:"column:level"`
	Status       string `gorm:"column:status"`
	Effective    int    `gorm:"column:effective_level"`
}

type ComputedGrant struct {
	ProjectID    string `json:"project_id"`
	MembershipID string `json:"membership_id"`
	Desired      int    `json:"desired_level"`
	Effective    int    `json:"effective_level"`
}

func LoadGrantSources(ctx context.Context, db *gorm.DB, tenantID string) ([]GrantSource, error) {
	var result []GrantSource
	err := db.WithContext(ctx).Raw(`WITH sources AS (
 SELECT project_id,membership_id,level FROM direct_grants WHERE tenant_id=?
 UNION ALL SELECT g.project_id,m.membership_id,g.level FROM team_grants g JOIN team_memberships m ON m.tenant_id=g.tenant_id AND m.team_id=g.team_id WHERE g.tenant_id=?
 UNION ALL SELECT project_id,membership_id,0 FROM grants WHERE tenant_id=?
 ) SELECT s.project_id,s.membership_id,s.level,m.status,coalesce(g.effective_level,0) AS effective_level
 FROM sources s JOIN memberships m ON m.id=s.membership_id AND m.tenant_id=?
 LEFT JOIN grants g ON g.tenant_id=? AND g.project_id=s.project_id AND g.membership_id=s.membership_id`, tenantID, tenantID, tenantID, tenantID, tenantID).Scan(&result).Error
	return result, err
}

func SaveComputedGrants(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	grants []ComputedGrant,
) (int64, error) {
	data, err := json.Marshal(grants)
	if err != nil {
		return 0, err
	}
	result := db.WithContext(ctx).
		Exec(`INSERT INTO grants(tenant_id,project_id,membership_id,desired_level,effective_level)
 SELECT ?,project_id,membership_id,desired_level,effective_level FROM jsonb_to_recordset(?::jsonb) AS g(project_id text,membership_id text,desired_level integer,effective_level integer)
 ON CONFLICT(project_id,membership_id) DO UPDATE SET desired_level=EXCLUDED.desired_level,effective_level=EXCLUDED.effective_level`, tenantID, string(data))
	return result.RowsAffected, result.Error
}

func ListGrantSources(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	membershipID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT d.project_id,'direct' AS kind,d.membership_id AS id,'直接分配' AS name,d.level FROM direct_grants d WHERE d.tenant_id=? AND d.membership_id=? UNION ALL SELECT g.project_id,'team',t.id,t.name,g.level FROM team_grants g JOIN teams t ON t.id=g.team_id JOIN team_memberships tm ON tm.team_id=t.id WHERE g.tenant_id=? AND tm.membership_id=? ORDER BY project_id,kind,name`,
		tenantID,
		membershipID,
		tenantID,
		membershipID,
	)
}

func ListSnapshotGrants(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	admin bool,
	userID string,
) ([]map[string]any, error) {
	return collect(ctx, db, `SELECT g.*,coalesce(d.level,0) AS direct_level,
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',t.id,'name',t.name,'level',tg.level)) FROM team_grants tg JOIN teams t ON t.id=tg.team_id JOIN team_memberships tm ON tm.team_id=t.id WHERE tg.project_id=g.project_id AND tm.membership_id=g.membership_id),'[]'::jsonb) AS team_sources,u.name,u.email,m.status AS member_status,u.status AS user_status FROM grants g LEFT JOIN direct_grants d ON d.project_id=g.project_id AND d.membership_id=g.membership_id JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id WHERE g.tenant_id=? AND (? OR m.user_id=? OR EXISTS(SELECT 1 FROM grants own JOIN memberships om ON om.id=own.membership_id WHERE own.project_id=g.project_id AND om.user_id=? AND om.status='active' AND least(own.desired_level,own.observed_level,own.effective_level)>=40)) ORDER BY g.project_id,u.name LIMIT 1000`, tenantID, admin, userID, userID)
}

func ProjectAuthorization(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	projectID string,
	userID string,
) (Authorization, error) {
	var record Authorization
	err := db.WithContext(ctx).
		Raw(`SELECT g.desired_level,g.observed_level,g.effective_level,t.version,t.status,m.status,u.status FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=g.tenant_id WHERE g.tenant_id=? AND g.project_id=? AND m.user_id=?`, tenantID, projectID, userID).
		Row().
		Scan(&record.Desired, &record.Observed, &record.Effective, &record.Version, &record.TenantStatus, &record.MemberStatus, &record.UserStatus)
	return record, err
}

func ListSyncGrants(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT g.project_id,g.membership_id,g.desired_level,m.status AS member_status,u.status AS user_status,coalesce(u.gitlab_id,0) AS user_remote,p.gitlab_id AS project_remote FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN projects p ON p.id=g.project_id WHERE g.tenant_id=? ORDER BY g.project_id,g.membership_id`,
		tenantID,
	)
}

func RecordObservedAccess(
	ctx context.Context,
	db *gorm.DB,
	projectID string,
	membershipID string,
	actual int,
	confirmed bool,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE grants SET observed_level=?,checked_at=now(),effective_level=CASE WHEN ? THEN LEAST(desired_level,?) ELSE LEAST(effective_level,desired_level,?) END WHERE project_id=? AND membership_id=?`, actual, confirmed, actual, actual, projectID, membershipID)
	return result.RowsAffected, result.Error
}

func ClearTenantEffectiveAccess(ctx context.Context, db *gorm.DB, tenantID string) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE grants SET effective_level=0 WHERE tenant_id=?`, tenantID)
	return result.RowsAffected, result.Error
}

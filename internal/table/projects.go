package table

import (
	"context"

	"gorm.io/gorm"
)

type Project struct {
	ID       string  `gorm:"column:id;type:text;primaryKey;autoIncrement:false;uniqueIndex:projects_tenant_id_id_key,priority:2"`
	TenantID string  `gorm:"column:tenant_id;type:text;not null;uniqueIndex:projects_tenant_id_path_key,priority:1;uniqueIndex:projects_tenant_id_id_key,priority:1"`
	Name     string  `gorm:"column:name;type:text;not null"`
	Path     string  `gorm:"column:path;type:text;not null;uniqueIndex:projects_tenant_id_path_key,priority:2"`
	Mode     string  `gorm:"column:mode;type:text;not null;check:projects_mode_check,mode IN ('create','bind')"`
	GitLabID *int64  `gorm:"column:gitlab_id;type:bigint;unique"`
	Tenant   *Tenant `gorm:"foreignKey:TenantID;references:ID;constraint:projects_tenant_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION" json:"-"`
}

func (Project) TableName() string { return "projects" }

type ResolvedProject struct {
	ID, Path string
	GitLabID int64
}

func ListSnapshotProjects(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	admin bool,
	userID string,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT p.* FROM projects p WHERE p.tenant_id=? AND (? OR EXISTS(SELECT 1 FROM grants g JOIN memberships m ON m.id=g.membership_id WHERE g.project_id=p.id AND m.user_id=? AND m.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20)) ORDER BY p.name LIMIT 500`,
		tenantID,
		admin,
		userID,
	)
}

func ResolveProject(ctx context.Context, db *gorm.DB, tenantID string, projectID string) (ResolvedProject, error) {
	var record ResolvedProject
	err := db.WithContext(ctx).
		Raw(`SELECT id,path,gitlab_id FROM projects WHERE tenant_id=? AND id=? AND gitlab_id IS NOT NULL`, tenantID, projectID).
		Row().
		Scan(&record.ID, &record.Path, &record.GitLabID)
	return record, err
}

func ListWorkspaceProjects(ctx context.Context, db *gorm.DB, tenantID string, userID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT p.id,p.name,least(g.desired_level,g.observed_level,g.effective_level) AS level FROM projects p JOIN grants g ON g.project_id=p.id AND g.tenant_id=p.tenant_id JOIN memberships m ON m.id=g.membership_id WHERE p.tenant_id=? AND m.user_id=? AND m.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20 ORDER BY p.name,p.id`,
		tenantID,
		userID,
	)
}

func InsertProject(
	ctx context.Context,
	db *gorm.DB,
	projectID string,
	tenantID string,
	name string,
	path string,
	mode string,
) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`INSERT INTO projects(id,tenant_id,name,path,mode) VALUES(?,?,?,?,?)`, projectID, tenantID, name, path, mode)
	return result.RowsAffected, result.Error
}

func ListMemberProjects(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	membershipID string,
	includeAll bool,
) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT p.id,p.name,coalesce(g.desired_level,0) AS desired_level,coalesce(g.observed_level,0) AS observed_level,coalesce(g.effective_level,0) AS effective_level FROM projects p LEFT JOIN grants g ON g.project_id=p.id AND g.membership_id=? WHERE p.tenant_id=? AND (? OR g.membership_id IS NOT NULL) ORDER BY p.name`,
		membershipID,
		tenantID,
		includeAll,
	)
}

func ListSyncProjects(ctx context.Context, db *gorm.DB, tenantID string) ([]map[string]any, error) {
	return collect(
		ctx,
		db,
		`SELECT id,name,path,mode,coalesce(gitlab_id,0) AS gitlab_id FROM projects WHERE tenant_id=? ORDER BY id`,
		tenantID,
	)
}

func SetProjectGitLabID(ctx context.Context, db *gorm.DB, projectID string, gitlabID int64) (int64, error) {
	result := db.WithContext(ctx).Exec(`UPDATE projects SET gitlab_id=? WHERE id=?`, gitlabID, projectID)
	return result.RowsAffected, result.Error
}

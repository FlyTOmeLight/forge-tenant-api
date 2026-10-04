package platform

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// ProjectPermissions separates tenant administration from code authority.
type ProjectPermissions struct {
	ViewProject        bool `json:"viewProject"`
	ReadCode           bool `json:"readCode"`
	CreateTask         bool `json:"createTask"`
	ManageMembers      bool `json:"manageMembers"`
	ConfigureExecution bool `json:"configureExecution"`
}

func projectPermissions(level int, tenantAdmin, active bool) ProjectPermissions {
	return ProjectPermissions{ViewProject: tenantAdmin || level >= 20, ReadCode: level >= 20, CreateTask: level >= 30, ManageMembers: active && (tenantAdmin || level >= 40), ConfigureExecution: level >= 40}
}
func tenantPermissions(admin, active bool) Object {
	return Object{"manageMembers": admin && active, "manageProjects": admin && active, "viewSettings": admin, "viewAudit": admin, "viewSync": admin}
}
func (a *App) projectPeople(w http.ResponseWriter, r *http.Request, u User) {
	tid, pid := r.PathValue("tid"), r.PathValue("pid")
	level, e := a.taskRead(r, u)
	if e != nil {
		fail(w, e)
		return
	}
	rows, e := collect(r.Context(), a.DB, `SELECT u.id,u.name,(least(g.desired_level,g.observed_level,g.effective_level)>=20) AS "canReview",(least(g.desired_level,g.observed_level,g.effective_level)>=30) AS "canOwn" FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE g.tenant_id=$1 AND g.project_id=$2 AND t.status='active' AND m.status='active' AND u.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20 ORDER BY u.name`, tid, pid)
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, Object{"items": rows, "canAssign": level >= 30})
}
func (a *App) listPlatformTenants(w http.ResponseWriter, r *http.Request, u User) {
	if !u.Operator {
		fail(w, problem(403, "FORBIDDEN", "需要平台管理权限"))
		return
	}
	rows, e := collect(r.Context(), a.DB, `SELECT t.*, (SELECT count(*) FROM memberships m WHERE m.tenant_id=t.id AND m.status<>'removed') AS member_count,(SELECT count(*) FROM projects p WHERE p.tenant_id=t.id) AS project_count,coalesce((SELECT status FROM operations o WHERE o.tenant_id=t.id AND o.version=t.version),'pending') AS sync_status FROM tenants t ORDER BY created_at DESC LIMIT 200`)
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, Object{"items": rows})
}
func grantManager(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, u User, tid, pid string) (bool, error) {
	var allowed bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p JOIN tenants t ON t.id=p.tenant_id JOIN memberships m ON m.tenant_id=t.id JOIN users u ON u.id=m.user_id LEFT JOIN grants g ON g.project_id=p.id AND g.membership_id=m.id WHERE p.tenant_id=$1 AND p.id=$2 AND m.user_id=$3 AND t.status='active' AND m.status='active' AND u.status='active' AND (m.role='admin' OR least(g.desired_level,g.observed_level,g.effective_level)>=40))`, tid, pid, u.ID).Scan(&allowed)
	return allowed, err
}

func grantLevel(v any) int {
	switch n := v.(type) {
	case int32:
		return int(n)
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}
func grantRole(level int) string {
	switch {
	case level >= 40:
		return "admin"
	case level >= 30:
		return "developer"
	case level >= 20:
		return "reviewer"
	default:
		return "none"
	}
}

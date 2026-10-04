package platform

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type RoleDefinition struct {
	ID          string   `json:"id"`
	Scope       string   `json:"scope"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	Conditions  []string `json:"conditions"`
}

func roleDefinitions() []RoleDefinition {
	return []RoleDefinition{
		{"operator", "platform", "平台管理员", []string{"开通、启用和停用租户", "查看全局服务连接", "指定首位租户管理员"}, []string{"进入租户需要成员身份，代码操作需要项目授权"}},
		{"admin", "tenant", "租户管理员", []string{"邀请和管理租户成员", "管理团队成员", "接入项目和分配项目角色", "查看审计和权限同步"}, []string{"读取代码、创建任务和配置执行需要项目角色"}},
		{"member", "tenant", "普通成员", []string{"查看自己的角色和权限"}, []string{"项目能力由直接授权和团队授权决定"}},
		{"reviewer", "project", "审查者", []string{"读取代码和任务", "查看项目执行配置", "参与任务审查"}, []string{"提交认可或审查反馈需要被指定为该任务的审查者"}},
		{"developer", "project", "开发者", []string{"读取代码和任务", "查看项目执行配置", "创建任务并选择审查者", "控制本人负责的任务", "参与任务审查"}, []string{"控制任务需具有负责人或项目管理资格；审查需被指定"}},
		{"admin", "project", "项目管理员", []string{"读取代码和任务", "创建任务并选择审查者", "管理项目成员与团队授权", "配置项目执行", "管理项目任务", "参与任务审查"}, []string{"认可代码仍需被指定为审查者；GitLab 分支策略单独生效"}},
	}
}
func (a *App) roles(w http.ResponseWriter, r *http.Request, u User) {
	JSON(w, 200, Object{"items": roleDefinitions()})
}

// recomputeGrants materializes the union of direct and team assignments for the
// existing synchronization and execution authorization paths. Raising the target
// never raises the confirmed ceiling; removing a source preserves other sources.
func recomputeGrants(ctx context.Context, tx pgx.Tx, tid string) error {
	_, err := tx.Exec(ctx, `WITH sources AS (
 SELECT project_id,membership_id,level FROM direct_grants WHERE tenant_id=$1
 UNION ALL SELECT g.project_id,m.membership_id,g.level FROM team_grants g JOIN team_memberships m ON m.tenant_id=g.tenant_id AND m.team_id=g.team_id WHERE g.tenant_id=$1
 UNION ALL SELECT project_id,membership_id,0 FROM grants WHERE tenant_id=$1
 ), targets AS (
 SELECT s.project_id,s.membership_id,CASE WHEN m.status='active' THEN max(s.level) ELSE 0 END AS level
 FROM sources s JOIN memberships m ON m.id=s.membership_id AND m.tenant_id=$1 GROUP BY s.project_id,s.membership_id,m.status
 ) INSERT INTO grants(tenant_id,project_id,membership_id,desired_level)
 SELECT $1,project_id,membership_id,level FROM targets
 ON CONFLICT(project_id,membership_id) DO UPDATE SET desired_level=EXCLUDED.desired_level,effective_level=least(grants.effective_level,EXCLUDED.desired_level)`, tid)
	return err
}
func projectRoleLevel(role string) int {
	return map[string]int{"reviewer": 20, "developer": 30, "admin": 40}[role]
}
func (a *App) setTeamMember(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	tid, team, mid := r.PathValue("tid"), r.PathValue("team"), r.PathValue("mid")
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM teams t JOIN memberships m ON m.tenant_id=t.tenant_id JOIN users u ON u.id=m.user_id WHERE t.tenant_id=$1 AND t.id=$2 AND m.id=$3 AND ($4 OR (m.status='active' AND u.status='active')))`, tid, team, mid, r.Method == "DELETE").Scan(&ok)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, problem(404, "NOT_FOUND", "团队或已激活成员不存在")
	}
	if r.Method == "DELETE" {
		_, err = tx.Exec(ctx, `DELETE FROM team_memberships WHERE tenant_id=$1 AND team_id=$2 AND membership_id=$3`, tid, team, mid)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO team_memberships VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, tid, team, mid)
	}
	if err != nil {
		return nil, err
	}
	if err = recomputeGrants(ctx, tx, tid); err != nil {
		return nil, err
	}
	return Object{"team_id": team, "membership_id": mid, "removed": r.Method == "DELETE"}, nil
}
func (a *App) setTeamGrant(ctx context.Context, tx pgx.Tx, u User, r *http.Request, b Object) (Object, error) {
	tid, pid, team := r.PathValue("tid"), r.PathValue("pid"), r.PathValue("team")
	level := projectRoleLevel(str(b, "role"))
	if r.Method != "DELETE" && level == 0 {
		return nil, problem(422, "INVALID_ROLE", "项目角色无效")
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects p JOIN teams t ON t.tenant_id=p.tenant_id WHERE p.tenant_id=$1 AND p.id=$2 AND t.id=$3)`, tid, pid, team).Scan(&ok)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, problem(404, "NOT_FOUND", "项目或团队不存在")
	}
	if r.Method == "DELETE" {
		_, err = tx.Exec(ctx, `DELETE FROM team_grants WHERE tenant_id=$1 AND project_id=$2 AND team_id=$3`, tid, pid, team)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO team_grants VALUES($1,$2,$3,$4) ON CONFLICT(project_id,team_id) DO UPDATE SET level=EXCLUDED.level`, tid, pid, team, level)
	}
	if err != nil {
		return nil, err
	}
	if err = recomputeGrants(ctx, tx, tid); err != nil {
		return nil, err
	}
	return Object{"project_id": pid, "team_id": team, "role": grantRole(level), "removed": r.Method == "DELETE"}, nil
}
func (a *App) projectTeamGrants(w http.ResponseWriter, r *http.Request, u User) {
	tid, pid := r.PathValue("tid"), r.PathValue("pid")
	ok, err := grantManager(r.Context(), a.DB, u, tid, pid)
	if err != nil {
		fail(w, err)
		return
	}
	if !ok {
		fail(w, problem(404, "NOT_FOUND", "没有项目授权管理权限"))
		return
	}
	rows, err := collect(r.Context(), a.DB, `SELECT t.id,t.name,coalesce(g.level,0) AS level,(SELECT count(*) FROM team_memberships tm JOIN memberships m ON m.id=tm.membership_id WHERE tm.team_id=t.id AND m.status='active') AS member_count FROM teams t LEFT JOIN team_grants g ON g.team_id=t.id AND g.project_id=$2 WHERE t.tenant_id=$1 ORDER BY t.name`, tid, pid)
	if err != nil {
		fail(w, err)
		return
	}
	for _, row := range rows {
		row["role"] = grantRole(grantLevel(row["level"]))
		delete(row, "level")
	}
	JSON(w, 200, Object{"items": rows})
}
func (a *App) memberPermissions(w http.ResponseWriter, r *http.Request, u User) {
	ctx := r.Context()
	tid, mid := r.PathValue("tid"), r.PathValue("mid")
	admin, err := canManage(ctx, a.DB, u, tid)
	if err != nil {
		fail(w, err)
		return
	}
	var actorMember bool
	if err = a.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=$1 AND user_id=$2 AND status='active')`, tid, u.ID).Scan(&actorMember); err != nil {
		fail(w, err)
		return
	}
	if !actorMember {
		fail(w, problem(404, "NOT_FOUND", "未加入该租户"))
		return
	}
	members, err := collect(ctx, a.DB, `SELECT m.id,m.user_id,m.role,m.status,u.name,u.status AS user_status,u.operator,t.status AS tenant_status FROM memberships m JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id WHERE m.tenant_id=$1 AND (($2='' AND m.user_id=$3) OR m.id=$2) AND ($4 OR m.user_id=$3) AND m.status<>'removed'`, tid, mid, u.ID, admin)
	if err != nil {
		fail(w, err)
		return
	}
	if len(members) != 1 {
		fail(w, problem(404, "NOT_FOUND", "成员不存在或不可查看"))
		return
	}
	m := members[0]
	mid = m["id"].(string)
	active := m["status"] == "active" && m["user_status"] == "active" && m["tenant_status"] == "active"
	tenantAdmin := m["role"] == "admin" && m["status"] == "active" && m["user_status"] == "active"
	sources, err := collect(ctx, a.DB, `SELECT d.project_id,'direct' AS kind,d.membership_id AS id,'直接分配' AS name,d.level FROM direct_grants d WHERE d.tenant_id=$1 AND d.membership_id=$2 UNION ALL SELECT g.project_id,'team',t.id,t.name,g.level FROM team_grants g JOIN teams t ON t.id=g.team_id JOIN team_memberships tm ON tm.team_id=t.id WHERE g.tenant_id=$1 AND tm.membership_id=$2 ORDER BY project_id,kind,name`, tid, mid)
	if err != nil {
		fail(w, err)
		return
	}
	for _, s := range sources {
		s["role"] = grantRole(grantLevel(s["level"]))
		delete(s, "level")
	}
	projects, err := collect(ctx, a.DB, `SELECT p.id,p.name,coalesce(g.desired_level,0) AS desired_level,coalesce(g.observed_level,0) AS observed_level,coalesce(g.effective_level,0) AS effective_level FROM projects p LEFT JOIN grants g ON g.project_id=p.id AND g.membership_id=$2 WHERE p.tenant_id=$1 AND ($3 OR g.membership_id IS NOT NULL) ORDER BY p.name`, tid, mid, tenantAdmin)
	if err != nil {
		fail(w, err)
		return
	}
	for _, p := range projects {
		desired := grantLevel(p["desired_level"])
		level := min(desired, grantLevel(p["observed_level"]), grantLevel(p["effective_level"]))
		reason := "已生效"
		if !active {
			level = 0
			reason = "租户、成员或账号未激活"
		} else if desired == 0 {
			reason = "未分配项目代码角色"
		} else if level < desired {
			reason = "等待 GitLab 确认，暂按当前有效角色执行"
		}
		p["desiredRole"] = grantRole(desired)
		p["effectiveRole"] = grantRole(level)
		p["permissions"] = projectPermissions(level, tenantAdmin, active)
		p["reason"] = reason
		own := []Object{}
		for _, s := range sources {
			if s["project_id"] == p["id"] {
				own = append(own, s)
			}
		}
		p["sources"] = own
		delete(p, "desired_level")
		delete(p, "observed_level")
		delete(p, "effective_level")
	}
	teams, err := collect(ctx, a.DB, `SELECT t.id,t.name FROM teams t JOIN team_memberships tm ON tm.team_id=t.id WHERE tm.tenant_id=$1 AND tm.membership_id=$2 ORDER BY t.name`, tid, mid)
	if err != nil {
		fail(w, err)
		return
	}
	JSON(w, 200, Object{"member": m, "teams": teams, "tenantPermissions": tenantPermissions(tenantAdmin, active), "projects": projects})
}

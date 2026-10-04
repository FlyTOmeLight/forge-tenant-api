package platform

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func (a *App) Handler() http.Handler {
	m := gin.New()
	m.HandleMethodNotAllowed = true
	m.RedirectTrailingSlash = false
	_ = m.SetTrustedProxies(nil)
	m.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
		slog.Error("HTTP handler panic", "route", c.FullPath())
		c.AbortWithStatusJSON(500, Object{"error": Object{"code": "INTERNAL_ERROR", "message": "服务暂不可用"}})
	}))
	route(m, "POST", "/internal/v1/session/check", a.service(a.checkSession))
	route(m, "POST", "/internal/v1/projects/resolve", a.service(a.resolveProject))
	route(m, "GET", "/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if e := a.DB.Ping(ctx); e != nil {
			JSON(w, 503, Object{"status": "unavailable"})
			return
		}
		JSON(w, 200, Object{"status": "ok"})
	})
	route(m, "GET", "/auth/login", a.login)
	route(m, "GET", "/auth/callback", a.callback)
	route(m, "GET", "/api/get-app-login", a.identityProxy)
	route(m, "POST", "/api/login", a.identityProxy)
	route(m, "POST", "/auth/logout", a.authorized(func(w http.ResponseWriter, r *http.Request, u User) {
		c, _ := r.Cookie("forge_session")
		_, e := a.DB.Exec(r.Context(), "DELETE FROM sessions WHERE token_hash=$1", Hash(c.Value))
		if e != nil {
			fail(w, e)
			return
		}
		a.cookie(w, "forge_session", "", -1)
		JSON(w, 200, Object{"ok": true})
	}))
	route(m, "GET", "/api/v1/me", a.authorized(a.me))
	route(m, "GET", "/api/v1/platform/users", a.authorized(a.listUsers))
	route(m, "GET", "/api/v1/platform/tenants", a.authorized(a.listPlatformTenants))
	route(m, "GET", "/api/v1/tenants/:tid/projects/:pid/people", a.authorized(a.projectPeople))
	route(m, "GET", "/api/v1/tenants", a.authorized(a.listTenants))
	route(m, "GET", "/api/v1/roles", a.authorized(a.roles))
	route(m, "GET", "/api/v1/tenants/:tid/permissions/me", a.authorized(a.memberPermissions))
	route(m, "GET", "/api/v1/tenants/:tid/members/:mid/permissions", a.authorized(a.memberPermissions))
	route(m, "PUT", "/api/v1/tenants/:tid/teams/:team/members/:mid", a.authorized(a.mutate("add_team_member", a.setTeamMember)))
	route(m, "DELETE", "/api/v1/tenants/:tid/teams/:team/members/:mid", a.authorized(a.mutate("remove_team_member", a.setTeamMember)))
	route(m, "GET", "/api/v1/tenants/:tid/projects/:pid/team-grants", a.authorized(a.projectTeamGrants))
	route(m, "PUT", "/api/v1/tenants/:tid/projects/:pid/team-grants/:team", a.authorized(a.mutate("set_team_grant", a.setTeamGrant)))
	route(m, "DELETE", "/api/v1/tenants/:tid/projects/:pid/team-grants/:team", a.authorized(a.mutate("revoke_team_grant", a.setTeamGrant)))
	route(m, "GET", "/api/v1/tenants/:tid", a.authorized(a.snapshot))
	route(m, "GET", "/api/v1/tenants/:tid/projects/:pid/grant-candidates", a.authorized(a.grantCandidates))
	route(m, "GET", "/api/v1/tenants/:tid/projects/:pid/access", a.authorized(a.selfAccess))
	route(m, "POST", "/api/v1/platform/tenants", a.authorized(a.mutate("create_tenant", a.createTenant)))
	route(m, "PATCH", "/api/v1/platform/tenants/:tid", a.authorized(a.mutate("update_tenant", a.updateTenant)))
	route(m, "POST", "/api/v1/tenants/:tid/members", a.authorized(a.mutate("invite_member", a.addMember)))
	route(m, "PATCH", "/api/v1/tenants/:tid/members/:mid", a.authorized(a.mutate("update_member", a.updateMember)))
	route(m, "DELETE", "/api/v1/tenants/:tid/members/:mid", a.authorized(a.mutate("remove_member", a.removeMember)))
	route(m, "POST", "/api/v1/tenants/:tid/members/:mid/invitation", a.authorized(a.mutate("resend_invitation", a.resendInvitation)))
	route(m, "POST", "/api/v1/tenants/:tid/teams", a.authorized(a.mutate("create_team", a.createTeam)))
	route(m, "POST", "/api/v1/tenants/:tid/projects", a.authorized(a.mutate("add_project", a.addProject)))
	route(m, "PUT", "/api/v1/tenants/:tid/projects/:pid/grants/:mid", a.authorized(a.mutate("set_grant", a.setGrant)))
	route(m, "DELETE", "/api/v1/tenants/:tid/projects/:pid/grants/:mid", a.authorized(a.mutate("revoke_grant", a.setGrant)))
	route(m, "POST", "/api/v1/tenants/:tid/operations/:oid/retry", a.authorized(a.mutate("retry_sync", a.retryOperation)))
	route(m, "POST", "/api/v1/invitations/inspect", a.invitationInfo)
	route(m, "POST", "/api/v1/invitations/accept", a.acceptInvitation)
	route(m, "POST", "/internal/v1/access/check", a.service(a.checkAccess))
	route(m, "GET", "/internal/v1/access/events", a.service(a.accessEvents))
	m.NoRoute(func(c *gin.Context) {
		JSON(c.Writer, 404, Object{"error": Object{"code": "NOT_FOUND", "message": "接口不存在"}})
	})
	m.NoMethod(func(c *gin.Context) {
		JSON(c.Writer, 405, Object{"error": Object{"code": "METHOD_NOT_ALLOWED", "message": "请求方法不支持"}})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")

		m.ServeHTTP(w, r)
	})
}
func (a *App) me(w http.ResponseWriter, r *http.Request, u User) {
	_, csrf, e := a.authenticate(r)
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, Object{"user": u, "csrf_token": csrf, "permissions": Object{"managePlatform": u.Operator}})
}
func (a *App) listUsers(w http.ResponseWriter, r *http.Request, u User) {
	if !u.Operator {
		fail(w, problem(403, "FORBIDDEN", "需要平台运营权限"))
		return
	}
	rows, e := collect(r.Context(), a.DB, `SELECT id,name,email,status,operator,gitlab_id FROM users WHERE status='active' ORDER BY created_at LIMIT 200`)
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, Object{"items": rows})
}
func (a *App) listTenants(w http.ResponseWriter, r *http.Request, u User) {
	rows, e := collect(r.Context(), a.DB, `SELECT t.*, (SELECT count(*) FROM memberships m WHERE m.tenant_id=t.id AND m.status<>'removed') AS member_count,(SELECT count(*) FROM projects p WHERE p.tenant_id=t.id) AS project_count,coalesce((SELECT status FROM operations o WHERE o.tenant_id=t.id AND o.version=t.version),'pending') AS sync_status FROM tenants t WHERE EXISTS(SELECT 1 FROM memberships m WHERE m.tenant_id=t.id AND m.user_id=$1 AND m.status='active') ORDER BY t.created_at DESC LIMIT 200`, u.ID)
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, Object{"items": rows})
}
func canManage(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, u User, tid string) (bool, error) {
	var ok bool
	e := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=$1 AND user_id=$2 AND role='admin' AND status='active')`, tid, u.ID).Scan(&ok)
	return ok, e
}
func (a *App) snapshot(w http.ResponseWriter, r *http.Request, u User) {
	ctx := r.Context()
	tid := r.PathValue("tid")
	admin, e := canManage(ctx, a.DB, u, tid)
	if e != nil {
		fail(w, e)
		return
	}
	ts, e := collect(ctx, a.DB, `SELECT t.* FROM tenants t WHERE id=$1 AND ($2 OR (status='active' AND EXISTS(SELECT 1 FROM memberships m WHERE m.tenant_id=t.id AND m.user_id=$3 AND m.status='active')))`, tid, admin, u.ID)
	if e != nil {
		fail(w, e)
		return
	}
	if len(ts) != 1 {
		fail(w, problem(404, "NOT_FOUND", "企业不存在或不可访问"))
		return
	}
	result := Object{"tenant": ts[0], "permissions": tenantPermissions(admin, ts[0]["status"] == "active"), "teams": []Object{}, "operations": []Object{}, "audit": []Object{}}
	queries := []struct {
		name, sql string
		args      []any
	}{
		{"members", `SELECT m.*,u.name,u.email,u.status AS user_status,u.gitlab_id,coalesce((SELECT jsonb_agg(jsonb_build_object('id',t.id,'name',t.name) ORDER BY t.name) FROM team_memberships tm JOIN teams t ON t.id=tm.team_id WHERE tm.membership_id=m.id),'[]'::jsonb) AS teams FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.status<>'removed' AND ($2 OR m.user_id=$3) ORDER BY u.name LIMIT 500`, []any{tid, admin, u.ID}},
		{"projects", `SELECT p.* FROM projects p WHERE p.tenant_id=$1 AND ($2 OR EXISTS(SELECT 1 FROM grants g JOIN memberships m ON m.id=g.membership_id WHERE g.project_id=p.id AND m.user_id=$3 AND m.status='active' AND least(g.desired_level,g.observed_level,g.effective_level)>=20)) ORDER BY p.name LIMIT 500`, []any{tid, admin, u.ID}},
		{"grants", `SELECT g.*,coalesce(d.level,0) AS direct_level,
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',t.id,'name',t.name,'level',tg.level)) FROM team_grants tg JOIN teams t ON t.id=tg.team_id JOIN team_memberships tm ON tm.team_id=t.id WHERE tg.project_id=g.project_id AND tm.membership_id=g.membership_id),'[]'::jsonb) AS team_sources,u.name,u.email,m.status AS member_status,u.status AS user_status FROM grants g LEFT JOIN direct_grants d ON d.project_id=g.project_id AND d.membership_id=g.membership_id JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id WHERE g.tenant_id=$1 AND ($2 OR m.user_id=$3 OR EXISTS(SELECT 1 FROM grants own JOIN memberships om ON om.id=own.membership_id WHERE own.project_id=g.project_id AND om.user_id=$3 AND om.status='active' AND least(own.desired_level,own.observed_level,own.effective_level)>=40)) ORDER BY g.project_id,u.name LIMIT 1000`, []any{tid, admin, u.ID}},
	}
	if admin {
		queries = append(queries, struct {
			name, sql string
			args      []any
		}{"teams", `SELECT t.*,(SELECT count(*) FROM team_memberships tm JOIN memberships m ON m.id=tm.membership_id WHERE tm.team_id=t.id AND m.status='active') AS member_count FROM teams t WHERE tenant_id=$1 ORDER BY name LIMIT 500`, []any{tid}}, struct {
			name, sql string
			args      []any
		}{"operations", `SELECT * FROM operations WHERE tenant_id=$1 ORDER BY version DESC LIMIT 100`, []any{tid}}, struct {
			name, sql string
			args      []any
		}{"audit", `SELECT * FROM audit_events WHERE tenant_id=$1 ORDER BY id DESC LIMIT 100`, []any{tid}})
	}
	for _, q := range queries {
		rows, e := collect(ctx, a.DB, q.sql, q.args...)
		if e != nil {
			fail(w, e)
			return
		}
		if q.name == "projects" {
			for _, p := range rows {
				access, err := a.access(ctx, u.ID, tid, p["id"].(string))
				if err != nil {
					fail(w, err)
					return
				}
				level, _ := access["level"].(int)
				p["permissions"] = projectPermissions(level, admin, ts[0]["status"] == "active")
			}
		}
		if q.name == "grants" {
			for _, g := range rows {
				g["direct_role"] = grantRole(grantLevel(g["direct_level"]))
				sources := []Object{}
				if g["direct_role"] != "none" {
					sources = append(sources, Object{"kind": "direct", "id": g["membership_id"], "name": "直接分配", "role": g["direct_role"]})
				}
				if teams, ok := g["team_sources"].([]any); ok {
					for _, v := range teams {
						t := v.(map[string]any)
						sources = append(sources, Object{"kind": "team", "id": t["id"], "name": t["name"], "role": grantRole(grantLevel(t["level"]))})
					}
				}
				g["sources"] = sources
				delete(g, "team_sources")
				delete(g, "direct_level")
				g["desired_role"] = grantRole(grantLevel(g["desired_level"]))
				g["observed_role"] = grantRole(grantLevel(g["observed_level"]))
				level := min(grantLevel(g["desired_level"]), grantLevel(g["observed_level"]), grantLevel(g["effective_level"]))
				if ts[0]["status"] != "active" || g["member_status"] != "active" || g["user_status"] != "active" {
					level = 0
				}
				g["effective_role"] = grantRole(level)
			}
		}
		result[q.name] = rows
	}
	JSON(w, 200, result)
}
func (a *App) access(ctx context.Context, uid, tid, pid string) (Object, error) {
	var desired, observed, effective int
	var version int64
	var tenantState, memberState, userState string
	e := a.DB.QueryRow(ctx, `SELECT g.desired_level,g.observed_level,g.effective_level,t.version,t.status,m.status,u.status FROM grants g JOIN memberships m ON m.id=g.membership_id JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=g.tenant_id WHERE g.tenant_id=$1 AND g.project_id=$2 AND m.user_id=$3`, tid, pid, uid).Scan(&desired, &observed, &effective, &version, &tenantState, &memberState, &userState)
	if errors.Is(e, pgx.ErrNoRows) {
		return Object{"allowed": false, "level": 0, "reason": "NO_GRANT"}, nil
	}
	if e != nil {
		return nil, e
	}
	level := min(desired, observed, effective)
	if tenantState != "active" || memberState != "active" || userState != "active" {
		level = 0
	}
	return Object{"allowed": level > 0, "level": level, "read": level >= 20, "write": level >= 30, "manage": level >= 40, "authorization_version": version, "tenant_id": tid, "project_id": pid}, nil
}
func (a *App) selfAccess(w http.ResponseWriter, r *http.Request, u User) {
	v, e := a.access(r.Context(), u.ID, r.PathValue("tid"), r.PathValue("pid"))
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, v)
}
func (a *App) service(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(a.Config.ServiceToken)) != 1 {
			fail(w, problem(401, "SERVICE_AUTH_REQUIRED", "服务身份校验失败"))
			return
		}
		fn(w, r)
	}
}
func (a *App) checkAccess(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID    string `json:"user_id"`
		TenantID  string `json:"tenant_id"`
		ProjectID string `json:"project_id"`
		Action    string `json:"action"`
	}
	if e := readJSON(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Action != "read" && in.Action != "write" && in.Action != "manage" {
		fail(w, problem(422, "INVALID_ACTION", "action 必须为 read、write 或 manage"))
		return
	}
	v, e := a.access(r.Context(), in.UserID, in.TenantID, in.ProjectID)
	if e != nil {
		fail(w, e)
		return
	}
	v["allowed"] = v[in.Action] == true
	JSON(w, 200, v)
}
func (a *App) accessEvents(w http.ResponseWriter, r *http.Request) {
	after, e := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if r.URL.Query().Get("after") == "" {
		after = 0
		e = nil
	}
	if e != nil || after < 0 {
		fail(w, problem(422, "INVALID_CURSOR", "游标无效"))
		return
	}
	rows, e := collect(r.Context(), a.DB, `SELECT * FROM access_events WHERE id>$1 ORDER BY id LIMIT 200`, after)
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, Object{"items": rows})
}
func mustReadLimited(r io.Reader) []byte {
	b, e := io.ReadAll(io.LimitReader(r, 1<<20))
	if e != nil {
		return []byte(`{"status":"error","msg":"身份响应读取失败"}`)
	}
	return b
}

func (a *App) grantCandidates(w http.ResponseWriter, r *http.Request, u User) {
	ctx := r.Context()
	tid, pid := r.PathValue("tid"), r.PathValue("pid")
	allowed, e := grantManager(ctx, a.DB, u, tid, pid)
	if e != nil {
		fail(w, e)
		return
	}
	if !allowed {
		fail(w, problem(404, "NOT_FOUND", "项目不存在或无授权管理权限"))
		return
	}
	rows, e := collect(ctx, a.DB, `SELECT m.id,u.name,u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.status='active' AND u.status='active' ORDER BY u.name LIMIT 500`, tid)
	if e != nil {
		fail(w, e)
		return
	}
	JSON(w, 200, Object{"items": rows})
}

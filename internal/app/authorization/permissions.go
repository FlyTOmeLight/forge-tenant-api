package authorization

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

type RoleDefinition struct {
	ID          string   `json:"id"`
	Scope       string   `json:"scope"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	Conditions  []string `json:"conditions"`
}

type ProjectPermissions struct {
	ViewProject        bool `json:"viewProject"`
	ReadCode           bool `json:"readCode"`
	CreateTask         bool `json:"createTask"`
	ManageMembers      bool `json:"manageMembers"`
	ConfigureExecution bool `json:"configureExecution"`
}

func RoleDefinitions() []RoleDefinition {
	return []RoleDefinition{
		{
			ID:          "operator",
			Scope:       "platform",
			Name:        "平台管理员",
			Permissions: []string{"开通、启用和停用租户", "查看全局服务连接", "指定首位租户管理员"},
			Conditions:  []string{"进入租户需要成员身份，代码操作需要项目授权"},
		},
		{
			ID:          "admin",
			Scope:       "tenant",
			Name:        "租户管理员",
			Permissions: []string{"邀请和管理租户成员", "管理团队成员", "接入项目和分配项目角色", "查看审计和权限同步"},
			Conditions:  []string{"读取代码、创建任务和配置执行需要项目角色"},
		},
		{
			ID:          "member",
			Scope:       "tenant",
			Name:        "普通成员",
			Permissions: []string{"查看自己的角色和权限"},
			Conditions:  []string{"项目能力由直接授权和团队授权决定"},
		},
		{
			ID:          "reviewer",
			Scope:       "project",
			Name:        "审查者",
			Permissions: []string{"读取代码和任务", "查看项目执行配置", "参与任务审查"},
			Conditions:  []string{"提交认可或审查反馈需要被指定为该任务的审查者"},
		},
		{
			ID:          "developer",
			Scope:       "project",
			Name:        "开发者",
			Permissions: []string{"读取代码和任务", "查看项目执行配置", "创建任务并选择审查者", "控制本人负责的任务", "参与任务审查"},
			Conditions:  []string{"控制任务需具有负责人或项目管理资格；审查需被指定"},
		},
		{
			ID:          "admin",
			Scope:       "project",
			Name:        "项目管理员",
			Permissions: []string{"读取代码和任务", "创建任务并选择审查者", "管理项目成员与团队授权", "配置项目执行", "管理项目任务", "参与任务审查"},
			Conditions:  []string{"认可代码仍需被指定为审查者；GitLab 分支策略单独生效"},
		},
	}
}

func ProjectCapabilities(level int, tenantAdmin, active bool) ProjectPermissions {
	return ProjectPermissions{
		ViewProject:        tenantAdmin || level >= 20,
		ReadCode:           level >= 20,
		CreateTask:         level >= 30,
		ManageMembers:      active && (tenantAdmin || level >= 40),
		ConfigureExecution: level >= 40,
	}
}

func TenantPermissions(admin, active bool) Object {
	return Object{
		"manageMembers":  admin && active,
		"manageProjects": admin && active,
		"viewSettings":   admin,
		"viewAudit":      admin,
		"viewSync":       admin,
	}
}

func GrantLevel(v any) int {
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

func GrantRole(level int) string {
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

func ProjectRoleLevel(role string) int {
	return map[string]int{"reviewer": 20, "developer": 30, "admin": 40}[role]
}

func CanManage(ctx context.Context, db *gorm.DB, u identity.User, tid string) (bool, error) {
	record, err := table.TenantMemberState(ctx, db, tid, u.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return record.Role == "admin" && record.Status == "active", err
}

func GrantManager(ctx context.Context, db *gorm.DB, u identity.User, tid, pid string) (bool, error) {
	record, err := table.ProjectMemberState(ctx, db, tid, pid, u.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	active := record.TenantStatus == "active" && record.MemberStatus == "active" && record.UserStatus == "active"
	return active && (record.Role == "admin" || min(record.Desired, record.Observed, record.Effective) >= 40), err
}

func (a *Service) Access(ctx context.Context, uid, tid, pid string) (*tenantv1.CheckAccessResponse, error) {
	record, e := table.ProjectAuthorization(ctx, a.DB, tid, pid, uid)

	if errors.Is(e, sql.ErrNoRows) {
		return &tenantv1.CheckAccessResponse{Reason: "NO_GRANT"}, nil
	}
	if e != nil {
		return nil, e
	}
	level := min(record.Desired, record.Observed, record.Effective)
	if record.TenantStatus != "active" || record.MemberStatus != "active" || record.UserStatus != "active" {
		level = 0
	}
	return &tenantv1.CheckAccessResponse{
		Allowed:              level > 0,
		Level:                level,
		Read:                 level >= 20,
		Write:                level >= 30,
		Manage:               level >= 40,
		AuthorizationVersion: record.Version,
		TenantID:             tid,
		ProjectID:            pid,
	}, nil
}

// Source roles are combined in the business layer, inside the caller's transaction.
func RecomputeGrants(ctx context.Context, tx *gorm.DB, tid string) error {
	sources, err := table.LoadGrantSources(ctx, tx, tid)
	if err != nil {
		return err
	}
	type key struct{ projectID, membershipID string }
	targets := make(map[key]table.ComputedGrant)
	for _, source := range sources {
		id := key{projectID: source.ProjectID, membershipID: source.MembershipID}
		target := targets[id]
		target.ProjectID = source.ProjectID
		target.MembershipID = source.MembershipID
		if source.Status == "active" {
			target.Desired = max(target.Desired, source.Level)
		}
		target.Effective = min(source.Effective, target.Desired)
		targets[id] = target
	}
	if len(targets) == 0 {
		return nil
	}
	computed := make([]table.ComputedGrant, 0, len(targets))
	for _, target := range targets {
		computed = append(computed, target)
	}
	_, err = table.SaveComputedGrants(ctx, tx, tid, computed)
	return err
}

func (a *Service) ProjectRead(ctx context.Context, u identity.User, tid, pid string) (int, error) {
	v, e := a.Access(ctx, u.ID, tid, pid)
	if e != nil {
		return 0, e
	}
	level := v.Level
	if level < 20 {
		return 0, apperror.New(404, "NOT_FOUND", "项目不存在或无代码访问权限")
	}
	return level, nil
}

# HTTP API

浏览器使用同域 Cookie 会话。`GET /api/v1/me` 返回当前用户和 `csrf_token`；写请求须同时提供匹配 `PUBLIC_URL` 的 Origin 与 `X-CSRF-Token`。除邀请接受外的租户写操作要求 `Idempotency-Key`；已存在企业还要求 `If-Match: "<tenant.version>"`。

写操作返回 202、`operation_id` 和新的企业 `version`。202 表示目标状态已持久化；GitLab 是否落实需要读取同步操作。成员邀请响应另外返回一次性 `invitation_url`，不自动发送邮件。

| 方法与路径 | 行为与权限 |
| --- | --- |
| `GET /api/v1/me` | 当前激活身份、CSRF token、`permissions.managePlatform` |
| `GET /api/v1/tenants` | 当前用户以有效成员身份加入的租户 |
| `GET /api/v1/platform/tenants` | 平台管理员查询全部租户元数据 |
| `POST /api/v1/platform/tenants` | 平台管理员创建租户；body: name, slug, admin_user_id（必填，明确指定首位管理员） |
| `GET /api/v1/tenants/{tid}` | 企业快照；后端过滤成员、项目、授权、操作和审计范围 |
| `PATCH /api/v1/platform/tenants/{tid}` | 运营设置 status=active / suspended |
| `GET /api/v1/platform/users` | 运营查询已纳入平台的统一账号 |
| `POST /api/v1/tenants/{tid}/members` | 企业管理员邀请成员；name, email, role=admin / member |
| `PATCH /api/v1/tenants/{tid}/members/{mid}` | 修改 role 和 team_id；保护最后一位有效管理员 |
| `DELETE /api/v1/tenants/{tid}/members/{mid}` | 立即撤销企业访问，并安排项目撤权；保留统一账号 |
| `POST /api/v1/tenants/{tid}/members/{mid}/invitation` | 重新邀请；使旧链接失效 |
| `POST /api/v1/tenants/{tid}/teams` | 创建团队；name；不授予权限 |
| `POST /api/v1/tenants/{tid}/projects` | 企业管理员接入项目；name, path（根 Group 下后缀）, mode=create / bind |
| `GET /api/v1/tenants/{tid}/projects/{pid}/grant-candidates` | 企业 / 该项目管理员查询可授权成员 |
| `PUT /api/v1/tenants/{tid}/projects/{pid}/grants/{mid}` | 企业 / 该项目管理员设置 role=admin / developer / reviewer |
| `DELETE /api/v1/tenants/{tid}/projects/{pid}/grants/{mid}` | 企业 / 该项目管理员撤销项目授权 |
| `GET /api/v1/tenants/{tid}/projects/{pid}/access` | 当前用户实际平台权限；没有授权时 allowed=false |
| `POST /api/v1/tenants/{tid}/operations/{oid}/retry` | 企业管理员对当前 failed / conflict 目标发起新版本重试 |
| `POST /api/v1/invitations/inspect` | token 检查；只接受同源请求 |
| `POST /api/v1/invitations/accept` | token, password（仅新账号）；已有账号必须登录匹配身份 |
| `GET /api/v1/platform/connections` | 平台管理员检查 GitLab、模型和身份接入；不返回管理凭证 |
| `POST /auth/logout` | 删除服务端会话并清除 Cookie |

现阶段操作和审计通过企业快照返回，设计稿中的独立列表端点尚未拆出。设计稿是目标设计，本文件记录当前可调用接口。

## 控制台权限边界

平台管理员只管理租户生命周期和全局连接。进入租户工作区必须有有效成员身份。创建租户不会自动给操作者增加成员身份，只有 `admin_user_id` 指定的账号成为首位管理员。已有成员关系不变。

租户管理员管理成员、团队和项目授权；读取代码、创建任务、配置执行仍需有效项目授权。项目管理员只管理所属项目的成员和执行配置。任务操作还校验负责人或指定审查者资格。

- 快照 `permissions`：`manageMembers`、`manageProjects`、`viewSettings`、`viewAudit`、`viewSync`。
- 每个项目的 `permissions`：`viewProject`、`readCode`、`createTask`、`manageMembers`、`configureExecution`。
- `GET /api/v1/tenants/{tid}/projects/{pid}/people`：有项目读取权限者查询同项目有效协作者。返回 `id`、`name`、`canReview`、`canOwn`；仅用于审查者、负责人选择，不授予租户通讯录权限。
- 授权条目提供 `desired_role`、`observed_role`、`effective_role` 供展示。前端按命名能力显示操作，后端每次请求重新鉴权。

停用租户后禁止组织写操作和代码访问，租户管理员仍可查看设置、同步和审计。重新启用由平台管理员操作。

## 授权字段

`desired_level` 是目标 GitLab 角色，`observed_level` 是最近核验值，`effective_level` 是平台已经确认可授予的能力上限。实际能力还取决于账号、企业与成员状态。

- 新增 / 升级：上限保持原值，当前版本同步确认后才能提高。
- 降级 / 撤销：事务中立即降低上限，不等 GitLab。
- 停用 / 启用：清零上限；启用后重新核验，再恢复资格。
- 旧同步结果：可以记录观察事实，但不能提高当前权限上限。

角色值：0 无权限，20 审查者，30 开发者，40 项目管理员。实例管理员和非 active GitLab 用户不纳入自动授权；受保护分支策略仍由 GitLab 决定。

## 常见错误

```json
{"error":{"code":"LAST_TENANT_ADMIN","message":"请先指定另一位已激活的企业管理员"}}
```

| HTTP | 代码示例 | 含义 |
| --- | --- | --- |
| 401 | AUTH_REQUIRED | 登录或会话失效 |
| 403 | CSRF_REJECTED / FORBIDDEN | 来源校验或权限不足 |
| 404 | NOT_FOUND | 不存在或没有此租户 / 项目的访问范围 |
| 409 | LAST_TENANT_ADMIN / IDEMPOTENCY_CONFLICT | 管理员保护或重复请求冲突 |
| 410 | INVITATION_EXPIRED | 邀请到期、被重发替代或已经消费 |
| 412 | VERSION_MISMATCH | 企业在表单打开后变化；重新读取后再操作 |
| 422 | INVALID_ROLE / INVALID_PROJECT | 请求字段无效 |

同一幂等键须对应相同操作者、路径、请求体和版本。幂等结果加密保存在数据库中；重复请求返回原操作，不重复产生业务变更。

## 项目执行配置与新问题任务

`GET /api/v1/platform/connections` 返回 GitLab 与模型探测结果及当前会话的身份接入信息。

项目路径 `/api/v1/tenants/{tid}/projects/{pid}` 下新增：

- `GET /coding-settings`：读取配置、分支、在线执行器和管理权限。
- `PUT /coding-settings`：项目管理员按请求体 `version` 更新配置；保存后使检查状态失效。
- `POST /coding-settings/check`：验证基准分支提交及模型存在，记录该配置版本。
- `POST /tasks/new`：需要项目写权限与已检查配置；以 `Idempotency-Key` 防重，创建 `mrIid=0` 的任务和排队轮次。必填问题、复现及验收条件，`reviewerIds` 支持多位具有读取权限的审查者。

每轮 `config_snapshot` 固定当时配置。旧的 `POST /tasks` 仍可关联现有开放 MR；任务完成结果可携带新生成的 `mrIid` 与独立 Agent 判断。完整契约见 [coding-tasks.md](coding-tasks.md)。

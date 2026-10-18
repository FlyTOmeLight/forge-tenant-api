# HTTP API

公开业务接口接受同域 `forge_session` Cookie 或用户 Bearer API key，同时提供二者返回 `400 AMBIGUOUS_IDENTITY_PROOF`。`GET /api/v1/me`、退出登录和凭据管理仅接受 Cookie 会话；`GET /api/v1/me` 返回当前用户和 `csrf_token`。Cookie 写请求须同时提供匹配 `PUBLIC_URL` 的 Origin 与 `X-CSRF-Token`，Bearer API key 请求不校验 CSRF。除邀请接受外的租户写操作要求 `Idempotency-Key`；已存在企业还要求 `If-Match: "<tenant.version>"`。

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
| `PATCH /api/v1/tenants/{tid}/members/{mid}` | 修改租户 role；团队成员关系使用独立接口；保护最后一位有效管理员 |
| `DELETE /api/v1/tenants/{tid}/members/{mid}` | 立即撤销企业访问，并安排项目撤权；保留统一账号 |
| `POST /api/v1/tenants/{tid}/members/{mid}/invitation` | 重新邀请；使旧链接失效 |
| `POST /api/v1/tenants/{tid}/teams` | 创建团队；name；随后可为团队分配项目角色 |
| `POST /api/v1/tenants/{tid}/projects` | 企业管理员接入项目；name, path（根 Group 下后缀）, mode=create / bind |
| `GET /api/v1/tenants/{tid}/projects/{pid}/grant-candidates` | 企业 / 该项目管理员查询可授权成员 |
| `PUT /api/v1/tenants/{tid}/projects/{pid}/grants/{mid}` | 企业 / 该项目管理员设置个人直接角色 role=admin / developer / reviewer |
| `DELETE /api/v1/tenants/{tid}/projects/{pid}/grants/{mid}` | 企业 / 该项目管理员撤销个人直接授权；团队授权保留 |
| `GET /api/v1/tenants/{tid}/projects/{pid}/access` | 当前用户实际平台权限；没有授权时 allowed=false |
| `POST /api/v1/tenants/{tid}/operations/{oid}/retry` | 企业管理员对当前 failed / conflict 目标发起新版本重试 |
| `POST /api/v1/invitations/inspect` | token 检查；只接受同源请求 |
| `POST /api/v1/invitations/accept` | token, password（仅新账号）；已有账号必须登录匹配身份 |
| `GET /api/v1/platform/connections` | 由网关转发到 forge-coding-api；本服务不注册此接口 |
| `POST /auth/logout` | 删除服务端会话并清除 Cookie；不撤销已签发的 API key |
| `POST /api/v1/credentials` | 仅 Cookie 会话：创建自己的用户 API key，默认 90 天、最长 365 天；响应一次性返回 token |
| `GET /api/v1/credentials` | 仅 Cookie 会话：列出自己的凭据元数据，永不返回 token 或摘要 |
| `DELETE /api/v1/credentials/:id` | 仅 Cookie 会话：撤销自己的凭据，重复撤销视为成功；撤销立即生效 |

用户 API key 形如 `Authorization: Bearer ftk_<43 位 base64url 随机数>`，数据库只保存 SHA-256 摘要。凭据是账号级，不绑定租户或项目；被移出租户后该 key 对该租户立即失效，对其他租户仍然有效。凭据管理只允许 Cookie 会话，避免被盗 key 自我续命；凭据创建不经过幂等响应表。使用 API key 调用租户写接口时，仍须遵守 `Idempotency-Key` 与 `If-Match` 契约。

现阶段操作和审计通过企业快照返回，设计稿中的独立列表端点尚未拆出。设计稿是目标设计，本文件记录当前可调用接口。

## Forge / GitLab 统一登录

Forge 与 GitLab 的浏览器授权入口统一为 `PUBLIC_URL/login/oauth/authorize`。登录页只接收后端已配置的客户端与精确回调地址：Forge 使用 `OIDC_CLIENT_ID` 和 `/auth/callback`，GitLab 使用 `GITLAB_OIDC_CLIENT_ID` 与 `GITLAB_PUBLIC_URL/users/auth/openid_connect/callback`。Forge 回调继续校验一次性 state、PKCE 和 nonce；GitLab 回调由其 OIDC 适配器校验。

身份代理只把 Casdoor 的 `casdoor_session_id` 映射为浏览器的 `forge_identity`。该 Cookie 为 host-only、HttpOnly、SameSite=Lax，HTTPS 部署启用 Secure。请求身份服务时只携带该身份 Cookie，不转发 Forge 会话或管理凭证。登录页发现身份 Cookie 后尝试复用 Casdoor 会话换取当前客户端的授权码；失效时显示密码表单。`POST /auth/logout` 清除 Forge 和身份 Cookie；GitLab 已建立的独立会话仍按 GitLab 的退出流程处理。

GitLab 需将授权端点指向上述 Forge 登录页，并启用 `autoSignInWithProvider: openid_connect`。issuer、token、userinfo 与 JWKS 端点继续使用 Casdoor；关闭 discovery 后显式设置端点，防止 discovery 将授权地址覆盖回旧身份页面。现有用户的 `sub` / `extern_uid` 和项目权限保持原绑定。生产环境使用可访问的 HTTPS 域名，浏览器无需集群 DNS 或证书例外。

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

## Coding 服务集成

任务与执行配置由 [forge-coding-api](https://github.com/FlyTOmeLight/forge-coding-api) 提供。租户 API 不再注册这些路由。

- `POST /internal/v1/identity/resolve`：服务身份调用，提交原始用户请求的证明并返回解析出的用户主体。`{"type":"session","session":..,"method":..,"origin":..,"csrf":..}` 校验 Cookie 会话，写方法必须通过 Origin / CSRF 检查；`{"type":"api_key","api_key":"ftk_..."}` 校验用户 Bearer API key。响应为 `{"id","name","email","operator"}`，不接受调用方自报 user_id；同时提交两种证明返回 `400 AMBIGUOUS_IDENTITY_PROOF`。
- `POST /internal/v1/projects/resolve`：服务身份调用，按 tenant_id 与 project_id 返回 GitLab 项目 ID 和路径，不返回凭据。
- 项目成员与角色、`/internal/v1/access/check` 和持久化撤权事件仍由租户 API 维护。

## 角色与多团队授权

`GET /api/v1/roles` 返回固定角色目录：平台管理员、租户管理员、普通成员，以及项目管理员、开发者、审查者。每个条目包含 `id`、`scope`、`name`、`permissions` 和 `conditions`。当前不支持自定义角色。

| 方法与路径 | 范围与行为 |
| --- | --- |
| `GET /api/v1/tenants/{tid}/permissions/me` | 有效租户成员查看自己的角色、团队、具体能力和项目授权来源；停用租户时仍可查看权限受限原因 |
| `GET /api/v1/tenants/{tid}/members/{mid}/permissions` | 本人或租户管理员查看指定成员权限；项目管理员不能借此读取其他项目权限 |
| `PUT /api/v1/tenants/{tid}/teams/{team}/members/{mid}` | 租户管理员将已激活成员加入团队；允许加入多个团队 |
| `DELETE /api/v1/tenants/{tid}/teams/{team}/members/{mid}` | 租户管理员移出团队，只移除该团队带来的项目授权 |
| `GET /api/v1/tenants/{tid}/projects/{pid}/team-grants` | 租户 / 项目管理员查询可分配团队、人数和已分配角色 |
| `PUT /api/v1/tenants/{tid}/projects/{pid}/team-grants/{team}` | 租户 / 项目管理员为团队分配 role=admin / developer / reviewer |
| `DELETE /api/v1/tenants/{tid}/projects/{pid}/team-grants/{team}` | 撤销该团队在此项目的角色；其他授权来源保留 |

新增写接口沿用 CSRF、`Idempotency-Key`、租户 `If-Match`、审计和同步操作返回契约。团队和项目、成员必须属于同一租户。

权限报告的 `projects[]` 包含 `desiredRole`、`effectiveRole`、`permissions`、`reason`、`sources`；每条来源包含 `kind=direct/team`、`id`、`name`、`role`。租户快照中的成员改为 `teams: [{id,name}]`，移除旧 `team_id` / `team_name` 字段；授权条目新增 `direct_role` 与 `sources`。个人授权写入响应使用 `direct_role`，聚合目标和实际能力从快照或权限报告读取。

项目目标角色取所有直接授权和团队授权中的最高等级。降低目标时立即压低有效能力；提高目标时等待当前版本 GitLab 确认。GitLab 接收计算后的每人项目角色；团队在 Forge 内管理，不额外创建具有继承权限的 GitLab Group。

数据库版本 5 引入 `direct_grants`、`team_memberships`、`team_grants`。原 `grants` 保存聚合目标与远端确认结果，执行器继续读取同一条有效权限链路。迁移把已有正向项目授权保存为直接来源，把旧团队字段转成团队成员关系，再删除旧字段。重复迁移不会恢复已撤销的来源。移出租户清除其直接来源和所有团队关系；重新邀请不会恢复旧授权。

### Enterprise Agent workspace authorization

`POST /internal/v1/workspace/check` requires a service API key and `{ "user_id": "...", "tenant_id": "..." }`. It returns `{allowed:true,canEdit:true,projects:[{id,name,level}]}` for an active enterprise member in an active enterprise. The project list only includes grants whose minimum desired/observed/effective level is at least 20. Enterprise membership does not grant repository access; platform operator status does not bypass membership. Missing or inactive membership returns 403. This endpoint does not synchronize or grant permissions.

## server-go HTTP 接入

Controller 使用原生 Gin，公开写操作绑定明确的请求结构；未知字段或错误字段类型返回 400。业务 Service 的写方法独立执行权限检查、事务、持久化幂等和审计 / outbox。

业务路径、Cookie / CSRF、幂等键、租户版本、响应字段和 `{error:{code,message}}` 错误格式保持现有契约。公开业务路由接受两种身份之一：`forge_session` Cookie 或用户 Bearer API key（`Authorization: Bearer ftk_...`）；同时提交两种返回 400，不静默回退。Cookie 写请求仍校验 Origin 与 `X-CSRF-Token`，API key 请求不做浏览器 CSRF 检查。内部接口要求 `Authorization: Bearer <服务 API key>`（由 `forge service-credential issue` 签发，每个服务一把）；只传裸 token、传用户 API key 或未知 key 都返回 401 `SERVICE_AUTH_REQUIRED`。JSON 参数通过框架严格绑定，拒绝连续文档和结构化请求中的未知字段；请求体上限仍为 64 KiB。写操作保留原始请求字节用于幂等摘要。

框架为请求生成或传递 `Request-ID`，记录方法、路由、状态与耗时。Recovery 在响应提交前返回 JSON 错误；提交后停止处理，不追加第二段错误响应。租户 API 提供 `sdk/go/tenantv1` Go SDK，覆盖身份解析、作用域解析、工作区、项目解析、权限检查与撤权事件；撤权事件仍通过 JSON 游标轮询读取，没有 SSE。

`GET /healthz` 为进程探针，`GET /readyz` 为 PostgreSQL 就绪检查；数据库不可用时后者返回 503。`GET /ping` 和 `GET /version` 由 server-go 提供。

## Go SDK

导入 `github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1`，使用 `tenantv1.New(Config{Endpoint, ServiceAPIKey, Timeout})` 构造客户端，`ServiceAPIKey` 是本服务自己的服务 API key。方法与接口逐一对应：`ResolveIdentity`、`ResolveScope`、`CheckWorkspace`、`ResolveProject`、`CheckAccess`、`ListAccessEvents`。请求、响应类型由 SDK 与服务端共用，现有字段名称和无授权时的响应保持不变。

SDK 还提供 `tenantv1.Identity(MiddlewareConfig{Client, SessionCookie})`：从入站请求提取 Cookie 或用户 Bearer API key，调用 `ResolveIdentity` 并把 `*Principal` 注入 request context，业务代码用 `PrincipalFromContext` 读取；server-go 项目使用子包 `sdk/go/tenantv1/servergo` 的同名中间件，它同时写入框架的用户键和 request context。缺少或无效凭据返回 401，CSRF 失败返回 403，租户服务不可达返回 503 且不执行下游 handler，默认不缓存认证结果。

`*tenantv1.Error` 保留实际 HTTP 状态、业务代码、消息和 Request-ID；网络与 context 错误保留错误身份。默认超时 15 秒，无自动重试或重定向。调用示例、资源关闭和游标责任见 [SDK 接入说明](../sdk/go/tenantv1/README.md)。


## 物料接入作用域解析

`POST /internal/v1/scope/resolve` 使用服务 API key（`Authorization: Bearer`），校验原始用户证明和本次精确作用域。`type=session` 提交 `session`、原始 `method`、`origin`、`csrf`；`type=api_key` 提交用户 `api_key` 和原始 `method`，不做浏览器 CSRF 检查。旧调用省略 `type` 时按 session 处理，同时提供两种凭据拒绝。两种证明均须提交 `tenant_id` 和可选 `project_id`。`project_id` 省略或 null 表示租户直属；空字符串拒绝。写请求不能改写为 GET 或代填 Origin。

成功直接返回 `{"user_id":"u_example","principal":{"id":"u_example","name":"User","email":"user@example.test","operator":false},"tenant_id":"t_example","project_id":null,"ownership_state":"active"}`。必须为有效用户、active 租户及 active 租户成员，operator 不绕过成员资格。项目仅核对其属于本次租户，不查 read/write/manage 或 grants，也不要求已同步 GitLab。

仅支持 active scope；suspended 为 `403 SCOPE_INACTIVE`，不存在或项目不匹配为 `404 SCOPE_NOT_FOUND`，成员资格不足为 `403 SCOPE_ACCESS_DENIED`，无效输入为 `422 INVALID_SCOPE`。会话与 CSRF 沿用 `401 AUTH_REQUIRED`、`403 CSRF_REJECTED`。数据库故障保留服务错误，不推定为 deleted。返回 UserID 和 principal 只能来自已校验的用户证明，不接受调用方自报 UserID、角色或归属状态。

Go SDK：`tenantv1.Client.ResolveScope(ctx, *ResolveScopeRequest)`；请求会输出明确 `project_id:null`，响应不包含 session 或服务凭据。

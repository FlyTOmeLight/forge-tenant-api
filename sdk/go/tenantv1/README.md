# Forge 租户服务 Go SDK

导入路径：`github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1`。SDK 与服务端共用请求和响应类型，保持现有 JSON 字段名称。接口属于可信服务之间的调用，需要租户 API 为本服务签发的服务 API key（`forge service-credential issue <service>`）；用户凭据不能调用内部接口。

```go
package integration

import (
    "context"

    "github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func canWrite(ctx context.Context, endpoint, serviceAPIKey, userID, tenantID, projectID string) (bool, error) {
    client, err := tenantv1.New(tenantv1.Config{
        Endpoint:      endpoint,
        ServiceAPIKey: serviceAPIKey,
    })
    if err != nil {
        return false, err
    }
    defer client.Close()

    access, err := client.CheckAccess(ctx, &tenantv1.CheckAccessRequest{
        UserID:    userID,
        TenantID:  tenantID,
        ProjectID: projectID,
        Action:    tenantv1.AccessWrite,
    })
    if err != nil {
        return false, err
    }
    return access.Allowed, nil
}
```

服务中应在依赖装配阶段创建并复用 Client，在所有调用结束后关闭。`Endpoint` 必须使用 HTTPS，本机回环地址可使用 HTTP。每个方法接收独立的 context 和请求对象，不保存入站会话或用户身份。默认超时 15 秒，可以通过 `Config.Timeout` 调整；不自动重试或跟随重定向。

| 方法 | 用途 |
| --- | --- |
| `ResolveIdentity` | 用原始用户请求的证明解析用户身份：Cookie 会话（附 method、origin、csrf）或用户 Bearer API key |
| `ResolveScope` | 校验 Cookie 会话或用户 API key 与精确的租户 / 项目作用域，返回 Principal、用户 ID 和归属状态 |
| `CheckWorkspace` | 校验租户成员身份，返回可访问的项目 |
| `ResolveProject` | 返回租户内项目的 GitLab ID 和路径 |
| `CheckAccess` | 检查 read / write / manage，并返回当前实际权限及授权版本 |
| `ListAccessEvents` | 使用 `After` 读取最多 200 条持久化授权变更事件 |

## 各端鉴权方式

身份在入口确定后写入 context 和 Gin 键。内部链路只透传，不再回租户服务解析。

| 调用方 | 请求上带什么 | 谁解析身份 | 安装的中间件 | 写入的键 |
| --- | --- | --- | --- | --- |
| **前端（已登录）** | 网关注入 `x-forge-user-id`、`x-forge-tenant-id`，可选 `x-forge-project-id` | 网关已解析，服务只校验网关凭据 | `MiddlewareTrustedIdentity` | `KeyUserID`、`KeyTenantID`、`KeyProjectID` |
| **OpenAPI / 用户 API key** | `Authorization: Bearer ftk_...` 或 `forge_session` Cookie，二选一 | 本服务调租户 API | 仅需身份用 `Identity` / `MiddlewareIdentity`；需要租户或项目权限用 `Authorization` / `MiddlewareAuthorization` | `Principal`，范围通过时还有 `KeyTenantID`、`KeyProjectID` |
| **内部服务调用** | 调用方的 Bearer 服务 key，加上 `x-impersonate-target-*` 三个头 | 上游已解析，下游只校验调用方是内部服务 | `MiddlewareServiceAuth`，需要透传身份时再加 `MiddlewareInternalPropagation` | `Key*` 与 `KeyImpersonateOrigin*` 同为透传的用户，`KeyAuthMode = Internal` |

上游发送身份用 server-go `internalsdk`：打开 `EnableImpersonate`，`Wrap(client, inboundCtx)` 把当前 `KeyUserID`、`KeyTenantID`、`KeyProjectID` 写到 `x-impersonate-target-*`。

## 中间件一览与注册顺序

同一路由只选一种身份来源。下面按注册顺序列出；靠前的先执行。

**面向浏览器和 OpenAPI 的路由**，用 `Identity` 或 `Authorization` 二选一，不叠加：

```go
// net/http：仅需身份
mux.Handle("/documents", tenantv1.Identity(client)(http.HandlerFunc(documents)))

// net/http：身份加租户、项目权限
mux.Handle("GET /tenants/{tid}/projects/{pid}/documents/{id}",
    tenantv1.Authorization(tenantv1.AuthorizationConfig{
        Client: client, Scope: projectScope, Action: tenantv1.AccessRead,
    })(http.HandlerFunc(getDocument)))
```

Gin 上对应 `MiddlewareIdentity(client)` 和 `MiddlewareAuthorization(config)`。`MiddlewareIdentity` 是 `Identity` 的 Gin 形式，两者共用凭据提取和错误映射。

**面向可信网关的路由**，网关已经注入 `x-forge-*`，服务不再问租户：

```go
trusted, err := tenantv1.MiddlewareTrustedIdentity(tenantv1.GatewayConfig{
    Verify: verifyGateway, // 校验请求来自网关，不读取 x-forge-* 身份头
})
gateway := routes.Group("/api", trusted)
```

`GatewayConfig.Verify` 由接入方实现，必须证明请求来自网关，不能读取身份头本身。请求已带身份时返回 403，所以它必须放在 `MiddlewareIdentity` 之前，通常单独成组。

**面向内部服务的路由**，先认证调用方，再采纳透传身份：

```go
serviceAuth := tenantv1.ServiceAuthConfig{
    Authenticate: authenticateService, // 用服务 key 向 forge-tenant 换出调用方服务名
}
caller, err := tenantv1.MiddlewareServiceAuth(serviceAuth)
propagation, err := tenantv1.MiddlewareInternalPropagation(serviceAuth)
internal := routes.Group("/internal", caller, propagation)
```

顺序不能换：`MiddlewareInternalPropagation` 读取 `MiddlewareServiceAuth` 写入的调用方身份。只校验服务身份、不需要用户身份时，只装 `MiddlewareServiceAuth`。`ServiceAuthConfig.Authenticate` 由接入方实现，用调用方的服务 key 向 forge-tenant 换出服务名。凭据被拒绝时返回 `*tenantv1.Error` 且状态为 401，中间件保持 401；其余错误（超时、网络故障、租户服务 5xx）统一返回 503，便于调用方把基础设施故障和凭据拒绝区分开。`Authorize` 可选，用于拒绝特定调用方的透传。

| 中间件 | 运行环境 | 作用 | 放在哪 |
| --- | --- | --- | --- |
| `Identity` | `net/http` | Cookie 或用户 API key 解析成 `Principal` | 浏览器 / OpenAPI 路由，与 `Authorization` 二选一 |
| `Authorization` | `net/http` | 同上，再加 URL 范围和项目权限 | 带租户、项目的业务路由 |
| `MiddlewareIdentity` | Gin | `Identity` 的 Gin 形式，写 `KeyUserID` | 同上 |
| `MiddlewareAuthorization` | Gin | `Authorization` 的 Gin 形式，写 `KeyUserID`、`KeyTenantID`、`KeyProjectID` | 同上 |
| `MiddlewareTrustedIdentity` | Gin | 校验网关后采纳 `x-forge-*` | 网关入口，单独成组，位于身份中间件之前 |
| `MiddlewareServiceAuth` | Gin | 校验内部服务 key，写入调用方服务名 | 内部路由的第一个 |
| `MiddlewareInternalPropagation` | Gin | 采纳 `x-impersonate-target-*` | 紧跟 `MiddlewareServiceAuth` |

凭据规则：Cookie 与 Bearer 不能同时出现，返回 `400 AMBIGUOUS_IDENTITY_PROOF`；`X-User-ID`、`X-Tenant-ID`、`X-Project-ID` 这类用户头和 query 参数不能当作身份或范围来源。

## 身份中间件

业务服务不必自己解析 Cookie。`tenantv1.Identity` 从入站请求取出 Cookie 或用户 Bearer API key，调用 `ResolveIdentity`，把结果放进 request context：

```go
client, err := tenantv1.New(tenantv1.Config{Endpoint: endpoint, ServiceAPIKey: serviceAPIKey})
if err != nil {
    return err
}
handler := tenantv1.Identity(client)(mux)

func documents(w http.ResponseWriter, r *http.Request) {
    principal, ok := tenantv1.PrincipalFromContext(r.Context())
    if !ok {
        http.Error(w, "unauthenticated", http.StatusUnauthorized)
        return
    }
    _ = principal.ID
}
```

请求同时带 Cookie 和 Bearer 时返回 `400 AMBIGUOUS_IDENTITY_PROOF`，不猜测身份；缺少或无效凭据返回 401，写请求的 Origin / CSRF 不匹配返回 403，租户服务不可达、返回服务端错误或调用服务自身的 key 失效时返回 503，且不执行下游 handler。默认不缓存认证结果，每个请求都重新解析。

Gin 上用 `MiddlewareIdentity(client)`，效果相同并写入 `constants.KeyUserID`：

```go
routes.GET("/me", tenantv1.MiddlewareIdentity(client), func(c *core.Context) {
    _ = c.GetString(constants.KeyUserID)
    principal, _ := tenantv1.PrincipalFromContext(c.Request.Context())
    _ = principal.ID
})
```

范围与项目权限见下一节 `Authorization` / `MiddlewareAuthorization`。

## 范围鉴权中间件

需要租户或项目范围的业务路由使用 `tenantv1.Authorization`。`AuthorizationConfig` 声明服务端路由要求，不能从请求头或请求体读取要求的权限：

- `Scope` 只提取 URL 参数。`TenantID`、`ProjectID` 为 `*string`：`nil` 表示未提供，空字符串或首尾空白表示非法，返回 400，不能降级为无范围请求。标准 `net/http` 必须提供此回调。
- `RequireTenant: true` 用于必须带租户的路由。仅验证身份的路由返回 `Scope{}`。
- `Action: tenantv1.AccessRead / AccessWrite / AccessManage` 用于项目路由，同时要求租户和项目参数。缺少任一参数返回 400；提供项目却未配置 Action，或 Action 非法、Client 未初始化、标准适配器缺 Scope 回调，都返回脱敏的 503 并阻断 handler。

```go
// 注册到 ServeMux 后，PathValue 才能读取匹配路由中的 URL 参数。
projectScope := func(r *http.Request) (tenantv1.Scope, error) {
    tid, pid := r.PathValue("tid"), r.PathValue("pid")
    // 路由声明了这两个参数，缺值也保留指针，让中间件拒绝空值。
    return tenantv1.Scope{TenantID: &tid, ProjectID: &pid}, nil
}
mux.Handle("GET /tenants/{tid}/projects/{pid}/documents/{id}",
    tenantv1.Authorization(tenantv1.AuthorizationConfig{
        Client: client,
        Scope: projectScope,
        Action: tenantv1.AccessRead,
    })(http.HandlerFunc(getDocument)))
```

没有范围时中间件调用 `ResolveIdentity`；有租户时只调用 `ResolveScope` 取得身份并验证租户成员资格，项目范围还验证项目属于该租户。项目路由随后调用 `CheckAccess`，只有 `Allowed=true` 且响应范围一致才放行。不会重复调用 `ResolveIdentity`，也不按 HTTP 方法推断项目权限。`ResolveScope` 不负责项目权限，不能单独替代 `CheckAccess`。

Cookie 和 Bearer 使用同一凭据提取规则；Cookie 转发原始 method、Origin 和 CSRF。用户 ID 只取已验证 Principal，`X-User-ID`、`X-Tenant-ID`、`X-Project-ID` 等用户头不作为范围或身份来源。未找到作用域保留 `404 SCOPE_NOT_FOUND`，无权限返回 403；服务调用失败、服务 key 被拒绝和响应身份／范围不一致返回脱敏的 503。任何失败均不进入业务 handler。

`MiddlewareAuthorization(tenantv1.AuthorizationConfig)` 通过 `middleware.HTTPAuthorization` 接入标准 `Authorization` 逻辑：默认从 Gin 路由读取 `:tid`、`:pid`（`Params.Get`，区分“未提供”与空字符串）。也可自定义 `Scope` 回调。成功后写入 `constants.KeyUserID`，范围校验通过时写入 `KeyTenantID`、`KeyProjectID`；`AuthorizationFromContext` 仍可从 `c.Request.Context()` 读取 `Principal` 与 `Action`。

```go
routes.GET("/tenants/:tid/projects/:pid/documents/:id",
    tenantv1.MiddlewareAuthorization(tenantv1.AuthorizationConfig{
        Client: client,
        Action: tenantv1.AccessRead,
    }), documentController)
```

中间件验证用户对 URL 指定项目的权限。业务资源 ID 的归属仍由业务查询保证，例如文档读取必须同时限定 `tenant_id`、`project_id` 和 `id`：

```go
func getDocument(w http.ResponseWriter, r *http.Request) {
    auth, ok := tenantv1.AuthorizationFromContext(r.Context())
    if !ok || auth.TenantID == nil || auth.ProjectID == nil {
        http.Error(w, "scope required", http.StatusForbidden)
        return
    }
    var body string
    err := db.QueryRowContext(r.Context(),
        `SELECT body FROM documents WHERE tenant_id = $1 AND project_id = $2 AND id = $3`,
        *auth.TenantID, *auth.ProjectID, r.PathValue("id")).Scan(&body)
    if errors.Is(err, sql.ErrNoRows) {
        http.NotFound(w, r)
        return
    }
    if err != nil {
        http.Error(w, "document unavailable", http.StatusInternalServerError)
        return
    }
    _, _ = io.WriteString(w, body)
}
```

上述示例假定服务已装配 `db *sql.DB`。若业务只按 `id` 查询，调用方仍可能借已获授权项目的 URL 读取另一项目的资源；中间件无法补上业务查询遗漏的归属条件。更新、删除和列表查询也必须限定已验证范围。

`ResolveScopeRequest.Type` 省略时仍按 Cookie session 处理，保留已有调用；新请求可设置 `Type: IdentityProofAPIKey` 和 `APIKey`，两种证明都须传原始 `Method`。新服务响应提供 `Principal`。底层 SDK 仍能读取旧响应，中间件要求非空且与 `UserID` 一致的 Principal，接入时需配套升级服务端。

## 错误与交付

HTTP 失败返回 `*tenantv1.Error`，通过 `errors.As` 读取 `Status`、`Code`、`Message`、`RequestID`。401 / 403 等错误保留租户 API 的业务代码；代理返回非 JSON 错误时仍保留实际 HTTP 状态。网络错误、取消和超时保留原始错误身份，可以使用 `errors.Is(err, context.Canceled)` 或 `errors.Is(err, context.DeadlineExceeded)`。

身份须来自已验证会话或任务责任链。`CheckAccess` 的 `Allowed=false` 是一次成功查询得到的拒绝结果；接口不可达也应拒绝敏感操作。消费撤权事件时，调用方在成功处理后持久化最后一条事件 ID，SDK 不自动翻页、确认事件或停止执行器。

SDK 随本仓库根模块交付，没有独立 go.mod。依赖 `github.com/FlyTOmeLight/server-go` v0.13.0 及以上（`MiddlewareInternalPropagation` 需要其中的 `auth.InternalPropagatedIdentity`）。

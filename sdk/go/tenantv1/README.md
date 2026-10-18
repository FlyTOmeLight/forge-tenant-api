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

## 身份中间件

业务服务不必自己解析 Cookie。`tenantv1.Identity` 从入站请求取出 Cookie 或用户 Bearer API key，调用 `ResolveIdentity`，把结果放进 request context：

```go
client, err := tenantv1.New(tenantv1.Config{Endpoint: endpoint, ServiceAPIKey: serviceAPIKey})
if err != nil {
    return err
}
handler := tenantv1.Identity(tenantv1.MiddlewareConfig{Client: client})(mux)

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

server-go 项目直接 import `tenantv1.GinIdentity` 与 `tenantv1.GinAuthorization`，它们返回 `core.HandlerFunc`，通过 `github.com/FlyTOmeLight/server-go/middleware` 的 HTTP 适配层接入，同时写入框架用户键、租户键、项目键和 request context，业务代码两种读法一致。

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

`GinAuthorization(tenantv1.AuthorizationConfig)` 默认读取路由 `:tid`、`:pid` 参数，使用 `Params.Get` 保留“未提供”的区别。默认不读取同名 query 参数；标准适配器如需 query 参数，回调必须显式处理缺失、空值和重复值。可提供自己的 Scope 回调替换默认提取方式。成功后会写入 `constants.KeyUserID`，并在范围校验通过时写入 `constants.KeyTenantID` 与 `constants.KeyProjectID`；`AuthorizationFromContext` 返回 `Principal`、可选的 `TenantID` / `ProjectID` 和本次通过的 `Action`。

```go
routes.GET("/tenants/:tid/projects/:pid/documents/:id",
    tenantv1.GinAuthorization(tenantv1.AuthorizationConfig{
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

SDK 随本仓库根模块交付，没有独立 go.mod。本次 `ServiceAPIKey`、`ResolveIdentity`、身份与范围鉴权中间件改动尚未发布，旧版 v0.9.1 不包含这些接口。

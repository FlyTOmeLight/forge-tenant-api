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
| `ResolveScope` | 校验会话与精确的租户 / 项目作用域，返回用户和归属状态 |
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

server-go 项目使用子包 `github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1/servergo`，其 `Identity(tenantv1.MiddlewareConfig)` 返回 `core.HandlerFunc`，同时写入框架用户键和 request context，业务代码两种读法一致。

HTTP 失败返回 `*tenantv1.Error`，通过 `errors.As` 读取 `Status`、`Code`、`Message`、`RequestID`。401 / 403 等错误保留租户 API 的业务代码；代理返回非 JSON 错误时仍保留实际 HTTP 状态。网络错误、取消和超时保留原始错误身份，可以使用 `errors.Is(err, context.Canceled)` 或 `errors.Is(err, context.DeadlineExceeded)`。

身份须来自已验证会话或任务责任链。`CheckAccess` 的 `Allowed=false` 是一次成功查询得到的拒绝结果；接口不可达也应拒绝敏感操作。消费撤权事件时，调用方在成功处理后持久化最后一条事件 ID，SDK 不自动翻页、确认事件或停止执行器。

SDK 随本仓库根模块交付，没有独立 go.mod。本次 `ServiceAPIKey`、`ResolveIdentity` 与身份中间件改动尚未发布，旧版 v0.9.1 不包含这些接口。

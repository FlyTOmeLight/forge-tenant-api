# Forge 租户服务 Go SDK

导入路径：`github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1`。SDK 与服务端共用请求和响应类型，保持现有 JSON 字段名称。接口属于可信服务之间的调用，需要租户 API 的 `SERVICE_TOKEN`。

```go
package integration

import (
    "context"

    "github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func canWrite(ctx context.Context, endpoint, serviceToken, userID, tenantID, projectID string) (bool, error) {
    client, err := tenantv1.New(tenantv1.Config{
        Endpoint: endpoint,
        ServiceToken: serviceToken,
    })
    if err != nil {
        return false, err
    }
    defer client.Close()

    access, err := client.CheckAccess(ctx, &tenantv1.CheckAccessRequest{
        UserID: userID,
        TenantID: tenantID,
        ProjectID: projectID,
        Action: tenantv1.AccessWrite,
    })
    if err != nil {
        return false, err
    }
    return access.Allowed, nil
}
```

服务中应在依赖装配阶段创建并复用 Client，在所有调用结束后关闭。每个方法接收独立的 context 和请求对象，不保存入站会话或用户身份。默认超时 15 秒，可以通过 `Config.Timeout` 调整；不自动重试或跟随重定向。

| 方法 | 用途 |
| --- | --- |
| `CheckSession` | 校验会话；写请求同时提交原始 method、origin、csrf |
| `CheckWorkspace` | 校验租户成员身份，返回可访问的项目 |
| `ResolveProject` | 返回租户内项目的 GitLab ID 和路径 |
| `CheckAccess` | 检查 read / write / manage，并返回当前实际权限及授权版本 |
| `ListAccessEvents` | 使用 `After` 读取最多 200 条持久化授权变更事件 |

HTTP 失败返回 `*tenantv1.Error`，通过 `errors.As` 读取 `Status`、`Code`、`Message`、`RequestID`。401 / 403 等错误保留租户 API 的业务代码；代理返回非 JSON 错误时仍保留实际 HTTP 状态。网络错误、取消和超时保留原始错误身份，可以使用 `errors.Is(err, context.Canceled)` 或 `errors.Is(err, context.DeadlineExceeded)`。

身份须来自已验证会话或任务责任链。`CheckAccess` 的 `Allowed=false` 是一次成功查询得到的拒绝结果；接口不可达也应拒绝敏感操作。消费撤权事件时，调用方在成功处理后持久化最后一条事件 ID，SDK 不自动翻页、确认事件或停止执行器。

SDK 随本仓库根模块交付，没有独立 go.mod。使用 `go get github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1@v0.9.0` 安装。

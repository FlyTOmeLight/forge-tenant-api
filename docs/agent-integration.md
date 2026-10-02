# Coding Agent 集成契约

租户服务通过 HTTP 提供授权事实，不导出 Go 内部包。Coding Agent 当前只有独立设计项目，尚未接入执行器；本仓库不会假装执行过任务停止、连接关闭或 GitLab 写入拦截。

## 操作前鉴权

服务调用携带 `Authorization: Bearer <SERVICE_TOKEN>`，令牌只保存在可信服务的 Secret 中。

```http
POST /internal/v1/access/check
Authorization: Bearer <SERVICE_TOKEN>
Content-Type: application/json

{"user_id":"u_...","tenant_id":"t_...","project_id":"p_...","action":"write"}
```

```json
{"allowed":false,"level":20,"read":true,"write":false,"manage":false,"authorization_version":8,"tenant_id":"t_...","project_id":"p_..."}
```

`action` 为 read / write / manage。调用者须从自己的已验证会话或任务责任链确定 user_id，不能相信浏览器随意传入的身份。创建任务、继续任务、代码写入、MR 修改、订阅流和读取产物，都要按照所需能力调用检查。服务不可达时拒绝敏感操作，不能无限期沿用缓存权限。

## 撤权事件

```http
GET /internal/v1/access/events?after=123
Authorization: Bearer <SERVICE_TOKEN>
```

返回按 ID 递增的 `items`，每页最多 200 条；每项包含 `id, tenant_id, version, reason, created_at`。消费方持久化最后成功处理的 ID，再读取下一页；处理必须可重放。事件包括所有企业授权目标变更，消费方应重新检查受影响资源，不仅根据 reason 猜测权限。

收到事件后，Coding Agent 应先收紧自动写入资格，再请求停止相关执行、断开已经失去权限的 SSE / WebSocket 订阅、收回产物访问。具体要求延续其 PRD：60 秒未确认停止时显示“停止未确认”；启用企业或重新授权不会自动继续、恢复或重新打开任务。

读取授权与调用 GitLab 之间仍有在途窗口。执行器应集中持有写凭证并通过写入闸门发出外部请求，避免发放可绕过闸门的长期通用凭证。已经送达 GitLab 的操作不能承诺撤回，需要核验与审计其结果。

访问事件接口采用有游标的持久化拉取，不提供消费者 ACK 或推送 Webhook。已迁入的代码任务另有 gate / finish 与停止确认协议，见 [coding-tasks.md](coding-tasks.md)。其他 Agent 应自行完成在途操作、停止确认与流断开的集成验收。

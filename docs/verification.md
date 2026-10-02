# 前后端拆分验证

验证日期：2026-10-03。范围固定为用户确认的本轮开始时功能快照。

## 发布与实现

- 独立仓库：forge-tenant-api（Go / Gin）和 forge-tenant-web（Vue 3 / TypeScript / Vite）。
- 两套 Docker 构建和 Helm release；后端镜像只包含 Go 程序与 CA，前端镜像只包含静态页面及 Nginx。
- Gin 实际注册租户、成员、项目、认证与任务路由；后端 `/`、`/invite` 等页面地址返回 JSON 404，由前端提供页面。
- 最终镜像：`forge-tenant-api:0.2.0-final`、`forge-tenant-web:0.2.0-final`。
- OrbStack namespace：`forge-tenant-split`，独立数据库。浏览器入口：`http://127.0.0.1:8483`。
- 原仓库、原 namespace 与 8383 服务未覆盖；测试数据与原企业数据分开。原仓库后来新增的项目执行配置和直接创建问题任务未纳入。

## 静态与数据库验证

- `npm run build`：Vue 模板、TypeScript 类型和 Vite 生产构建通过。
- `npm run format:check`：通过。
- `npm audit --audit-level=high`：0 个漏洞。
- `go test -tags=nomsgpack -race -count=1 ./...`：真实 PostgreSQL 隔离 schema 运行，通过，未跳过数据库用例。
- `go vet -tags=nomsgpack ./...`、Go 构建：通过。
- `govulncheck v1.8.0 -tags=nomsgpack ./...`：无代码可达漏洞。修复 Gin 间接依赖 GO-2026-5676，将 quic-go 升至 0.59.1；扫描仍提示非调用路径上的依赖模块公告，未宣称所有传递依赖完全无公告。
- 前端、后端、可选执行器 Helm lint 通过；前后端 TLS Ingress 模板渲染通过。
- 最终镜像经 Helm 发布；API、Worker、PostgreSQL、Web Pod 就绪。

## 最终镜像的真实端到端检查

- 真实 OIDC 授权码、PKCE 和服务端会话登录。
- 新成员接受一次性邀请，真实创建 Casdoor 与 GitLab 绑定账号。
- 自有 UI 创建项目并授予 Developer，运营角色无隐式代码权限。
- 同一统一账号跨企业复用；未加入的企业不可读取或修改。
- Worker 停止期间本地降级立即生效；重启恢复持久化任务，升级等待当前版本确认。
- 真实继承权限导致撤权冲突；修复后重试成功，另一企业授权和账号保留。
- 企业停用撤销实际 GitLab 项目授权；启用重新核验目标权限。
- 真实服务重复请求复用结果；请求内容冲突与 CSRF 被拒绝。
- 平台创建的新账号通过自有登录页实际登录 GitLab，用户 ID 与预配置绑定一致。
- 真实控制台桌面与移动端无页面级横向溢出或脚本异常。
- Coding Agent 服务接口能读取持久化撤权事件。

## Vue 任务页面回归

- Vue 表单关联真实 GitLab MR，任务与审计持久化。
- 人工审查认可绑定当前版本，未提前完成任务。
- Vue 人工介入与后端停止确认协议一致；使用协议测试客户端，未调用模型。
- 任务页四档宽度无溢出或脚本异常，弹窗 Escape 恢复焦点。

任务 smoke 在本轮测试项目中创建独立分支和 MR；使用协议测试客户端领取任务和报告停止。没有重新运行真实模型修复、推送 Agent 代码或合入 MR。原始完整试点脚本保留在前端仓库，可单独运行。

浏览器测试结果和截图保存在前端 `.local/evidence/`，未提交登录会话、管理员密码、令牌或邀请链接。GitHub CI 负责可重复的构建、静态检查和数据库回归；真实 Casdoor/GitLab 联调需要本机外部环境。

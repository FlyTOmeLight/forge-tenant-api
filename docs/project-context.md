# Forge 租户 API 项目背景

核对日期：2026-10-18。当前源码与接口契约决定实现事实；本文件保存决策、用户纠正与未完成边界。docs/verification.md 的部署及验收结果仅代表记录当时的状态。

## 职责与设计决策

本仓库负责租户、统一账号、成员、团队、项目授权、身份登录和 GitLab 权限同步，提供 API、认证回调与 Go SDK。前端归 forge-tenant-web，研发任务归 forge-coding-api，不将任务执行或前端服务合并进本仓库。

统一账号可属于零个或多个租户，也可没有项目授权。项目只属于一个租户；成员资格不自动授予代码权限。平台管理员不绕过成员检查，租户管理员读代码仍需项目授权。个人与各团队授权取最高等级；升权等待远端确认，降权立即限制平台能力。撤权持久化与执行器确认停止是不同状态。

完整重构需按业务职责拆 internal/app、按实际表名拆 internal/table，不能只调整入口和装配。公开 service/ 负责构造与生命周期，cmd/server 负责命令、信号和退出码。API 使用 server-go app.Run / Build；Worker、migrate、bootstrap 按自身依赖装配。Worker 退出后才释放数据库；NewAPI / NewWorker 借用调用方数据库，拥有各自 HTTP 连接。

连接由 server-go/db.Module 管理，查询和事务使用 GORM；业务写入、幂等、审计和 outbox 保持同一事务，共用机制归 internal/tenantwrite。Controller、业务 Service、表查询与外部客户端各有职责。GitLab、Casdoor 优先使用官方 SDK，配置归顶层 [Gitlab] / [Casdoor]。现有 Casdoor 实例传输适配负责请求取消、私有 CA 和脱敏，弥补 SDK 缺少请求级 context 与独立 HTTP 客户端的边界。服务调用契约见 sdk/go/tenantv1。

2026-10-18 配置与身份改造：配置按用途拆为 `[DB]`（server-go `db.Config`：驱动、DSN、连接池）和 `[Tenant]`（`PublicURL`、出站 `CAFile`、幂等响应加密密钥 `EncryptionKey`）；原 `[Forge]` 整节、`ModelURL` 与全局 `ServiceToken` 删除。加密密钥环境变量由 `APP_KEY` 改为 `TENANT_ENCRYPTION_KEY`，部署时只改变量名、值必须沿用原值，否则历史幂等响应无法解密。`BootstrapUser` 移出配置，只由 bootstrap 命令读 `BOOTSTRAP_USERNAME`。服务间调用改为每个服务一把数据库凭据（`credentials` 表，Bearer `ftk_...`，只存 SHA-256 摘要，可单独轮换撤销），`/internal/v1/session/check` 由 `/internal/v1/identity/resolve` 取代；公开业务路由同时接受 `forge_session` Cookie 和用户 API key，凭据管理仅限 Cookie 会话。本轮不做 AK/SK 签名，凭据不绑定租户或项目，权限仍每次实时检查。本轮只验收本仓库，其他仓库的接入另行处理。

## 最终用户纠正尚未落实

最后一次架构会话要求干净 MVP，明确拒绝历史团队升级和兼容迁移。此前保留历史结构及数据升级的方案已被该要求替代。

本次核对当前未提交工作区仍有 SchemaVersion / schema_versions、migrateTeamRoles、旧单团队及直接授权升级代码；README 和验证记录仍称其完成。不能将这部分历史迁移称为已接受的 MVP 成果。下一次架构重构应先确定目标 MVP schema，再删除不需要的历史路径与相应旧验收。目录迁移不包含该业务重构。

大量装配、业务、表模型和测试改动尚未提交，应保留；Git HEAD 不代表完整工作区实现。

## 验证边界

2026-10-14 的装配、业务拆分和 GORM 改动历史记录了真实 PostgreSQL 全仓 race、静态检查、构建及部分进程启停通过，但未提交或部署，未重新完成真实 Casdoor / GitLab 浏览器联调。通过技术检查不代表满足最终 MVP 范围。

授权、成员生命周期和同步变更需运行真实 PostgreSQL 集成测试；缺少 TEST_DATABASE_URL 而跳过不算通过。浏览器验收归 forge-tenant-web/tests/e2e.cjs。

2026-10-13 历史记录：v0.9.1 已发布且外部 SDK / 配置验收通过，运行环境未升级；v0.8.1 曾部署本机 Kubernetes。实时版本需重新核实。本机 Kubernetes 联调 namespace 承载后端 release forge-tenant 与前端 forge-tenant-web，Casdoor / GitLab 位于身份服务 namespace；不将其称为线上生产。私有配置及证据保留在忽略的 .local/，输出需脱敏。

## 对话来源

- 01a134ee-a507-7d30-989d-5f6662a3a49d：租户关系、完整框架重构、SDK、GORM、官方 SDK 与顶层配置要求。
- 01a13711-02fa-7b23-8910-920332c60f5e：server-go 布局、按业务及表名拆分、最终 MVP 纠正。

迁移时按旧 cwd 查到 23 条记录：2 条用户主会话、10 条工作子代理、11 条审批记录；原始会话保持原样。

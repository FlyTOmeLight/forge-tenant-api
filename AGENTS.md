# Forge 租户 API

- 目录约定和交付契约见 README、docs/api.md 及实际 handler。前端在独立 forge-tenant-web 仓库。
- 授权、成员生命周期或同步变更需验证 internal/platform 中的真实 PostgreSQL 测试；未提供 TEST_DATABASE_URL 的跳过不算验证通过。浏览器验收在前端仓库 tests/e2e.cjs。
- Agent 权限与执行停止分别遵守 docs/agent-integration.md、docs/coding-tasks.md；不要把撤权已提交等同于执行器停止已确认。
- 本机应用部署在 forge-tenant-split：后端 Helm release 为 forge-tenant，前端为 forge-tenant-web。Casdoor 与 GitLab 位于 tenant-lab。
- .local/ 保存私有配置，保持 Git 忽略。提交和输出的验收结果应脱敏。

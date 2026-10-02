# Forge 租户 API

独立 Go / Gin 后端，管理企业、统一账号、成员、项目授权和同步队列，并对接 Casdoor 与 GitLab CE。配套 Vue 控制台在 [forge-tenant-web](https://github.com/FlyTOmeLight/forge-tenant-web)。后端只提供 API 和认证回调，不编译或服务前端页面。

## 目录与 HTTP 层

按 [Go 官方服务端模块布局](https://go.dev/doc/modules/layout#server-project) 组织：入口位于 `cmd/`，私有实现位于 `internal/`。Gin 负责路由、方法匹配和恢复中间件。Gin 官方未规定必须使用 controller/service/repository 分层，本项目保留围绕租户事务与授权的内聚实现。

```text
cmd/forge/          api / worker / migrate / bootstrap 程序入口
internal/config/    环境变量解析
internal/platform/  Gin 路由、认证、租户事务、同步和代码任务
internal/remote/    Casdoor / GitLab 协议适配
configs/            环境配置样例
charts/forge-tenant/ API、Worker、PostgreSQL、可选接口 Ingress
scripts/            联调准备、Helm 发布和试点执行器
docs/               API 与 Agent 集成契约
```

`internal/platform/router.go` 把 Gin 路由参数写入标准 `http.Request`，授权与业务 handler 保留标准 HTTP 接口；没有另挂 ServeMux。所有租户、项目和任务路径由 Gin 匹配。

## 构建与测试

```sh
go build -tags=nomsgpack -o bin/forge ./cmd/forge
GIN_MODE=release go test -tags=nomsgpack ./...
go vet -tags=nomsgpack ./...
TEST_DATABASE_URL='postgres://user:password@host/database?sslmode=disable' GIN_MODE=release go test -tags=nomsgpack -race -count=1 ./...
helm lint charts/forge-tenant
```

Go 版本以 `go.mod` 为准。数据库测试在真实 PostgreSQL 中创建隔离 schema 并清理；没有 `TEST_DATABASE_URL` 时跳过不算通过。`nomsgpack` 去除未使用的 MessagePack 编解码，JSON 使用 Go 标准库；无需 CGO。Gin v1.12.0 为官方 MIT 依赖，与本项目工具链兼容；引入目的为用户要求的 Gin HTTP 路由。依赖漏洞检查见验证记录。

## Kubernetes / Helm

配置示例：`configs/env.example`。API 与 Worker 使用同一镜像的不同命令，独立扩缩容。现有数据库与 Casdoor / GitLab 实例沿用，账号和权限数据无需迁移到新数据库。

本机已有联调配置时，保留其 `.local/deployment.json` 和集群 `forge-tenant-env`、`forge-tenant-ca` Secret；**不要重新生成密钥或数据库密码**。首次建立本机联调可执行：

```sh
python3 scripts/prepare-lab.py --lab-config /path/to/infra.json --kubectl /path/to/kubectl
FORGE_IMAGE_TAG=0.2.0 ./scripts/deploy-lab.sh
```

后端 Helm release 仍为 `forge-tenant`，namespace 同名，以保持数据库 PVC、Service 和执行器调用地址。前端独立 release `forge-tenant-web`。浏览器入口通过前端 Service 提供：

```sh
kubectl --context orbstack -n forge-tenant port-forward service/forge-tenant-web 8383:80
```

后端单独调试可把 `service/forge-tenant` 转发到 8484。`PUBLIC_URL` 始终指向浏览器看到的前端 origin（本机为 `http://127.0.0.1:8383`），OIDC 回调为该 origin 的 `/auth/callback`，认证跳转页面由 Vue 提供。正式环境应使用 TLS Ingress 和可被集群拉取的版本化镜像；内置单副本 PostgreSQL 用于本机验证，外部数据库可设置 `postgres.enabled=false`。

## 已有业务契约

- 自有前端登录；OIDC code + PKCE + nonce，服务端会话、CSRF。
- 企业、成员邀请与激活、跨企业统一身份、管理员保护、团队。
- 私有 GitLab Group 与项目、项目级 Reporter / Developer / Maintainer。
- 业务事务、审计、幂等与 outbox 一致提交；Worker 租约、有限重试和周期对账。
- 降权先限制平台能力；升权等待当前版本远端确认；继承权限冲突可诊断和重试。
- Coding Agent 鉴权、持久化撤权事件，以及从原仓库迁入的代码任务 API 与试点执行器代码。

HTTP 契约见 [docs/api.md](docs/api.md)，访问控制接入见 [docs/agent-integration.md](docs/agent-integration.md)，任务状态与执行器见 [docs/coding-tasks.md](docs/coding-tasks.md)。任务执行器仍受原有试点项目和文件范围限制，本次拆分不扩大执行范围。

邮件、MFA、密码找回、计费和通用多项目执行器不在本轮范围。当前列表有数量上限（企业/账号 200，成员/团队/项目 500，授权 1000，操作/审计 100），尚未分页。

## 验证与仓库迁移

浏览器 E2E 在前端仓库中，支持 `FORGE_API_ROOT` 指向本仓库。`.local/`、凭证、会话与测试产物不推送。原 `forge-tenant-platform` 保留为迁移源，新开发分别在两个独立仓库继续。

本次实际执行的检查与限制见 [docs/verification.md](docs/verification.md)。

## 本次独立验证环境

为保留另一会话对原平台的并行修改，本轮使用 namespace `forge-tenant-split`、独立 PostgreSQL 和 OIDC client `forge-tenant-split-console`，浏览器入口为 `http://127.0.0.1:8483`。该环境中的企业和项目是验收数据；原 `forge-tenant` / 8383 及其数据未被切换或覆盖。

继续更新此验证环境时使用 `FORGE_NAMESPACE=forge-tenant-split FORGE_IMAGE_TAG=0.2.0-final ./scripts/deploy-lab.sh`。前端 E2E 同时设置 `FORGE_NAMESPACE=forge-tenant-split`。本机端口转发命令：

```sh
kubectl --context orbstack -n forge-tenant-split port-forward service/forge-tenant-web 8483:80
```

本轮迁移以开始时的功能快照为准，包括当时的 MR 关联任务；开发过程中新增的项目执行配置、直接创建问题任务等留在原仓库，后续单独迁入。

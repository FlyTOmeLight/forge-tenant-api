# Forge 租户 API

基于 server-go v0.4.0 的独立 Go 后端，管理企业、统一账号、成员、项目授权和同步队列，并对接 Casdoor 与 GitLab CE。配套 Vue 控制台在 [forge-tenant-web](https://github.com/FlyTOmeLight/forge-tenant-web)。后端只提供 API 和认证回调，不编译或服务前端页面。

## 目录与 HTTP 层

按 [Go 官方服务端模块布局](https://go.dev/doc/modules/layout#server-project) 与 server-go 的职责划分组织。依赖从入口显式构造，事务和权限判断由租户业务层负责。

```text
cmd/server/               进程入口与 api / worker / migrate / bootstrap 命令
internal/service/         HTTPHost 服务、路由装配、资源与 Worker 生命周期
internal/app/account/     账号、登录、会话；Controller / Service / router
internal/app/workspace/   租户工作区、成员、团队和项目写入；Controller / Service / router
internal/app/authorization/ 项目权限、角色、权限说明和内部接口；Controller / Service / router
internal/app/synchronization/ 持久化 GitLab 同步与核验
internal/app/httpapi/     Cookie 会话中间件、绑定和既有错误协议映射
internal/app/apperror/    领域错误
internal/identity/       统一账号身份和令牌
internal/table/          GORM 持久化记录、查询与 SQL 迁移
internal/client/         Casdoor、GitLab、OIDC 与 HTTP 连接
internal/config/         业务配置
sdk/go/tenantv1/         服务调用契约与客户端
conf/server/            server-go 配置与环境变量占位符
```

`internal/service.API` 实现 `host.Service`，统一安装会话与服务 Bearer 中间件并注册各领域 router。Controller 直接接收 Gin context、绑定类型化请求并调用业务 Service；移除 net/http handler 桥接，router 只注册 Controller 方法。账号、工作区、授权与同步各有独立 Service 和依赖；工作区的公开写方法负责授权、事务、持久化幂等与审计 / outbox，控制器不编排事务。授权 Service 复用 SDK 权限响应类型，工作区调用授权 Service 计算项目能力，同步模块通过版本核验更新已观测授权。`Resources` 负责资源构造、初始化失败清理和关闭，`Worker` 负责同步协程的取消与等待。

数据库连接由 `server-go/db.Module` 管理，Table 使用 GORM 的 `WithContext`、`Raw` / `Exec` 执行查询与写入，业务层使用 GORM 事务。`DATABASE_URL` 原样传给框架 DSN，连接池上限为 12；同步 Worker 使用 GORM `Connection` 固定会话锁连接。表结构与版本化 SQL 迁移保留，不使用 AutoMigrate。

server-go 管理配置快照、HTTPHost 启动与排空、请求 ID、日志、Recovery、严格 JSON 绑定和服务 Bearer 鉴权。Controller 在 HTTP 边界将领域及框架错误映射为既有 `{error:{code,message}}` 格式。租户幂等结果仍与业务、审计和 outbox 一起写入 PostgreSQL。

默认加载 `conf/server/config.toml`，可用 `SERVER_CONFIG_PATH` 指定配置文件。`[Forge]` 保留既有环境变量名称，`[HTTPServer]` 配置监听地址与超时，`[Log]` 配置日志。配置在启动时读取；调整后重启对应进程。API 收到退出信号后由 HTTPHost 排空请求，再由 Bootstrap 关闭数据库和外部 HTTP 空闲连接；Worker 取消并完成当前操作的结果持久化后释放资源。

`/healthz` 返回 HTTPHost 的进程就绪状态；`/readyz` 额外检查 PostgreSQL，Helm readinessProbe 使用后者。`/ping` 和 `/version` 由框架提供。

## 构建与测试

```sh
go build -tags=nomsgpack -o bin/forge ./cmd/server
GIN_MODE=release go test -tags=nomsgpack ./...
go vet -tags=nomsgpack ./...
TEST_DATABASE_URL='postgres://user:password@host/database?sslmode=disable' GIN_MODE=release go test -tags=nomsgpack -race -count=1 ./...
helm lint charts/forge-tenant
```

Go 版本以 `go.mod` 为准。数据库测试在真实 PostgreSQL 中创建隔离 schema 并清理；没有 `TEST_DATABASE_URL` 时跳过不算通过。`nomsgpack` 去除未使用的 MessagePack 编解码，JSON 使用 Go 标准库；无需 CGO。Gin v1.12.0 为官方 MIT 依赖，与本项目工具链兼容；引入目的为用户要求的 Gin HTTP 路由。依赖漏洞检查见验证记录。

## 服务调用 SDK

可信 Go 服务使用 [`sdk/go/tenantv1`](sdk/go/tenantv1/README.md) 调用会话校验、租户工作区、项目解析、项目权限和撤权事件接口。SDK 复用 server-go 的连接、超时和错误解码，提供有类型的请求 / 响应；服务端复用同一契约。调用方在装配阶段创建 Client，运行期间复用，退出时关闭。SDK 随根模块 v0.9.0 发布，使用 `go get github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1@v0.9.0` 安装。

## Kubernetes / Helm

配置示例：`configs/env.example`。API 与 Worker 使用同一镜像的不同命令，独立扩缩容。现有数据库与 Casdoor / GitLab 实例沿用，账号和权限数据无需迁移到新数据库。

本机已有联调配置时，保留其 `.local/deployment.json` 和集群 `forge-tenant-env`、`forge-tenant-ca` Secret；**不要重新生成密钥或数据库密码**。首次建立本机联调可执行：

```sh
python3 scripts/prepare-lab.py --lab-config /path/to/infra.json --kubectl /path/to/kubectl
FORGE_IMAGE_TAG=0.9.0 ./scripts/deploy-lab.sh
```

本机部署统一使用 namespace `forge-tenant-split`。后端 Helm release 为 `forge-tenant`，前端独立 release 为 `forge-tenant-web`。浏览器入口通过前端 Service 提供：

```sh
kubectl --context orbstack -n forge-tenant-split port-forward service/forge-tenant-web 8483:80
```

后端单独调试可把 `service/forge-tenant` 转发到 8484。`PUBLIC_URL` 始终指向浏览器看到的前端 origin（本机部署为 `http://127.0.0.1:8483`），OIDC 回调为该 origin 的 `/auth/callback`，认证跳转页面由 Vue 提供。正式环境应使用 TLS Ingress 和可被集群拉取的版本化镜像；内置单副本 PostgreSQL 用于本机验证，外部数据库可设置 `postgres.enabled=false`。

## 已有业务契约

- 自有前端登录；OIDC code + PKCE + nonce，服务端会话、CSRF。
- 企业、成员邀请与激活、跨企业统一身份、管理员保护、团队。
- 私有 GitLab Group 与项目、项目级 Reporter / Developer / Maintainer。
- 业务事务、审计、幂等与 outbox 一致提交；Worker 租约、有限重试和周期对账。
- 降权先限制平台能力；升权等待当前版本远端确认；继承权限冲突可诊断和重试。
- Coding Agent 鉴权、会话校验、项目解析和持久化撤权事件。研发任务已迁到独立 forge-coding-api。

HTTP 契约见 [docs/api.md](docs/api.md)，访问控制接入见 [docs/agent-integration.md](docs/agent-integration.md)，研发任务见 [forge-coding-api](https://github.com/FlyTOmeLight/forge-coding-api)。

邮件、MFA、密码找回、计费和通用多项目执行器不在本轮范围。当前列表有数量上限（企业/账号 200，成员/团队/项目 500，授权 1000，操作/审计 100），尚未分页。

## 验证与仓库迁移

浏览器 E2E 在前端仓库中，支持 `FORGE_API_ROOT` 指向本仓库。`.local/`、凭证、会话与测试产物不推送。租户、研发和 Agent 执行分别由独立仓库维护。

本次实际执行的检查与限制见 [docs/verification.md](docs/verification.md)。

## 当前本机环境

当前使用 namespace `forge-tenant-split`、独立 PostgreSQL 和 OIDC client `forge-tenant-split-console`，浏览器入口为 `http://127.0.0.1:8483`。旧版一体化平台、旧 Python 执行器和 `forge-tenant` 命名空间已退役。历史数据库与源码保存在本机加密归档中。

部署脚本默认使用当前命名空间。更新时用 `FORGE_IMAGE_TAG=<版本> ./scripts/deploy-lab.sh`。前端 E2E 默认使用同一命名空间。本机端口转发命令：

```sh
kubectl --context orbstack -n forge-tenant-split port-forward service/forge-tenant-web 8483:80
```

后续功能已迁入：项目执行配置与接入检查、直接新建问题任务、多审查者、每轮配置快照、Agent 判断、代码差异，以及统一导航下的三种任务布局。研发业务现已迁到独立 forge-coding-api，真实模型交付结果见该仓库的验证记录。

## 本机 Forge → GitLab 单点登录

当前工作台使用 `http://127.0.0.1:8483`，GitLab 使用 `http://127.0.0.1:18080`。首次登录 Forge 后，打开项目或 MR 会自动完成 GitLab 登录并返回原页面。升级前的 Forge 会话没有身份 Cookie，需要退出 Forge 后重新登录一次。

当前配置由 `scripts/configure-lab-sso.py` 维护。脚本使用已有 Secret 中的凭证，在内存中更新既有 Casdoor/GitLab 客户端，保留账号、外部身份与项目权限。需要 Python 3、Ruby 标准 YAML 库、kubectl、Helm 和已安装版本的 GitLab Chart，不需要导出凭证：

```sh
# 保持转发运行，再在另一个终端运行配置脚本。
kubectl --context orbstack -n tenant-lab port-forward svc/casdoor 18500:8000 --address 127.0.0.1
python3 scripts/configure-lab-sso.py --gitlab-chart /path/to/gitlab-10.4.1.tgz
```

脚本默认更新 `forge-tenant-split`，可通过 `--namespace` 指定。GitLab 和 Forge 的浏览器入口必须保持可达。本机 GitLab 的 `18080` 转发已由用户级 launchd 服务 `dev.light.forge-gitlab-portforward` 托管，登录后启动，进程退出后自动重启。配置位于 `~/Library/LaunchAgents/dev.light.forge-gitlab-portforward.plist`，仅监听 `127.0.0.1`。可用 `launchctl print gui/$(id -u)/dev.light.forge-gitlab-portforward` 检查状态；不要同时启动另一条 GitLab 转发。工作台的 `8483` 转发同样由用户级 launchd 服务 `dev.light.forge-web-portforward` 托管，配置位于 `~/Library/LaunchAgents/dev.light.forge-web-portforward.plist`，在 Pod 更新导致断开后自动重连。配置后运行前端仓库的 `npm run test:sso`，使用已有可访问 MR 验证完整跳转，不创建或修改 MR。完整租户与权限回归使用 `npm run test:e2e`。

GitLab 实例管理员的本地账号入口为 `/users/sign_in?auto_sign_in=false`。配置依据：[GitLab OmniAuth](https://docs.gitlab.com/integration/omniauth/) 与 [OpenID Connect](https://docs.gitlab.com/administration/auth/oidc/)。

## GitLab 凭据热加载

Helm 将应用 Secret 的 `GITLAB_TOKEN` 挂载为 `/var/run/forge-gitlab/token`，并设置 `GITLAB_TOKEN_FILE`。API 与 Worker 每次请求读取当前文件，Secret 更新后无需重启。文件配置后若缺失或为空，请求报错，不使用旧环境变量。环境变量方式继续用于本地运行。

共享服务令牌的定时续期由 `forge-coding-api` Chart 的 `gitlabCredentials` CronJob 负责。本服务的 API 与 Worker 应列入其 `otherTargets`，所属命名空间应列入 `otherNamespaces`。`/forge gitlab-credential-status` 使用相同客户端读取挂载文件，仅输出令牌元数据。

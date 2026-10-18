# Forge 租户 API

基于 server-go v0.9.0 的独立 Go 后端，管理企业、统一账号、成员、项目授权和同步队列，并对接 Casdoor 与 GitLab CE。配套 Vue 控制台在 [forge-tenant-web](https://github.com/FlyTOmeLight/forge-tenant-web)。后端只提供 API 和认证回调，不编译或服务前端页面。

## 目录与 HTTP 层

按 [Go 官方服务端模块布局](https://go.dev/doc/modules/layout#server-project) 与 [server-go v0.9.0 项目组织约定](https://github.com/FlyTOmeLight/server-go/blob/v0.9.0/docs/go-project-layout.md) 的职责划分组织。依赖从入口显式构造，事务和权限判断由租户业务层负责。

```text
cmd/server/               进程入口与 api / worker / migrate / bootstrap 命令
service/                  公开构造、独立应用装配、HTTP 与 Worker 生命周期
internal/app/authentication/ 登录、OIDC、会话与当前登录主体
internal/app/credential/     用户与服务 API key 签发、列表和撤销
internal/app/user/           平台账号列表与初始运营账号
internal/app/tenant/         租户创建、列表、启停
internal/app/member/         成员邀请、角色调整、移除和重新邀请
internal/app/team/           团队与团队成员
internal/app/project/        项目接入、项目人员、直接及团队授权、项目解析
internal/app/invitation/     邀请签发、查看、接受与身份激活
internal/app/authorization/  角色、成员权限说明、有效权限判断与撤权事件
internal/app/workspace/      工作区视图与工作区访问校验
internal/app/synchronization/ GitLab 同步、核验与人工重试
internal/tenantwrite/        租户写事务、幂等结果、审计和同步 outbox
internal/httpapi/            会话、用户及服务凭据中间件、绑定和错误协议映射
internal/apperror/           业务错误类型
internal/identity/       统一账号身份和令牌
internal/table/          GORM 表模型、查询与 Go 数据迁移
internal/client/         Casdoor、GitLab、OIDC 与 HTTP 连接
internal/config/         业务配置
sdk/go/tenantv1/         服务调用契约与客户端
conf/server/            server-go 配置与环境变量占位符
```

`service.API` 实现 `host.Service`，统一安装 Cookie 会话、统一身份（Cookie 或用户 Bearer API key）和服务 API key 中间件并注册各业务 Controller。每个 app 包把自身的 Controller、请求类型和业务方法放在一起；Controller 使用框架 `core.Context` 绑定参数、输出响应，Service 负责业务校验和写入。`workspace` 只聚合工作区读取，不再持有成员、团队、项目和邀请写入。项目解析及授权列表归 `project`，工作区访问校验归 `workspace`，登录会话与平台账号管理分别归 `authentication` 和 `user`。

各写入业务共用 `tenantwrite.Writer`，在同一租户锁与数据库事务中完成当前授权、幂等回放、版本校验、业务写入、审计、撤权事件和同步 outbox。各业务只定义自己的变更；邀请接受保留自身的身份创建事务。`service.Build` 通过 `app.Runtime` 初始化独立数据库并交付 API，由 `app.Run` 清理；公开 `NewAPI` / `NewWorker` 借用调用方数据库并拥有各自的 HTTP 连接。`Worker` 负责同步协程的取消与等待。

`internal/table` 按实际表名拆文件：`users`、`tenants`、`memberships`、`teams`、`team_memberships`、`projects`、`grants`、`direct_grants`、`team_grants`、`credentials`、`operations`、`audit_events`、`access_events`、`invitations`、`sessions`、`login_flows`、`idempotency`、`schema_versions`。模型与查询放在所属表文件中，联表查询按返回主体归属。全库迁移、PostgreSQL 锁、JSONB 查询结果转换分别由 `database.go`、`locks.go`、`query.go` 负责。

数据库连接由 `server-go/db.Module` 管理，驱动、DSN 和连接池来自 `[DB]`；Table 使用 GORM 的 `WithContext`、`Raw` / `Exec` 执行查询与写入，业务层使用 GORM 事务。同步 Worker 使用 GORM `Connection` 固定会话锁连接。表结构由各表完整 GORM 模型定义，`Migrate` 在事务锁内执行 `AutoMigrate`，字段类型、NULL、默认值、唯一约束、复合外键及检查约束均由模型标签声明。历史团队和直接授权数据用 Go 迁移一次，`schema_versions` 记录完成状态；重复迁移不会恢复已撤销的授权。

server-go 管理配置快照、HTTPHost 启动与排空、请求 ID、日志、Recovery、严格 JSON 绑定和服务 API key 鉴权。Controller 在 HTTP 边界将领域及框架错误映射为既有 `{error:{code,message}}` 格式。租户幂等结果仍与业务、审计和 outbox 一起写入 PostgreSQL。

默认加载 `conf/server/config.toml`，可用 `SERVER_CONFIG_PATH` 指定配置文件。`[DB]` 配置数据库驱动、DSN 和连接池，`[Tenant]` 配置本服务的 `PublicURL`、出站 `CAFile` 和幂等响应加密密钥 `EncryptionKey`（环境变量 `TENANT_ENCRYPTION_KEY`，即原 `APP_KEY`，改名时须沿用原值）；顶层 `[Gitlab]` 集中配置 GitLab API / 浏览器地址、OIDC 客户端和管理凭据，`[Casdoor]` 集中配置身份服务、管理客户端、组织和 Forge OIDC 客户端。bootstrap 命令另读环境变量 `BOOTSTRAP_USERNAME`，不作为常驻服务配置。`[HTTPServer]` 配置监听地址与超时，`[Log]` 配置日志。GitLab API 调用使用官方 `client-go/v3`；Casdoor 管理调用复用官方 `casdoor-go-sdk` 的客户端 URL、用户和响应类型。Casdoor SDK v1.56.0 的用户方法不接收 context，HTTP 客户端为包级全局，因此传输保留实例适配，以支持请求取消及私有 CA。业务配置和日志在启动时读取，调整后重启对应进程；API 的框架配置由 `app.Run` 监听，HTTPHost 支持动态更新的项目按框架规则生效。API 收到退出信号后由 `app.Run` 协调 HTTPHost 排空请求，再停止服务、关闭外部 HTTP 空闲连接和数据库；Worker 取消并完成当前操作的结果持久化后释放资源。

独立入口使用 `service.Run(ctx, mode)`，API 通过 `app.Run` 统一装配。框架 v0.9.0 不自动读取 `[Log]`，入口装配显式读取并传入 `Options.LogConfig`。Worker 只创建数据库和 GitLab 客户端；migrate 只连接数据库；bootstrap 只创建数据库和 Casdoor 客户端。现有 `/forge api|worker|migrate|bootstrap|gitlab-credential-status` 部署命令保持不变；服务 API key 由 `forge service-credential issue|list|revoke` 维护。

组合应用导入公开 `service` 包，用 `service.Config{DB: db.Config{...}, Tenant: service.TenantConfig{...}, Gitlab: service.GitlabConfig{...}, Casdoor: service.CasdoorConfig{...}}`（`DB` 为 `server-go/db.Config`）构造配置，调用 `service.NewAPI(ctx, sharedDB, cfg)` 并交给宿主生命周期。配置沿用各 TOML 节的校验规则；`NewAPI` 初始化 OIDC，不执行数据库迁移或平台账号 bootstrap。宿主通过 `service.SetupHTTP` 安装 Forge 响应协议，并负责迁移及共享数据库关闭；不要调用独立 `Build` 重复创建数据库。`NewWorker` 的运行 context 由 `Start` 传入，`Stop` 完成后再关闭借用的数据库。

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

可信 Go 服务使用 [`sdk/go/tenantv1`](sdk/go/tenantv1/README.md) 调用身份解析、租户工作区、作用域解析、项目解析、项目权限和撤权事件接口；SDK 还提供把入站 Cookie 或用户 API key 解析成用户身份的 `net/http` 中间件与 server-go 适配。SDK 复用 server-go 的连接、超时和错误解码，提供有类型的请求 / 响应；服务端复用同一契约。调用方在装配阶段创建 Client，运行期间复用，退出时关闭。SDK 随根模块 v0.11.0 发布，使用 `go get github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1@v0.11.0` 安装。

## Kubernetes / Helm

配置示例：`configs/env.example`。API 与 Worker 使用同一镜像的不同命令，独立扩缩容。现有数据库与 Casdoor / GitLab 实例沿用，账号和权限数据无需迁移到新数据库。

本机已有联调配置时，保留其 `.local/deployment.json` 和集群 `forge-tenant-env`、`forge-tenant-ca` Secret；**不要重新生成密钥或数据库密码**。首次建立本机联调可执行：

```sh
python3 scripts/prepare-lab.py --lab-config /path/to/infra.json --kubectl /path/to/kubectl
FORGE_IMAGE_TAG=0.11.0 ./scripts/deploy-lab.sh
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

# 持久化修复任务与本地执行器

任务页面：`http://127.0.0.1:8383/tasks`。复用 Forge 的真实 OIDC 登录、服务端会话、CSRF 和项目授权。任务、轮次、审查认可、正式反馈与操作事件保存在 PostgreSQL。

本次交付接入已有开放 MR。关联任务默认人工处理；只有负责人或项目管理员显式恢复后，外部执行器才领取一轮。项目读取 / 写入 / 管理权限分别要求有效 GitLab 等级 20 / 30 / 40；平台运营身份不会绕过代码授权。首位指定审查者为关联任务的创建人。

## 控制与审查

- 人工介入立即递增执行 epoch，关闭执行 gate。外部执行器终止整个子进程组，并在确认退出后回传 `Stopped`。停止未确认期间不可恢复；租约超时只进入等待停止，不伪造停止成功。
- 转交任务只改变负责人，保留控制权、正在执行的轮次、分支和 MR。新负责人必须拥有有效代码写权限。
- 任务版本冲突返回 412；代码版本冲突返回 409。恢复、反馈与人工认可均绑定当前 commit。代码变化后自动检查与人工认可失效。
- GitLab 行级讨论保留文件、行号、commit 与 discussion ID。同步评论不执行修改。审查者将冲突意见整合后正式提交；执行中或人工处理期间的要求入队。队列绑定的代码版本变化后，审查者需重新核对，负责人才能开始下一条要求。
- 同一任务最多一个排队、运行或等待停止的轮次。有效权限撤回后旧执行失效，重新授权不恢复旧执行。
- 自动检查与人工认可独立记录。认可不会完成任务；同步到 MR 合入才完成。取消后发生的合入不会将已取消任务改为完成。

## 接口

公共接口前缀：`/api/v1/tenants/{tid}/projects/{pid}/tasks`。

| 方法 / 路径 | 行为 |
| --- | --- |
| GET / POST 前缀 | 查询任务 / 关联既有 MR |
| GET `/{task}` | 任务、轮次、反馈、事件与当前操作权限 |
| POST `/{task}/sync` | 同步 MR 当前事实与分页行级讨论 |
| POST `/{task}/actions` | `Intervene`、`Resume`、`Transfer`、`Cancel`、`Continue`、`Approve`、`Feedback`、`ReconfirmFeedback`、`NextFeedback` |

执行器接口使用服务身份：`POST /internal/v1/tasks/claim`、`POST /internal/v1/tasks/{task}/gate`、`POST /internal/v1/tasks/{task}/finish`。领取支持按 GitLab project ID 限定。gate 验证执行 epoch、worker、租约和当前有效项目写权限。服务 token 不作为公共页面凭据。

## 当前试点

GitLab 私有项目 `coding-agent-pilot/agentic-kit-pilot`（ID 1），沿用 MR !1。原始 `lab-developer/agentic-kit-pilot` 已转移到 Forge 管理的试点企业组，MR、讨论和代码历史保留。原 `/Users/light/Code/agentic-kit` 不参与自动写入。

外部执行器：`scripts/task-executor/executor.py`；部署：独立 Helm Chart `charts/forge-task-executor`。镜像使用 uv 建立独立 Python 环境并安装仓库要求的 pre-commit。容器通过 `host.docker.internal:11434` 调用用户本地 Ollama `qwen3.8:27b-mlx`。任务工作区使用独立 PVC。

执行器当前只处理该项目的 `internal/pkg/storagepath/storagepath.go` 与 `storagepath_test.go`，每轮最多 3 次修复、30 分钟实际执行。依次执行 gofmt、相关包测试、vet、diff 检查和原有提交 hook；推送使用精确旧 head 条件，拒绝替换人工新提交。保留模型返回、检查与补丁证据，不自动合入。

这仍是人工注入案例，真实 bug 样本计数为 0。该单项目容器与 gate 不是面向不可信仓库的完整租户沙箱。完整 PRD 尚需实现从新问题创建分支/MR、通用项目执行策略、多审查者设置、澄清暂停/重入、执行配置快照、60 秒停止未确认的主动通知，以及实际人工提交/合入的完整验收。当前 MR 更新依赖显式同步；页面定时读取数据库状态。

## 验证与证据

`go test -race ./... -count=1` 使用 `TEST_DATABASE_URL` 连接真实 PostgreSQL 的隔离测试 schema；未配置时任务测试不作为数据库验收证据。`go vet ./...` 验证 Go 代码。任务测试覆盖停止与迟到结果、队列串行、转交、取消后合入、权限撤回再授予、过期租约、过期代码版本及执行器项目范围。

前端仓库中的 `npm run test:tasks` 在配置好的已部署服务上使用真实登录，读取私有 `.local/deployment.json` 与不含凭据的 `.local/task-pilot.json`。它会实际启动 / 中断 / 恢复试点任务并更新 MR，故不是只读测试。结果与桌面、移动端截图位于 `.local/task-evidence/`。凭据与会话不应提交到仓库。

## GitLab 环境修复

本地 GitLab 原先只有 `registry.enabled=false`，应用仍尝试访问未部署的 registry。以相同 Chart 10.4.1 执行 `helm upgrade --reuse-values --set global.registry.enabled=false` 后，项目转移成功。两个开关分别控制部署与应用集成，见 [GitLab Globals 文档](https://docs.gitlab.com/charts/charts/globals/)。转移使用 [Projects API](https://docs.gitlab.com/api/projects/#transfer-a-project-to-a-new-namespace)。


## 拆分后的执行器部署

执行器作为可选独立 Helm release，保留原试点执行范围。先准备已有 Secret 引用，再执行 `helm upgrade --install forge-task-executor charts/forge-task-executor -n forge-tenant`；镜像、模型地址和试点项目由 values 配置。原集群中由其他会话运行的执行器未在本次拆分中接管或替换。

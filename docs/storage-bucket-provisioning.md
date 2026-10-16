# 租户存储空间开通

新建租户时，租户、首位管理员和 `tenant_bucket_provisions` 开通义务在同一数据库事务中提交。此任务以 `tenant_id` 唯一，独立于 GitLab `operations`；GitLab 失败、版本 supersede 或成功回执都不会消费此义务。存储不可用不回滚已经创建的租户。

## 配置和执行

`server worker` 同时运行 GitLab 同步与独立的 bucket worker；两者共用数据库生命周期，退出时全部停止后才释放连接。[Storage] 未启用时，仍持久登记新租户义务，但不调用 storage，不表示空间已经开通。

```toml
[Storage]
URL = "https://storage.example.test"
ManagementToken = "<独立的 storage tenant-service 管理凭据>"
Timeout = "15s"
```

部署模板提供 `STORAGE_URL` 和 `STORAGE_MANAGEMENT_TOKEN`，默认禁用。管理凭据必须至少 32 字符、无空白，并且不同于 Forge `ServiceToken`。URL 为 HTTPS origin（仅本机测试允许 loopback HTTP），不得携带用户名、密码、查询或路径。可通过现有 Forge.CAFile 配置私有 CA；不跳过证书校验。请求超时可设置为 1s–1m，默认 15s。管理 HTTP 不跟随重定向、不使用 CookieJar。

worker 在短事务内用 `FOR UPDATE SKIP LOCKED` 领取一项任务，事务外请求 `POST /v1/storage/tenant-buckets`，JSON 为 `{"tenantID":"t_..."}`，使用上述独立 Bearer。成功响应必须返回相同 tenantID、非空 backendID 与 bucket。凭据只来自配置，不写入任务；失败记录不保存远端响应正文或含敏感信息的请求错误。未引用 artifact 整个 Go 模块，避免其新框架依赖向 tenant 引入版本耦合；此处只实现这一条管理 wire 契约。

任务状态为 pending/running/succeeded/failed；每次领取消耗一次预算，默认最多 5 次，包含进程中途退出的领取。失败后等待 2、4、8、16 秒再试，最终失败停止。租约为请求超时加 10 秒，进程退出后可在过期时重新领取；确认结果必须匹配租约 ID，旧进程不能覆盖新结果。storage 使用 TenantID 幂等确认同一个空间，解决远端成功但本地未确认的重放。已成功任务不会因业务版本变化重新开通。

## 现有租户和人工重排

迁移先创建表，再运行以下独立命令。命令复用现有配置和数据库装配，不启动 HTTP、不调用 Casdoor 或 GitLab；需要相应数据库写权限，不通过公网 API 暴露。

```sh
server migrate
server bucket-backfill
server worker
# 排除失败原因后，只重排明确指定且处于 failed 的单租户任务：
server bucket-retry t_example
```

回填按 tenantID 幂等登记，重复运行不会覆盖状态或结果。重排仅允许 failed，将次数清零，恢复 pending；pending/running/succeeded 或不存在的任务均报错，不能抢占在途租约。查询 `tenant_bucket_provisions` 中 status、attempts、next_attempt_at、lease_until、last_error、backend_id、bucket 可确认进度。配置为任意现有服务不等于该服务已经完成回填或开通，应以 succeeded 结果及真实 storage 联调为准。

本次定向测试覆盖真实 PostgreSQL 的创建回滚、重启过期租约、有限预算、幂等回填、受控重排、并发领取及旧确认拒绝，以及 GitLab 503 不阻塞 bucket worker。HTTP 测试覆盖返回 scope/字段校验、重定向拒绝和错误脱敏。真实部署及生产容量参数另行验收。

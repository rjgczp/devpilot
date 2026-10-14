# devpilot
DevPilot AI 是一个基于 Go + Python 的多租户 SaaS 监控与 AI 故障诊断平台。

## 用户注册

在启动 Go API 前，按[数据库迁移说明](docs/database-migrations.md)安装固定版本 Goose 并显式执行 migration up。API 不自动修改 Schema，原有 `/healthz` 与 `/readyz` 语义保持不变。

```sh
curl -i http://127.0.0.1:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  --data '{"email":"Alice.Example+tag@Example.COM","password":"a long local test phrase"}'
```

成功返回201和 `data`（id、规范化email、status、created_at），不签发登录令牌、Cookie或Session，不发送邮件。`active` **只表示未被禁用**，新用户 `email_verified_at=NULL`；后续功能不得仅凭 active 信任邮箱所有权。

邮箱采用MVP ASCII子集及全地址小写策略，去除首尾ASCII空格/Tab/CR/LF，保留点号和 `+tag`；规范化地址最多254 bytes、local part最多64 bytes。国际化、quoted local part等其他格式可能在别的邮件系统有效，但当前产品不支持。数据库UNIQUE约束是并发一致性的最终保证，重复邮箱返回409，既有密码/状态/验证/时间字段不变。这一接口明确暴露邮箱占用，不宣称抵御账号枚举。

密码为15–128个Unicode码点、最多512个UTF-8字节，不裁剪、截断或Unicode归一化，无字符类别要求。JSON body总长最多4096 bytes（包括尾随空白），只允许一个含email/password字符串的对象，拒绝未知/重复/缺失键与多值。先检查原始UTF-8和Unicode转义，拒绝孤立/反序/缺损代理项；仅检查JSON解码后的utf8.ValidString不足以保证这一点。合法代理对计一个码点，合法U+FFFD允许使用。

| HTTP | error.code | 含义 |
| --- | --- | --- |
| 201 | 无 | 已确认创建用户 |
| 400 | invalid_request | JSON结构、字段或原始Unicode转义错误 |
| 405 | method_not_allowed | 仅POST，附Allow: POST；HEAD无body |
| 413 | request_too_large | 超过4096 bytes |
| 415 | unsupported_media_type | 仅application/json及可选UTF-8 charset，无Content-Encoding |
| 422 | validation_failed | 不符合当前邮箱/密码策略 |
| 409 | email_conflict | 规范化邮箱已存在 |
| 503 | registration_busy | 两个哈希槽已满，Retry-After: 1 |
| 503 | service_unavailable | 连接不可用/中断，或服务端处理预算耗尽 |
| 503 | database_timeout | 数据库执行/取连接期限或确认的statement timeout |
| 500 | internal_error | 缺users等部署错误，或其他内部故障 |

错误体为 `{"error":{"code":"…","message":"固定安全文本"}}`，422可附仅含字段名和固定原因码的fields；响应为JSON并附 `Cache-Control: no-store`、`X-Content-Type-Options: nosniff`。不暴露密码、哈希或原始PgError/SQL/内部诊断；日志不记邮箱或请求体。完整机器可读契约见 [OpenAPI](docs/api/openapi.yaml)。

数据库超时/连接中断后，INSERT可能已经提交，错误只表示**无法确认注册结果**，不保证一定失败；服务端不自动重试。重试可能收到409，不能据此覆盖密码。客户端主动取消或断连时传播取消并停止发起后续工作，不尝试保证或写入503响应；服务端预算超时与客户端取消分开处理。

Argon2id暂定64 MiB、t=3、p=1，16-byte随机盐、32-byte输出、PHC编码。每进程最多2次同时哈希，无等待队列；4096-byte上限和廉价验证先于获取槽位，取消不能释放仍在计算的槽位。哈希真实返回才释放，GC/RSS回收不保证立即发生；这不是128 MiB进程内存硬上限。处理/数据库context预算为5s/2s，不能中断Argon本身。[基准与测试证据](openspec/changes/archive/2026-10-14-add-user-registration/verification.md)记录实际资源与延迟；不得为了性能目标擅自降低安全参数。

**公开部署前必须另行实现并评审注册限流与防滥用控制**。两个槽仅控制本进程并发资源，不防止持续批量注册或跨实例滥用；本Change不实现限流系统，不扩展登录、JWT、Workspace、Redis或AI能力。

单元测试无需数据库：`go test ./... -count=1`；竞态检查：`go test -race ./... -count=1`。真实PG18测试和安全隔离/清理步骤见[迁移说明](docs/database-migrations.md)。缺少TEST_DATABASE_URL或Goose时显式集成测试失败，不跳过。

## 本地配置

需要 Go 1.24 或更新版本与 Docker Compose。复制示例文件并自行设置本地密码：

```sh
cp .env.example .env
```

编辑 `.env`，填写 `POSTGRES_PASSWORD`，并设置匹配的 `DATABASE_URL`，格式为 `postgres://devpilot:<URL 编码后的本地密码>@127.0.0.1:5432/devpilot?sslmode=disable`。如果修改了 `POSTGRES_PORT`，也需同步修改连接 URL 的端口。`.env` 已被 Git 忽略；不要在仓库中提交真实凭据。Go 程序只读取进程环境，不自动读取 `.env`。在 bash 或 zsh 中显式导出：

```sh
set -a
. ./.env
set +a
```

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `DATABASE_URL` | 必填 | PostgreSQL 连接 URL |
| `HTTP_ADDR` | `:8080` | Go HTTP 监听地址 |
| `LOG_LEVEL` | `info` | `debug`、`info`、`warn`、`error` |
| `READINESS_TIMEOUT` | `2s` | 单次数据库就绪检查上限 |
| `SHUTDOWN_TIMEOUT` | `10s` | 收到退出信号后的总关闭预算 |
| `POSTGRES_USER` | `devpilot`（示例） | 本地 Compose 数据库用户 |
| `POSTGRES_DB` | `devpilot`（示例） | 本地 Compose 数据库名 |
| `POSTGRES_PASSWORD` | 必填 | 本地 Compose 数据库密码 |
| `POSTGRES_PORT` | `5432` | 仅绑定本机的数据库端口 |

配置错误会在 HTTP 服务监听前被拒绝，错误信息不会包含连接 URL 或密码。

## 本地 PostgreSQL

完成配置后，在仓库根目录启动 PostgreSQL 18，并等待健康检查通过：

```sh
docker compose up -d postgres
docker compose ps postgres
```

数据库仅映射到 `127.0.0.1`。停止服务会保留命名数据卷；确实需要清空本地数据时才使用 `down -v`：

```sh
docker compose stop postgres
docker compose down
docker compose down -v # 删除本地数据库数据
```

当前开发环境使用 `postgres:18`。后续部署时必须固定经验证的 PostgreSQL 18 小版本或镜像 digest，避免部署时自动升级到未经验证的镜像。

## 启动与停止 Go 服务

导出上述变量后，在仓库根目录运行：

```sh
go run ./cmd/api
```

启动、关闭和错误日志按行输出 JSON 到标准错误流。`GET /healthz` 只检查 Go 进程；`GET /readyz` 还会检查 PostgreSQL。HTTP 服务器的读头、读取、写入、空闲超时默认分别为 5s、10s、10s、60s。

保持 Go 服务运行，在另一个终端验收数据库故障与恢复。以下命令只输出 HTTP 状态码：

```sh
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/healthz # 200
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/readyz  # 200
docker compose stop postgres
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/healthz # 200
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/readyz  # 503
docker compose start postgres
docker compose ps postgres # 等待状态变为 healthy
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/readyz  # 200，无需重启 Go 服务
```

服务收到 SIGINT 或 SIGTERM 时立即变为未就绪，然后在 `SHUTDOWN_TIMEOUT` 指定的**总预算**内停止接收新请求、等待现有请求并关闭连接池。正常完成时退出码为 0；超时或关闭异常时记录原因，强制关闭剩余 HTTP 连接并以非零状态退出，不增加额外等待。`http.Server.Close()` 不能强制终止任意后台任务；任务必须响应 Context 取消。超时路径不保证未完成请求或尚被占用的数据库连接能优雅结束。

可用独立二进制验证信号退出，避免 `go run` 的包装进程影响退出码观察：

```sh
go build -o /tmp/devpilot-api ./cmd/api
/tmp/devpilot-api &
api_pid=$!
kill -TERM "$api_pid" # 也可使用 kill -INT
wait "$api_pid"
```

# Tasks

## 1. Go 工程与环境配置

- [x] 1.1 建立 `github.com/rjgczp/devpilot` Go 模块、`cmd/api` 入口及必要的 `internal` 包；运行 `go test ./...` 确认工程可编译。
- [x] 1.2 实现仅从进程环境读取的配置加载器，校验 `DATABASE_URL`、`HTTP_ADDR`、`LOG_LEVEL`、`READINESS_TIMEOUT`、`SHUTDOWN_TIMEOUT`；用表驱动测试验证缺省关闭超时为 10s、合法覆盖值、缺失值、非法地址或连接串、非正数或畸形超时，且错误不回显凭据。
- [x] 1.3 提供只含占位示例的 `.env.example`，在 README 说明变量默认值与显式导出方式；用单元测试验证 Go 进程不自动读取 `.env`，用 `git check-ignore .env` 确认真实配置被忽略，并检查示例未包含真实凭据。

## 2. HTTP 运行、日志与退出

- [x] 2.1 建立 `net/http` 服务及 `log/slog` JSON 日志；测试启动、关闭和模拟数据库错误的日志可解析为 JSON，且不包含 `DATABASE_URL`、密码或原始驱动错误。
- [x] 2.2 配置 `ReadHeaderTimeout=5s`、`ReadTimeout=10s`、`WriteTimeout=10s`、`IdleTimeout=60s` 的明确默认值；用服务构造测试核对四项值，并测试不完整请求头连接在限定时间内关闭。
- [x] 2.3 实现 `signal.NotifyContext` 对 SIGINT、SIGTERM 的处理、关闭状态切换、受管理任务的 context 取消，以及共享 10s 默认总截止时间的 `http.Server.Shutdown` 和连接池关闭；进程级测试分别发送两种信号，验证就绪状态先变 503、短请求完成、资源关闭且正常退出。
- [x] 2.4 实现 `Shutdown` 超时或异常时记录原因、调用 `http.Server.Close()`、在同一剩余预算内尽力清理资源并非零退出，不增加固定等待时间；用长请求和不配合取消的任务替身测试验证不会无限等待，并在 README 说明 `Server.Close()` 不能强制终止任意后台任务及超时清理限制。

## 3. PostgreSQL 与健康接口

- [x] 3.1 增加使用 `postgres:18` 的 Docker Compose 服务、挂载 `/var/lib/postgresql` 的命名数据卷、`pg_isready` 健康检查和只绑定 `127.0.0.1` 的端口；通过必填 `POSTGRES_PASSWORD` 环境变量注入密码，运行 `docker compose config --quiet` 并检查镜像、卷路径、端口与缺失密码时的失败行为。
- [x] 3.2 接入 `pgxpool`，在监听前校验连接配置但不要求数据库在线；测试非法连接配置阻止启动，而数据库暂时不可用时 HTTP 服务仍可启动并响应 `/healthz`。
- [x] 3.3 实现 `GET /healthz` 和 `GET /readyz`，对后者设置有界数据库检查，并在 Ping 前及写出 200 前检查关闭状态；使用可控的数据库检查替身测试正常、故障、超时、故障后恢复、关闭期间仍可访问以及响应不泄露敏感信息。
- [x] 3.4 在 README 写明数据库启动、停止、清理数据、健康接口验收命令，以及后续部署需固定 PostgreSQL 18 小版本或 digest；按文档使用同一 Go 进程验证数据库在线时两个接口为 200、离线时 `/healthz` 为 200 且 `/readyz` 为 503、恢复后 `/readyz` 自动回到 200。

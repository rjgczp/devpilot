# Bootstrap Go Backend 验收记录

验收日期：2026-10-13（Asia/Shanghai）。环境：macOS arm64、Go 1.24.3、Docker Engine 29.4.1、PostgreSQL 18 镜像。本文件记录本次实现的实际结果；Change 保持未归档。

## 自动化检查

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| `gofmt`（`cmd`、`internal`） | 通过 | `gofmt -l` 无输出 |
| `go vet ./...` | 通过 | 退出码 0 |
| `go test ./... -count=1` | 通过 | `cmd/api`、`internal/app`、`internal/config`、`internal/health` 四个包均为 `ok` |
| `go test -race ./... -count=1` | 通过 | 同上四个包均为 `ok`，未报告 data race |
| `go test -race ./internal/app -run TestRunner -count=20` | 通过 | 关闭路径重复 20 次，未报告失败或 data race |
| `go test ./cmd/api -run TestProcessSignalsWithInFlightRequest -count=5 -v` | 通过 | SIGINT 和 SIGTERM 两个子测试各通过 5 次 |
| GitHub Actions 工作流 | 通过 | PR #1 的 Go CI 执行 gofmt、`go vet ./...`、`go test ./... -count=1`、`go test -race ./... -count=1`；运行记录：https://github.com/rjgczp/devpilot/actions/runs/38361179216 |

## Docker Compose 集成验收

仓库原样配置通过 `docker compose config --quiet`；解析结果为 `postgres:18`、`127.0.0.1:5432`、命名卷 `postgres_data` 挂载到 `/var/lib/postgresql`。缺少 `POSTGRES_PASSWORD` 时，Compose 配置命令以非零状态退出。

本机 Docker 存储空间不足，原样配置的命名卷初始化报 `No space left on device`。未清理其他项目的 Docker 数据。为验证服务行为，使用同一 `compose.yaml` 加临时覆盖文件，仅将数据卷换为主机临时目录；镜像、密码注入、健康检查和回环端口配置保持一致。覆盖配置下 PostgreSQL 18 显示 `healthy`，端口显示 `127.0.0.1:5432->5432/tcp`。

同一个 Go 进程监听 `127.0.0.1:18081`，未在数据库停止和恢复之间重启：

| 数据库状态 | `/healthz` | `/readyz` |
| --- | --- | --- |
| 在线 | 200 | 200 |
| Compose `stop postgres` 后 | 200 | 503 |
| Compose `start postgres` 后 | 200 | 200 |

最后向 Go 进程发送 SIGINT，退出码为 0，日志包含 `server stopped`。验收容器、网络、临时数据目录和二进制已清理。仓库默认命名卷的**实际启动**仍需在 Docker 存储空间充足的环境复验。

## 对照 OpenSpec Specs

状态统计：通过 21，失败 0，未验证 1。这里的“通过”包含自动化测试、配置检查或上述真实数据库验收；“未验证”表示本机环境无法完成该场景。

| Spec / 场景 | 状态 | 证据或限制 |
| --- | --- | --- |
| backend-runtime / Valid configuration | 通过 | `TestLoadDefaultsAndOverrides`、进程启动测试 |
| backend-runtime / Invalid configuration | 通过 | `TestLoadRejectsInvalidConfigurationWithoutSecrets`、`TestProcessRejectsInvalidConfigurationWithoutSecret` |
| backend-runtime / Configuration file is not imported automatically | 通过 | `TestLoadDoesNotReadDotEnvFile` |
| backend-runtime / Default and invalid shutdown timeouts | 通过 | `TestLoadDefaultsAndOverrides`、`TestLoadRejectsInvalidConfigurationWithoutSecrets` |
| backend-runtime / Developer prepares local configuration | 通过 | `.env.example` 无真实凭据；`git check-ignore .env` 输出 `.env` |
| backend-runtime / Local database is started | 未验证 | 默认命名卷初始化因本机 Docker 空间不足而失败；临时主机目录覆盖下 PostgreSQL 18 正常启动并仅发布到回环地址 |
| backend-runtime / Password is absent | 通过 | 不设置 `POSTGRES_PASSWORD` 时 `docker compose --env-file /dev/null config --quiet` 失败 |
| backend-runtime / Service starts（日志） | 通过 | `TestProcessSignals` 验证 JSON 启动日志 |
| backend-runtime / Database operation fails（日志） | 通过 | `TestHealthAndReadinessTransitions` 验证 JSON 错误日志且无连接串和密码 |
| backend-runtime / SIGINT with in-flight request | 通过 | `TestProcessSignalsWithInFlightRequest/interrupt`、`TestRunnerGracefulShutdown` |
| backend-runtime / SIGTERM with in-flight request | 通过 | `TestProcessSignalsWithInFlightRequest/terminated`、`TestRunnerGracefulShutdown` |
| backend-runtime / Shutdown deadline expires | 通过 | `TestProcessShutdownTimeout`、`TestRunnerShutdownDeadline` |
| backend-runtime / Shutdown fails before the deadline | 通过 | `TestRunnerShutdownError`、`TestRunnerResourceCloseError` |
| backend-runtime / Client does not finish sending headers | 通过 | `TestIncompleteHeadersTimeOut` |
| backend-runtime / HTTP timeout defaults | 通过 | `TestNewServerTimeouts` |
| service-health / Service is running | 通过 | `TestHealthAndReadinessTransitions`、真实 Compose 验收 |
| service-health / PostgreSQL is unavailable（存活） | 通过 | `TestProcessStartsWhenDatabaseIsUnavailable`、真实 Compose 验收 |
| service-health / PostgreSQL is reachable | 通过 | 真实 Compose 验收：`/readyz` 为 200 |
| service-health / PostgreSQL is unavailable（就绪） | 通过 | `TestReadinessTimeoutAndResponsePrivacy`、真实 Compose 验收：`/readyz` 为 503 |
| service-health / PostgreSQL recovers | 通过 | 同一 Go 进程真实 Compose 验收：`/readyz` 恢复为 200 |
| service-health / Shutdown has begun | 通过 | `TestShutdownDuringPingIsNotReady`、`TestRunnerGracefulShutdown` |
| service-health / Readiness check fails（信息保护） | 通过 | `TestReadinessTimeoutAndResponsePrivacy`、`TestHealthAndReadinessTransitions` |

## 关闭风险审查

- 正常路径先取消并等待受管理任务，再关闭数据库池等资源；`TestRunnerClosesResourceAfterWorkerExit` 验证顺序。`Shutdown`、任务等待和资源关闭共用一个截止时间；`TestRunnerResourceCloseDeadline` 验证阻塞的资源关闭不会使退出无限等待。
- HTTP 超时或异常时调用 `Server.Close()`，返回非零状态；资源关闭失败仅记录固定错误类别，不记录可能含凭据的底层错误。
- `go test -race` 全量通过，关闭测试重复 20 次未发现数据竞争。这是测试范围内的结果，不等同于证明所有调度下均无竞态。
- 超时后无法强制停止不响应 Context 的任意后台 goroutine，也无法保证阻塞中的资源关闭函数在进程退出前完成；这是设计中已说明的限制，进程非零退出后由操作系统回收进程资源。

# devpilot
DevPilot AI 是一个基于 Go + Python 的多租户 SaaS 监控与 AI 故障诊断平台。

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

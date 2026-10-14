# 用户数据库迁移

迁移必须由操作人员或单一发布步骤显式执行，API 启动不修改 Schema。业务连接继续使用 pgx；迁移直接使用 Goose CLI，不包含自定义迁移引擎、Go 入口或锁。

## 固定工具

Goose **v3.26.0** 使用 Go 1.23 基线，可在仓库 Go 1.24.3 上构建；v3.27.0 要求 Go 1.25，因此本次保留 v3.26.0。密码模块固定 `x/crypto v0.48.0`（Go 1.24）；v0.49.0 要求 Go 1.25。仅使用密码模块的 Argon2id 包，不添加 SSH/TLS 等能力；后续升级需继续审查实际使用包的安全修复及工具链兼容性，不以 `@latest` 隐式升级。

```sh
go install -tags='no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb' github.com/pressly/goose/v3/cmd/goose@v3.26.0
export PATH="$(go env GOPATH)/bin:$PATH"
goose -version # v3.26.0
```

## 显式 Up 与状态

先按 README 显式导出本地环境。在仓库根目录执行；连接串通过环境传递，不放在命令参数中：

```sh
export GOOSE_DRIVER=postgres
export GOOSE_DBSTRING="$DATABASE_URL"
goose -env=none -dir ./migrations -timeout 30s status
goose -env=none -dir ./migrations -timeout 30s up
goose -env=none -dir ./migrations -timeout 30s status
```

`-env=none` 禁用 Goose 默认 `.env` 导入。`-timeout` 使用原生查询超时语义，不代表自定义进程总预算。默认事务保证一份 SQL migration 和版本更新原子提交；失败返回非零，不留下半张 users 表。`goose_db_version` 记录版本，重复 up 不改变已迁移数据。已应用 SQL 不得修改，通过新增后续版本修复。共享数据库一次只由一个受控步骤迁移，不假定未配置的 CLI 自带并发锁。

部署顺序是备份（有现存数据时）、串行 status/up/status、部署 API、验收注册。没有 users 时 API 仍能启动；`/readyz` 只 Ping，可能返回 200，而注册返回安全 500。这属于部署缺陷，不能靠健康检查替代 schema 验收。数据库连接故障返回 503，数据库执行期限到达返回 503 `database_timeout`。客户端取消不保证 HTTP 响应；INSERT 已提交但未确认时不承诺失败，不自动重试、补偿或覆盖旧账户。

Goose 原生失败输出可能包含 SQL 和驱动诊断，只能保留在受限操作终端。不要启用 verbose、打印 Goose 环境，或将原始输出贴进公开日志、工单、文档和 API；只分享脱敏摘要。API 日志与响应另有严格脱敏边界，不接入迁移原始输出。

## Down 和数据保护

常规 Down **仅用于操作人员明确确认可丢弃的开发或测试库**，会删除 users 及其数据。确认 GOOSE_DBSTRING 指向专用 disposable 库后，原生单步命令为 `goose -env=none -dir ./migrations -timeout 30s down`。Goose 不替本项目识别环境或弹出确认，不存在自定义 `--allow-destructive` 参数。

共享或生产日常发布只允许 status/up，禁止 down、down-to、redo、reset 作为自动回滚。回滚 API 保留新增表和用户数据，Schema 错误优先前向修复。异常破坏性恢复需要独立授权、已验证备份、恢复方案评审；不得承诺无损恢复。本 PR 不部署到生产。

## 专用 PostgreSQL 18 集成测试

本地启动专用容器（这些是可丢弃测试凭据，不能用作生产密码），不挂载开发卷：

```sh
docker run -d --name devpilot-registration-test \
  -e POSTGRES_USER=devpilot_test -e POSTGRES_PASSWORD=registration_test_only \
  -e POSTGRES_DB=devpilot_registration_test \
  -p 127.0.0.1:55432:5432 postgres:18
docker exec devpilot-registration-test pg_isready -U devpilot_test -d devpilot_registration_test
export TEST_DATABASE_URL='postgres://devpilot_test:registration_test_only@127.0.0.1:55432/devpilot_registration_test?sslmode=disable'
go test -tags=integration ./... -count=1 -timeout=120s
```

测试只接受库名 `devpilot_registration_test`，连接后再核对 current_database 和 PostgreSQL 18 版本。若 DATABASE_URL 也指向这个库，直接拒绝，即使连接用户/主机写法不同。不允许回退到应用连接串；无配置或无法连接必须失败，不得跳过。不要并行启动两个集成 suite 操作该库。测试用固定版本 Goose 和同一 SQL 目录验证 up/up、up/down/up 与失败回滚，运行期间会删除测试数据。

结束后仅清理这个专用容器：`docker rm -f devpilot-registration-test`。不要对开发 Compose 环境运行 down -v 来清理本测试。

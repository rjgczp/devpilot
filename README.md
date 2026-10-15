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

Argon2id暂定64 MiB、t=3、p=1，16-byte随机盐、32-byte输出、PHC编码。注册Hash与登录real/dummy Verify共用每进程最多2次同时计算，无等待队列；4096-byte上限和廉价验证先于获取槽位，取消不能释放仍在计算的槽位。哈希真实返回才释放，GC/RSS回收不保证立即发生；这不是128 MiB进程内存硬上限。处理/数据库context预算为5s/2s，不能中断Argon本身。[基准与测试证据](openspec/changes/archive/2026-10-14-add-user-registration/verification.md)记录实际资源与延迟；不得为了性能目标擅自降低安全参数。

**公开部署前必须另行实现并评审注册限流与防滥用控制**。两个槽仅控制本进程并发资源，不防止持续批量注册或跨实例滥用；注册与登录Change均不实现限流系统，公开部署前必须完成相应防滥用验收。

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
| `AUTH_MODE` | `production` | 仅production/development，生产HTTPS安全Cookie；开发HTTP必须显式选择 |
| `AUTH_ORIGIN` | 必填 | 唯一HTTP(S) origin，不含路径、末尾/、凭据、query、fragment或列表 |
| `SESSION_TTL` | `24h` | 绝对期限，可配置5m–24h，无滑动续期/Refresh Token |
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

## 共享密码计算预算

注册 Hash 与登录的真实/Dummy Verify 共用每进程两个 Argon2 槽，无等待队列；取消不能释放仍在运行的计算，真实返回或异常退出后才释放。登录另有两个完整流程槽，数据库等待不继续占用密码槽。共享容量竞争及共同 PostgreSQL/连接池故障仍可能影响注册，不承诺公平调度或完全故障隔离。

密码 Verify 严格接受当前规范 PHC：Argon2id v19、m=65536 KiB/t=3/p=1、16-byte盐、32-byte输出及严格无padding标准Base64；其他组合在计算前拒绝，未来参数演进必须先评审允许列表和资源预算。不存在邮箱或坏PHC使用预计算的同成本Dummy Verify，其结果永远不能认证用户。

## 用户登录与 PostgreSQL Session

先显式执行版本2迁移。默认生产配置要求 `AUTH_MODE=production` 和 `AUTH_ORIGIN=https://你的API域名`；Cookie 固定 `__Host-devpilot_session`、Secure、HttpOnly、SameSite=Lax、Path=/、无Domain。Go可以在受信任HTTPS入口后通过内部HTTP服务，但入口必须强制外部HTTPS并保护内部监听，不得记录Cookie；请求Host/Forwarded/X-Forwarded-*不能改变策略。

示例 `.env` 显式使用本地例外：`AUTH_MODE=development`、`AUTH_ORIGIN=http://127.0.0.1:8080`、`HTTP_ADDR=127.0.0.1:8080`。开发Origin仅HTTP localhost/回环IP，监听必须显式IP回环且端口匹配，Cookie名称为devpilot_session、仅此模式省略Secure。本地Cookie没有端口隔离；不能把本模式用于公开部署。AUTH_ORIGIN缺失/不合法、非法模式或TTL范围会在监听前失败；原配置需补充此必填项。

```sh
# 以下仅用于本地可丢弃测试账户。Cookie jar含bearer secret，限制权限。
cookie_jar=$(mktemp)
chmod 600 "$cookie_jar"
curl -i -c "$cookie_jar" http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -H 'Origin: http://127.0.0.1:8080' -H 'X-DevPilot-CSRF: 1' \
  --data '{"email":"Alice.Example+tag@Example.COM","password":"a long local test phrase"}'
curl -i -b "$cookie_jar" http://127.0.0.1:8080/api/v1/auth/me
curl -i -b "$cookie_jar" -c "$cookie_jar" -X POST \
  -H 'Origin: http://127.0.0.1:8080' -H 'X-DevPilot-CSRF: 1' \
  http://127.0.0.1:8080/api/v1/auth/logout
rm "$cookie_jar"
```

两个POST都要求唯一Origin（完整scheme/host/effective-port匹配配置）和唯一 `X-DevPilot-CSRF: 1`；固定头不是秘密。Sec-Fetch-Site若出现只能唯一same-origin；缺/null/重复/跨源Origin、错误头和Referer-only返回403，不查询账户或改Cookie。CLI也必须带两个头。无CORS授权，OPTIONS为405，GET不能退出。me只读，不需要这两个头。

登录沿用注册严格4096-byte JSON、原始Unicode/重复键/媒体和邮箱政策。验证密码接受1–128 Unicode码点/最多512 bytes，不trim/normalize；注册创建新密码仍最少15码点。邮箱不存在、错误密码、disabled及坏PHC统一401 invalid_credentials，固定消息 `Invalid email or password.`，每类执行一遍相同参数real/dummy验证；不发/删Cookie。网络耗时并非恒时，注册409仍暴露占用，本系统不宣称完全防枚举。

成功login=200及原Public DTO；仅确认session commit后签发新32-byte随机令牌的Cookie，数据库只存SHA-256(decoded secret)完整32-byte摘要。每次登录创建独立会话，不自动撤销之前会话，不改用户密码、状态、验证或时间字段。仅接受当前模式唯一规范Cookie，拒绝重复/非规范值，不接受Authorization/URL/body Token。Token/摘要/密码/PHC/邮箱/原始错误不进入日志或错误体，Token只在成功Set-Cookie出现。

默认 `expires_at-created_at=24h`，每次数据库查询强制 `expires_at > statement_timestamp()`、`revoked_at IS NULL` 和用户active。me成功200，任何无效身份同样401 unauthenticated / `Authentication required.`；me不改时间、不发Set-Cookie，无滑动续期、Refresh Token或refresh端点。即使过期/撤销行尚未删除或浏览器继续发送Cookie，也必须拒绝认证。

logout只撤销当前token；无/坏/重复Cookie也幂等204（不挑选任一重复token），空body并清当前模式Cookie。disabled/过期的token也可撤销，其他Session不受影响。DB故障返回500/503并保留Cookie，不能宣称退出已完成；未知commit只能由客户端显式幂等重试，服务器不自动重试。Cookie清除不是撤销证据。已经在撤销前获得身份快照的在途请求可完成；login/logout响应可能乱序，客户端应串行认证变更。本轮无多设备管理UI。

| HTTP | error.code | 新端点适用情况 |
| --- | --- | --- |
| 400 | invalid_request | login JSON结构/字段/原始Unicode错误 |
| 401 | invalid_credentials / unauthenticated | login统一凭据拒绝 / me无有效身份 |
| 403 | csrf_rejected | login/logout来源或头证明失败 |
| 405 | method_not_allowed | login/logout仅POST，me仅GET，精确Allow；HEAD无body |
| 413 / 415 / 422 | request_too_large / unsupported_media_type / validation_failed | login输入界限，422只含固定字段原因码 |
| 503 | authentication_busy | login共享Argon或完整流程槽满，Retry-After:1 |
| 503 | service_unavailable / database_timeout | 连接/处理预算失败或DB期限/确认statement timeout，无法确认结果 |
| 500 | internal_error | 缺users/sessions、随机源/验证器/内部故障 |

三个端点带no-store/nosniff，JSON采用data/error envelope，204无JSON。完整固定message和字段契约见OpenAPI。DB故障不能伪装401、使用缓存或默认认证通过；取消后不写响应，不据此承诺回滚。

清理Worker由Runner管理，每15分钟尝试一个最多1000行/2s批次，SKIP LOCKED避免多实例等待同一候选。只删除过期/撤销行，不循环耗尽；失败只记安全类别，下一周期再试，认证有效性不受清理失败影响。关闭取消Worker，在原总期限内等待后关池。当前无审计保留承诺，清理吞吐/表增长须随部署容量评审。

身份验证仅是查询时点的user UUID，不是整个业务操作期间的授权租约。未来Workspace必须分别检查资源/动作/membership/role，定义权限变更与写提交的锁/条件写/版本或隔离协议；普通事务内的一次SELECT或入口middleware都不能自动保证全程权限，锁也不能停止时钟到期。本轮无Workspace/RBAC。active仍不证明邮箱所有权；直接SQL禁用后再解禁可能恢复未撤销会话，未来disable操作必须事务内revoke-all。

**公开部署前仍必须另行实现并评审登录限流、防暴力破解/撞库与注册反滥用**：可信来源IP和canonical account键的速率/突发及失败预算、跨实例一致性、渐进延迟/恢复窗口、429与监测告警验收；不存在邮箱也需一致处理，避免硬锁账户产生DoS，不信任任意代理头。资源槽不是这些控制的替代，本轮没有实现或验收此限流系统。

登录与Session的实际测试、场景映射和混合Argon2资源基线见 [add-user-login验收记录](openspec/changes/archive/2026-10-15-add-user-login/verification.md)。

## Workspace 基础

版本3迁移增加Workspace与成员关系，版本1/2保持不变，按[迁移说明](docs/database-migrations.md)显式升级。用户可以创建多个Workspace；成员管理单独交付，本轮没有加入、邀请、更新、删除或角色变更入口。owner/member仅能读取所属Workspace，创建者自动成为owner。

三个接口为`POST /api/v1/workspaces`、`GET /api/v1/workspaces`、`GET /api/v1/workspaces/{workspace_id}`。Cookie名称和严格解析沿用认证协议，POST必须携带匹配AUTH_ORIGIN的唯一Origin和`X-DevPilot-CSRF: 1`，GET只读无需CSRF头。接口不修改Cookie、不续期。正常请求无效身份401；已认证非成员与未知资源同为404；有效用户无成员关系返回200空数组。

```sh
curl --cookie "$COOKIE_JAR" --header "Origin: $AUTH_ORIGIN" \
  --header 'X-DevPilot-CSRF: 1' --header 'Content-Type: application/json' \
  --data '{"name":"研发团队"}' "$API_BASE/api/v1/workspaces"
curl --cookie "$COOKIE_JAR" "$API_BASE/api/v1/workspaces?limit=50"
```

以上环境变量由调用者显式设置，Cookie文件须私密保存。名称允许重复，无slug；仅裁剪边缘ASCII空白，保留大小写/Unicode，要求1–100码点/最多400bytes，无Cc控制字符及剩余Unicode边缘空白。成功投影仅id/name/role/created_at/updated_at，role属于调用者。

创建返回201和Location，Workspace与初始owner在一个短事务原子提交。事务按User→Session锁定，取锁后以新的数据库语句时间校验Session。提交结果未知时不会返回成功或自动重试；客户端重新提交可能创建第二个同名Workspace，名称不能作幂等键。列表可用于检查实际存储，不能可靠关联同名的未知请求。

列表返回`{"data":[],"pagination":{"next_after":null}}`或授权资源数组，按UUID升序；limit默认50、1–100，after为规范小写UUID，next_after仅有更多记录时为最后返回ID。游标不提供权限，无总数及跨页快照保证。详情无查询参数，列表仅接受limit/after；重复、未知或非法参数400。完整状态/字段见[OpenAPI](docs/api/openapi.yaml)。

租户隔离由每次查询同时校验Session、active账户、成员关系和资源归属，不启用RLS；未来租户业务表必须含workspace_id，租户内唯一及父子外键也需包含租户边界。数据库管理员不受应用授权约束。成员关系使users删除RESTRICT，防止静默丢失所有权；本轮不提供账户删除或最后owner管理。公开部署前沿用既有认证反滥用要求，并需评审Workspace创建频率/容量，当前没有配额系统。

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

身份验证仅是查询时点的user UUID，不是整个业务操作期间的授权租约。未来Workspace必须分别检查资源/动作/membership/role，定义权限变更与写提交的锁/条件写/版本或隔离协议；普通事务内的一次SELECT或入口middleware都不能自动保证全程权限，锁也不能停止时钟到期。认证端点本身不授予Workspace权限。active仍不证明邮箱所有权；直接SQL禁用后再解禁可能恢复未撤销会话，未来disable操作必须事务内revoke-all。

**公开部署前仍必须另行实现并评审登录限流、防暴力破解/撞库与注册反滥用**：可信来源IP和canonical account键的速率/突发及失败预算、跨实例一致性、渐进延迟/恢复窗口、429与监测告警验收；不存在邮箱也需一致处理，避免硬锁账户产生DoS，不信任任意代理头。资源槽不是这些控制的替代，本轮没有实现或验收此限流系统。

登录与Session的实际测试、场景映射和混合Argon2资源基线见 [add-user-login验收记录](openspec/changes/archive/2026-10-15-add-user-login/verification.md)。

## Workspace 基础

版本3迁移增加Workspace与成员关系，版本1/2保持不变，按[迁移说明](docs/database-migrations.md)显式升级。用户可以创建多个Workspace；成员名单、移除与退出见下节；加入与邀请见后文；没有Workspace更新/删除或角色变更入口。owner/member可读取所属Workspace，创建者自动成为owner。

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

租户隔离由每次查询同时校验Session、active账户、成员关系和资源归属，不启用RLS；未来租户业务表必须含workspace_id，租户内唯一及父子外键也需包含租户边界。数据库管理员不受应用授权约束。成员关系使users删除RESTRICT，防止静默丢失所有权；本轮不提供账户删除；最后Owner退出由下述事务协议保护。公开部署前沿用既有认证反滥用要求，并需评审Workspace创建频率/容量，当前没有配额系统。

## Workspace 成员名单、移除与退出

仅支持 `GET /api/v1/workspaces/{workspace_id}/members` 与 `DELETE /api/v1/workspaces/{workspace_id}/members/{user_id}`。无POST成员或角色授予接口；加入/重加只通过下述目标Session明确接受邀请，知道UUID不等于同意，新加入角色只能为member。

Owner和Member均可查看名单，仅含membership_id、user_id、role、joined_at（成员关系创建时间，UTC RFC3339），不含邮箱、昵称或账户status。GET接受单一limit/after，默认50、最大100，按user_id升序，无total或跨页快照；游标不授予权限，也不检查它对应的用户是否存在。每一页通过一次数据库语句同时检查active账户、有效Session与当前成员关系。

DELETE只接受路径中的目标UUID，无query/非空body，无JSON输入要求；操作者只能来自Cookie。DELETE必须带与既有POST相同的Origin、X-DevPilot-CSRF: 1及可选same-origin Fetch Metadata。Member仅可自退；Owner可移除普通Member（含disabled账户的关系），不能移除其他Owner。Owner仅在仍有其他Owner关系时自退，最后Owner自退409 last_owner_required；只保证Owner关系数量，不保证至少一名active Owner。

确认提交后204空body，无JSON Content-Type、Location或Set-Cookie；严格强If-Match必须为 `"membership:<membership_id>:<role>"`（来自名单当次关系）；缺失428 precondition_required，关系缺失/代际或角色不匹配412 precondition_failed，弱tag/通配/列表/重复/非法tag400，不查询全局用户存在性。Owner重复旧删除变412，自退后无权限404。成员退出后重试因无成员资格404；无效Session401，禁止移除403 membership_forbidden。非成员、未知或非规范Workspace同404 workspace_not_found；已授权Workspace下非规范目标UUID为404 member_not_found。参数/非空body400；依赖/锁期限503，schema/内部错误500，固定错误文本见OpenAPI。

所有成员写遵循User FOR SHARE → Session FOR SHARE → Workspace FOR NO KEY UPDATE → Membership按user_id排序FOR UPDATE，同Workspace串行；取得全部可能阻塞的保护锁之后发新语句重新验证身份、过期时间、权限及Owner数量，删除始终同时绑定workspace_id、user_id、membership_id和role。5s处理/2s整个事务预算。未来成员/角色writer和受支持维护事务也必须使用同样顺序，不能先锁Membership/Session再反向锁Workspace/User。本阶段追加Migration4增加成员代际和邀请，无计数器、触发器或RLS；原PK/FK/CHECK不保证最后Owner，受信任直接SQL、特权DDL或TRUNCATE仍可绕过应用协议。不提供生产SQL加入/角色修改流程。

撤销/禁用/成员移除在新读取快照前提交即生效；已取得授权快照的在途读取可以完成。写先取得保护锁时，冲突的撤销或权限变更等待其结束；最终门控后的自然过期不强制回滚。旧Cookie、游标或先前响应不能授权新请求，移除也不会清除整个用户Session。

未知Commit、超时或取消不能证明回滚，不自动重试、不补偿添加、不返回未确认204；客户端取消不再尝试响应。Owner可重新读取名单/显式删除检查状态，自退用户可查看自己的Workspace列表；401本身不是删除成功证据。没有请求去重或exactly-once承诺；旧validator不会匹配重加的新关系，客户端不得自动刷新条件并重试。日志不含成员UUID、原始路径/query、Cookie、头/body、邮箱/名称、SQL/原始错误。

```sh
curl --cookie "$COOKIE_JAR" "$API_BASE/api/v1/workspaces/$WORKSPACE_ID/members?limit=50"
curl --cookie "$COOKIE_JAR" --request DELETE \
  --header "Origin: $AUTH_ORIGIN" --header 'X-DevPilot-CSRF: 1' \
  --header "If-Match: \"membership:$MEMBERSHIP_ID:$MEMBER_ROLE\"" \
  "$API_BASE/api/v1/workspaces/$WORKSPACE_ID/members/$TARGET_USER_ID"
```

变量由调用者显式设置，自己的UUID可从auth/me取得。MEMBERSHIP_ID和MEMBER_ROLE必须来自先前名单的目标关系，重试复用旧值。数据库角色、迁移1/2/3字节、Session绝对TTL与健康检查均不改变；回退应用不会恢复已经移除的权限。公开部署前仍需完成既有认证防滥用评审。

## Workspace 平台内定向邀请

Owner先通过既有沟通获得目标在auth/me中的UUID；不提供用户搜索、邮件或邀请码。创建成功只建立pending邀请，不建立成员关系。邀请归属Workspace；发起者移除/降为Member/禁用不使它失效，任何现任Owner可撤销。仅目标自己的active账户及有效Cookie Session可接受/拒绝；Owner不能代替同意。角色固定member，退出后必须新邀请，不重放accepted。

| 接口 | 请求/权限 | 成功 |
| --- | --- | --- |
| POST /api/v1/workspaces/{workspace_id}/invitations | Owner，严格单字段invitee_user_id JSON | 201 OwnerInvitation |
| GET /api/v1/workspaces/{workspace_id}/invitations | Owner，limit/after/status | 200 Owner列表 |
| GET /api/v1/invitations | 本人Session，limit/after/status | 200 本人收件箱 |
| POST /api/v1/invitations/{invitation_id}/accept | 接收者Session，空body/无query | 200 invitation_id/workspace_id/membership_id/role/joined_at |
| POST /api/v1/invitations/{invitation_id}/decline | 接收者Session，空body/无query | 204 |
| DELETE /api/v1/invitations/{invitation_id} | 现任Owner，空body/无query | 204撤销，保留记录 |

所有写要求同源CSRF，GET不写。创建最多4096 bytes、application/json（可带UTF-8 charset）、无编码，拒绝重复/未知/缺失/非字符串字段及非法Unicode。错误400/413/415；有Owner权限后，unknown/disabled/self/已有成员及非规范目标UUID统一422 invitee_unavailable。Member创建/Owner列表403，非成员Workspace404；item未知/非规范/不属接收者或非Owner撤销统一404 invitation_not_found。路径别名不重定向到写入。完整固定消息/响应/schema见OpenAPI。

pending仅能到accepted/declined/revoked/expired，固定604800秒；终态重复409 invitation_not_pending，过期409 invitation_expired；不会复活或覆盖角色。每对最多1存储pending，重复409 invitation_pending；每Workspace最多100有效pending，超限409 invitation_limit_reached。Owner创建成功意味着UUID可邀请，已承认有限可邀请性泄露，不宣称零枚举。

两个列表limit默认50/1–100、after为规范UUID、status仅pending（默认）/all，按邀请UUID升序、最多limit+1，无total/跨页快照。Owner字段恰好id/workspace_id/invitee_user_id/created_by_user_id/status/created_at/updated_at/expires_at；目标字段恰好id/workspace_id/workspace_name/status/created_at/updated_at/expires_at。当前名称仅对有效pending可见，终态历史workspace_name=null，不能跟踪撤销后的改名；邀请不是detail/roster权限。

所有mutation按Users UUID升序→Session→Workspace→Membership→Invitation保护后，以新statement_timestamp重验active、Session/邀请严格未过期、当前权限/归属与owner完整性。接受状态转换和新member原子提交，确认Commit才成功；门控后的自然过期可提交。未知提交不自动重试/补偿；all列表和成员列表帮助观察。没有RLS/任意SQL最后Owner保护保证。

过期不依赖Worker。GET只投影effective expired（updated_at=expires_at）；授权accept/decline/revoke触及过期pending会持久化expired、确认Commit再返回409。创建替换先验证Owner/目标；旧过期pending在替换成功时同事务提交，若有效pending配额已满，则**仍确认过期转换Commit后返回409**。授权/目标校验失败不转换状态；SQL/Commit失败503/500不能假称过期已落库。

**旧二进制不可与新邀请/重加混合运行，也不能在接受曾开放后直接回退旧DELETE。** 发布排空和停写步骤见迁移文档。公开部署前仍须追加账户/IP/Workspace网关限流、接收者跨租户频次/拒绝冷却、注册滥用控制、脱敏监控和历史容量/保留评审；100条配额不是完整防轰炸。本阶段不实现Redis、HTTP监控、告警或通知系统。

## Workspace 项目管理

先通过 `GET /api/v1/workspaces` 选择当前Workspace，再使用下列接口。Project继承当前Workspace权限：Owner创建/修改，Owner与Member读取；创建者退出、被移除或禁用后项目继续存在，创建者字段仅内部保留，不授予访问权。不增加项目成员/角色、跨Workspace汇总、slug、唯一名称、配额、创建幂等键、删除/归档/软删除/转移/status。

| 接口 | 请求 | 成功 |
| --- | --- | --- |
| POST /api/v1/workspaces/{workspace_id}/projects | name必填、description可省略 | 201、Location、ETag、data |
| GET /api/v1/workspaces/{workspace_id}/projects | limit默认50/1–100、after规范UUID | 200、data数组、pagination.next_after |
| GET /api/v1/workspaces/{workspace_id}/projects/{project_id} | 无query | 200、ETag、data |
| PATCH /api/v1/workspaces/{workspace_id}/projects/{project_id} | name/description至少一项、强If-Match | 200、新ETag、data |

POST/PATCH复用环境Cookie及Origin/X-DevPilot-CSRF:1同源校验。输入严格单JSON对象/字符串，拒绝重复/未知键、null、非法UTF-8/surrogate；只允许单application/json及可选UTF-8 charset，不允许Content-Encoding。项目body上限16384原始bytes，含转义和尾随空白；认证接口仍4096bytes。name沿用Workspace trim与Unicode规则（1–100码点/400bytes）；description最多2000码点/8000bytes，不trim，允许HT/LF、拒绝CR及其他Cc，空串清空。合法Unicode不规范化，名称允许重复。

data固定为id/workspace_id/name/description/version/created_at/updated_at；version为十进制字符串，时间UTC RFC3339Nano。强tag形如 `"project:<project_uuid>:1"`。修改前读取detail或保留创建/修改回执的tag，发送 `If-Match`；即使同值PATCH也递增version。缺tag428，旧版本412，不自动刷新tag/合并；弱tag、星号、多tag/重复或不规范header400。无条件GET/替代条件写协议，其他条件header被拒绝。错误无ETag/Location，所有Project响应禁止Set-Cookie。

查询SQL同时验证active用户、未撤销且未过期Session、当前membership和Project的Workspace归属。失效身份401，Member写403，非成员/不存在/错误Workspace与Project组合统一404 project_not_found。列表按id ASC、limit+1取候选，cursor仅下界，next_after仅有更多数据时提供；每页新快照，UUIDv7不是提交顺序，不能保证遍历涵盖所有并发晚提交。

所有Project可变字段的受支持写入必须通过 `internal/project.Store.Update`，保持授权和version递增；不得由其他业务模块直接UPDATE而破坏强ETag。稳定JSON表示无角色字段或压缩变体，改变投影/编码须重审validator。写事务顺序User→Session→Workspace→Membership→Project，所有保护等待后新语句重验Session时限及Owner；POST/PATCH确认Commit后才成功。请求5s，整个DB操作2s。未知Commit不代表回滚，不自动重试/补偿；POST手动重复可创建第二个同名项目，名称不能可靠恢复一次未知请求；PATCH持原tag重试可能412，需重新读取并决定。

Migration5由显式受控发布步骤执行，项目固定Workspace归属由约束与不可变字段trigger保护。未来子资源必须用(workspace_id,project_id)复合FK，不能仅靠已知Project UUID授权。无删除/配额意味着误建与存储增长需后续产品评审，公开部署的既有限流与容量要求不因本功能而豁免。

## HTTP Monitor 配置（Migration 6）

Project 下新增 `POST/GET /api/v1/workspaces/{workspace_id}/projects/{project_id}/monitors` 和 `GET/PATCH .../monitors/{monitor_id}`。当前 Owner 创建/修改，Owner/Member 读取；创建者退出不删除配置，也不保留访问权。完整 SQL 同时验证可信 Cookie Session、当前成员及所有父子归属。列表默认50、limit1–100，after为小写规范UUID，按id升序，游标不授予权限。

POST必须提供name/url；可选method仅`GET`，interval_seconds允许60/300/900/1800/3600（默认60），timeout_seconds为1–30且小于间隔（默认10），expected_status为单个200–599（默认200）。名称沿用Project的1–100码点/400字节及Unicode规则。只接受严格JSON对象，最大16384原始字节；未知/重复字段、null、非法Unicode、尾随JSON、非规范整数token拒绝。PATCH至少一个字段，缺省保留现值；每次成功包括同值修改均递增version。完整协议与固定消息见 `docs/api/openapi.yaml`。

URL只允许ASCII HTTP(S) DNS域名及对应默认端口；不接受IP（包括公网/IPv6/mapped/NAT64）、userinfo、query、fragment、非标准端口、自定义Header/Host/代理/重定向配置。原始Unicode域名拒绝，可用严格校验的punycode A-label；scheme/host小写，去掉单个尾随点和显式默认端口，空路径补`/`。保留escaped path字节语义及点段/双斜线，只把百分号hex改大写；URL原始及规范结果均最多2048字节。特殊域名/路径控制字符/不规范数字IP等拒绝。IDNA固定`x/net v0.49.0`（Go1.24.0基线），工具链仍Go1.24.3。

POST201返回Location和强ETag `"monitor:<id>:<version>"`；详情和PATCH200返回ETag。PATCH必须带唯一强If-Match：语法错误400，实际身份/范围/Owner检查后缺少条件428、过期412。Monitor更新不改变Project版本。版本是正bigint十进制字符串。相同名称/URL允许重复，POST无幂等保证；未知Commit/响应丢失可能已提交，不自动重试、补偿或刷新旧tag。

单项成功返回URL；列表省略URL，所有错误和应用日志不包含URL任何部分、输入body或凭据。path也可能包含令牌，此功能不是秘密保险箱。Cookie-only、同源CSRF、5秒请求/2秒整事务、no-store/nosniff及无Set-Cookie协议保持不变。

**所有操作仅保存/读取配置，不解析目标DNS或连接目标，不代表监控正在运行。** 无enabled/paused/desired_state、自动激活、删除、归档、Worker、结果或通知。未来引擎须另行审核存量激活，并独立验收DNS/IP分类、重绑定/TOCTOU、绑定已验证IP拨号、原域名TLS证书/SNI、禁不受控代理/跳转和出口限制；本轮纯URL单测不能证明执行期SSRF安全。

零目标网络验收的可复验Linux脚本在 `tests/network/run-monitor-audit.sh`：先运行隔离违规连接正向对照，要求审计返回失败，再运行真实配置端点，要求零未批准尝试。仅精确允许测试PG及本地HTTP地址；连接失败/超时仍算尝试，不能当作零网络。详见脚本注释与Change的verification记录。

### HTTP Monitor activation intent

`GET/PUT /api/v1/workspaces/{wid}/projects/{pid}/monitors/{mid}/activation`
manage **activation intent only**. Current Owner/Member can read; only the current
Workspace Owner can write. Cookie-only authentication and exact same-origin CSRF
are required for writes. A successful `enabled` response does **not** mean the
monitor is running: `execution_state` is always `blocked`, `blocked_reason` is
`scheduler_unavailable`, and `next_due_at` is always null. There is no production
probe, scheduler, monitor Worker, queue, run/result store or execution unlock flag.

Read the activation resource first, then PUT exactly two strings, for example
`{"desired_state":"enabled","configuration_version":"7"}`, with its strong
`If-Match: "monitor-activation:<monitor_id>:7:<generation>"`. Pause uses
`desired_state:"disabled"` and also confirms the current version/tag. The body is
at most1024 raw bytes. Each successful command, including a same-value command,
consumes one activation generation. Configuration ETags remain `monitor:<id>:<v>`.
Every successful configuration PATCH, even a name or same-value PATCH, atomically
revokes activation and advances its generation; callers must explicitly reconfirm.

A Workspace can hold at most100 enabled intents (409 `activation_limit_reached`).
Successful enable imposes persisted60-second cooling (429
`activation_rate_limited`, `Retry-After` seconds). Pause and configuration PATCH
remain available during cooling and at capacity. Cooling survives pause, config
changes and restart. Invalid identity401, invisible/mixed scope404, Member write403,
missing precondition428 and stale version/generation412 retain that priority after
syntax and CSRF checks. See `docs/api/openapi.yaml` for the full transport contract.

Only confirmed Commit returns success. An unknown outcome may already have
persisted: GET to reconcile; there is no automatic retry or compensation. Intent
belongs to the Workspace, not the author's Session. A former Owner immediately
loses access according to current membership. Only the last transition's safe
reason/actor/time is retained internally, not a complete audit history; actor and
URL are absent from the activation response. Before any future approved Worker
rollout, these intents must be cleared with generation advancement and the current
Owner must confirm again. Production egress has not been approved by this feature.

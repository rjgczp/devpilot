# Design

## Context

动机和范围见 `proposal.md`；行为契约见两份 delta specs。本设计已按用户有条件通过的审核意见修订，最后规格调整已完成并通过严格校验，按用户授权进入Apply。整体技术范围保持 Go 原生 HTTP、pgx、PostgreSQL 18、Goose 和 Argon2id；实施完成后创建PR等待独立审查，不自动合并或归档。

本地 `main` 已通过 `git pull --ff-only origin main` 同步，基线为 `14922d2`，同步前工作区干净。已阅读两份主 Specs、已归档 bootstrap 设计、README、Compose、CI、入口与相关配置、健康检查和运行测试。

当前只有 Go 1.24.3 单模块、`net/http`、`log/slog` 和 pgx/v5 v5.8.0；`cmd/api/main.go` 创建一个共享 pgxpool 并把健康 handler 交给 `app.Runner`。数据库不可用不阻止 HTTP 启动。`/readyz` 仅 Ping，现有 5s/10s/10s/60s HTTP 超时与 10s 总退出预算已有规格和测试。尚无 users、迁移、业务错误响应或 API 文档。CI 运行 gofmt、vet、普通测试及 race，尚无 PostgreSQL 服务。

## Goals / Non-Goals

**Goals:** 形成单端点、单业务表的纵向切片；数据库负责并发完整性，应用负责输入与安全边界；无需数据库也能跑单元测试，真实 PostgreSQL 验证数据库行为；迁移作为显式运维步骤。

**Non-Goals:** 不引入通用认证框架、ORM、sqlc 生成流水线或通用事件机制；不把健康检查升级成 schema 探针；不实现邮箱验证、账户管理和密码验证接口。用户明确排除的业务均保持排除。仅在测试内部重算哈希，验证存储正确性，不增加登录功能。

## Decisions

### 1. UUID：数据库生成 UUIDv7

推荐 `id uuid PRIMARY KEY DEFAULT uuidv7()`。PostgreSQL 18 已原生提供该函数，无扩展或 Go UUID 依赖；时间有序的键有利于后续索引局部性，在单次 INSERT RETURNING 中即可取回。应用从不接受客户端 ID。

备选：数据库 `gen_random_uuid()` 的 UUIDv4 更少暴露生成时间且兼容旧 PG；Go 生成 v4/v7 可在落库前获得 ID、适合离线或跨服务协作，但当前没有这种需要。v7 会携带近似生成时间，不能充当秘密或授权凭证；本项目已明确 PG18 基线，接受这项绑定。重复主键仍是内部故障，不能当邮箱冲突。

依据：[PostgreSQL 18 UUID functions](https://www.postgresql.org/docs/18/functions-uuid.html)。索引收益是基于有序键的设计判断，本轮未做性能测量。

### 2. 邮箱：ASCII 子集、全地址小写、单列唯一约束

推荐 `email text COLLATE "C" NOT NULL`，应用先移除首尾 ASCII SP/HT/CR/LF，再把 ASCII 全地址小写。仅接受 ASCII dot-atom local part（字母、数字与 `!#$%&'*+-/=?^_`、反引号、`{|}~`，点不能首尾/连续），local part 最长 64 bytes；域名至少两个标签，各标签 1–63 bytes，仅字母、数字、内部连字符，不接受 IP literal、末尾点、显示名或引号地址；规范值总长不超过 254 bytes。不做 DNS/MX 查询、投递检查、Gmail 去点或移除 `+tag`。`alice+tag` 与 `alice` 是不同身份。

数据库保底 CHECK：非空、ASCII、最多 254 bytes、`email = lower(email COLLATE "C")`、首尾不含 ASCII SP/HT/CR/LF；具名 `CONSTRAINT users_email_key UNIQUE(email)`。数据库检查不复制完整邮箱解析器，语法的完整校验由注册应用承担。C collation 使 ASCII 大小写及唯一比较不依赖部署 locale。绕过应用的非规范值被拒绝；并发以唯一约束为最终裁决。

备选：`citext` 隐式忽略大小写，但需要扩展且涉及 locale；`UNIQUE INDEX lower(email)` 可以保留原始大小写，却增加双表示及表达式索引错误分类工作；单独 `email_normalized` 适合以后展示原样邮箱，但现在无需求。国际化邮箱/IDNA 需要单独定义 Unicode 归一化与兼容迁移，不在本次隐式支持。

ASCII 子集与全地址小写是当前 MVP 产品兼容性策略，并非宣称所有邮件系统都忽略 local part 大小写，或其他格式一概无效。国际化 local part、quoted local part 等地址可能在其他邮件系统合法，但当前不支持；全小写可能合并理论上不同的邮箱。文档和422说明只能描述“当前策略不支持”，不能推广为通用邮箱有效性结论。数据库唯一约束始终是并发一致性的最终保证。参考：[PostgreSQL citext 的 locale 和比较说明](https://www.postgresql.org/docs/18/citext.html)。

### 3. Argon2id：明确成本、随机盐和有界并发

使用 `golang.org/x/crypto/argon2.IDKey`，本轮暂定安全参数为 `m=65536 KiB (64 MiB), t=3, p=1, salt=16 bytes, key=32 bytes, v=19`，不得为追求性能指标擅自降低。`crypto/rand` 每次产生新盐。只保存 `$argon2id$v=19$m=65536,t=3,p=1$<salt>$<hash>` PHC 字符串，Base64 无 padding；该编码保存未来验证/升级所需参数。密码模块及其测试检查完整编码、参数、盐和输出长度；数据库不校验 PHC 格式或具体参数。本次没有历史哈希升级路径，也不引入 pepper 与密钥管理。

这是高于 OWASP 最低配置的项目起始值；p=1 选择较低单请求 CPU 并行度，**不是照搬 RFC 的 p=4 配置，也不是声称已经调优**。[OWASP Password Storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html) 给出最低 m=19 MiB、t=2、p=1；[Go argon2 文档](https://pkg.go.dev/golang.org/x/crypto/argon2) 说明 KiB 单位和基于硬件调节。

先执行 method/media、有界4096-byte body、JSON结构、邮箱/密码政策等廉价校验，再检查 context 并尝试获取哈希槽；非法请求不占槽、不调用随机源/Argon2id或存储。每进程最多2个槽，无等待队列；有效请求在饱和时立即503 `registration_busy`，`Retry-After: 1`。两份 Argon 工作内存约128 MiB，但进程还包括 Go GC、HTTP、连接池等开销，不能把128 MiB宣传为进程内存硬上限。

槽位从获取到该次密码处理实际返回为止保持占用，使用 defer 覆盖成功、随机源/哈希错误和异常退出路径；正常情况下哈希返回后释放，再进行数据库操作。客户端取消或5s deadline本身不能提前释放正在计算的槽位。`IDKey` 不支持 context 中断，因此同步执行，不按请求另起后台goroutine逃离预算；哈希前后检查context，取消后不再发起INSERT。真实哈希运行中只能等待返回，不能声称请求取消会立即释放CPU/内存；GC/RSS回落也不保证与槽位释放同步。

Apply 时以真实参数记录机器/CPU/内存、Go版本、样本数量、并发1和2的p50/p95、allocations及进程峰值内存，依据该基线建立可复现的性能目标；本轮不宣称已达到任何时延/内存指标，也不以未经实测的500ms作为承诺。5s处理和2s数据库context是暂定控制预算，并非实测SLO或可中断哈希的保证。结果超出预算时先评审资源容量、预算与实现，任何安全参数降低都必须另获明确审核。单元handler测试用可控hasher观察占用计数与取消，少量真实密码/集成测试及资源基准使用真实成本。

备选：19 MiB/t=2/p=1成本低；64 MiB/t=3/p=4接近RFC第二档但单请求并行CPU较高；bcrypt存在输入长度约束。它们是比较方案，不是允许实施时自行降级的选项。两个槽只约束本进程的同时哈希数量，不能限制持续请求、自动批量注册或跨实例攻击，**不是完整反滥用限流**。公开部署前必须另行实现并评审注册限流与防滥用控制，包括适合部署的请求速率/突发限制、跨实例策略和可验证的滥用拦截；这一发布前置条件写入开发文档和验收清单。本Change只记录，不新增Redis、CAPTCHA、邮件挑战或限流系统。

### 4. 数据模型与初始账户状态

| 字段 | 数据库定义/约束 | 说明 |
| --- | --- | --- |
| id | uuid PK DEFAULT uuidv7() | 不由请求指定 |
| email | text COLLATE "C" NOT NULL + CHECK + users_email_key | 唯一规范身份，无原始邮箱副本 |
| password_hash | text NOT NULL，CHECK octet_length 在1–1024之间 | 只约束非空/合理存储长度，不绑定算法、PHC格式或具体参数 |
| status | text NOT NULL DEFAULT 'active', CHECK IN ('active','disabled') | active 仅代表账户未被禁用 |
| email_verified_at | timestamptz NULL | 新注册固定 NULL |
| created_at | timestamptz NOT NULL DEFAULT now() | 数据库时间 |
| updated_at | timestamptz NOT NULL DEFAULT now() | 创建时与 created_at 相同 |

使用 text+CHECK 比 PostgreSQL enum 更便于未来以显式迁移扩展；本次不添加状态变更 API 或 timestamp 更新触发器。唯一索引已由 PK/UNIQUE 生成，不加重复索引。

已接受新注册为 `active`，并明确 `email_verified_at=NULL`。active只表示账户没有被禁用，**不表示邮箱所有权已验证，也不表示已认证**。后续功能不得仅凭active信任邮箱：需要邮箱所有权的行为必须依赖独立建立的验证状态，null必须当作未验证，不能根据status推导或填充验证时间。对应Spec覆盖新建active/null、客户端注入验证字段被拒绝，以及未来消费者不能把active当验证证据的场景。本次不实现消费者功能或验证端点。重复请求不激活disabled用户、不改密码/验证状态。密码模块保证写入PHC；数据库不承担检测非空明文或伪PHC的职责，模块测试必须发现错误编码/参数与明文持久化回归，后续参数升级只需符合存储上限而不触发参数专属schema迁移。

### 5. Migration：直接复用 Goose 原生机制

推荐 `pressly/goose/v3` 的 SQL migration（`migrations/00001_create_users.sql`，Up/Down，同一文件），使用默认事务，`goose_db_version` 记录版本；已应用迁移不可原地修改，修正通过后续版本完成。仅创建这一个业务表，不创建额外认证表。

使用固定版本的原生 Goose CLI 读取仓库 `migrations/` SQL 文件，直接复用Up/Down标记、事务和版本表；不增加 `cmd/migrate`、自定义Provider入口、嵌入式迁移包、锁引擎或专属确认参数。集成测试调用同一版本CLI及同一目录脚本。业务API仍只用pgxpool，与迁移工具安装/执行解耦。

命令约定（Apply时固定工具版本并验证，当前不执行）：

```sh
export GOOSE_DRIVER=postgres
export GOOSE_DBSTRING="$DATABASE_URL"
goose -env=none -dir ./migrations -timeout 30s status
goose -env=none -dir ./migrations -timeout 30s up
```

显式把已导出的DATABASE_URL传入GOOSE_DBSTRING，不把DSN作为命令参数、不启用verbose或env配置打印；使用 `-env=none` 避免Goose默认加载.env。`-timeout 30s`沿用工具对查询context的原生语义，不承诺自定义30s进程总预算或10s锁获取。共享环境的迁移由单一受控发布步骤串行执行，再部署API；不假定未配置的CLI自带并发迁移锁。该限制通过部署步骤/操作文档保障，不为本切片搭建部署平台。

一次本地Down只用于操作人员明确确认可丢弃的开发/测试库，运行同一CLI的 `down` 子命令；会删除users及其数据。共享/生产日常发布仅允许status/up，禁止自动down/down-to/redo/reset；异常破坏性恢复另需授权、已验证备份和恢复计划评审。原生CLI不自动识别环境，也没有本项目自造的确认标志；不声称工具能替代发布权限和操作流程。

原生Goose失败可能包含SQL及驱动诊断，作为受限运维输出处理；本项目不为迁移CLI二次封装通用脱敏。不得将原始诊断复制到HTTP、公开日志、文档或工单，分享前只保留脱敏摘要；凭据通过环境/受限输出保护，API自身仍严格禁止原始PgError。测试库和CI仅用可丢弃凭据。

备选golang-migrate同样成熟，但本轮保留Goose；自定义Go入口仅在后续出现原生工具不能满足的具体需求时独立评审。API启动自动迁移会引入DDL权限、启动依赖和并发风险，明确禁止。部署先up后启用注册路由，不能用“API能启动”代替schema已经部署的证明。

参考：[Goose原生CLI、环境和事务说明](https://github.com/pressly/goose)、[原生命令](https://pressly.github.io/goose/documentation/cli-commands/)。Apply时在开发/CI安装说明中固定受维护且兼容Go基线的Goose CLI版本，将x/crypto版本固定到go.mod/go.sum；不使用 `@latest` 或隐式升级Go。若没有合适安全版本，先提出最小工具链修订；不要求应用二进制依赖Goose运行库。

### 6. 查询与分层：原生 pgx，暂不引入 sqlc

推荐以下最小边界，避免通用仓储框架：

- `internal/registration`：请求解析/验证、HTTP DTO、服务协调与错误映射；小型 Store/Hasher 接口支持故障注入。
- `internal/password`：真实 Argon2id 与盐生成；错误不携带密码。
- `internal/user`：pgx repository；一条参数化 `INSERT ... RETURNING id,email,status,created_at`，不 `SELECT *`，不使用 `ON CONFLICT DO UPDATE`。
- `cmd/api`：复用同一连接池，根 mux 挂载注册 path handler 和健康 routes；健康端点的文本响应保持原状。业务 JSON 响应约定仅用于本次注册路径，不顺带重构全站 404/405。

不先 SELECT 判断邮箱存在：这不能替代唯一约束，增加竞态和往返。校验后，无论邮箱是否已存在，都先做真实哈希再 INSERT；这避免提前查重形成额外明显成本差异，但 409 仍明确暴露占用情况。

`errors.As`只在内部识别 `*pgconn.PgError`，使用结构化分类，外部响应永不暴露该对象、SQLSTATE、Detail或ConstraintName。先区分请求取消、数据库context期限，再识别具名唯一/缺表/连接等错误；不靠公开返回的原始错误文本分类：

| 内部条件 | HTTP / code | 说明 |
| --- | --- | --- |
| 23505且ConstraintName=users_email_key | 409 email_conflict | 仅这一个约束是邮箱业务冲突 |
| 连接不可达、连接中断或获取连接失败（非超时） | 503 service_unavailable | 使用安全db_unavailable类别 |
| 数据库子context DeadlineExceeded/连接获取超时，或有明确证据的服务端statement timeout | 503 database_timeout | 使用安全db_timeout类别，不承诺零提交 |
| 42P01且确认是注册所需users缺失 | 500 internal_error | 内部部署错误，日志仅schema_missing |
| 其他约束、schema/SQL缺陷或未知错误 | 500 internal_error | 不误报邮箱冲突 |

SQLSTATE 57014仅表示query_canceled，不能孤立地当作超时；必须结合请求期限或PostgreSQL已知的固定statement-timeout Message作为内部证据，未知/本地化的取消诊断保守按内部错误处理，不通过暴露或解析Detail猜测。真实PG18测试验证固定statement-timeout诊断映射。客户端断连或主动取消（原始request context已结束）与服务端预算超时分开处理：取消后不再写HTTP状态/body，不保证返回503；数据库操作继承取消，已发出的INSERT结果可能仍不确定。原始request仍有效而服务端数据库子期限到达时才尝试503 database_timeout。处理预算在数据库前过期时使用503 service_unavailable，均不重新发起INSERT。单INSERT自带原子性，无额外事务或服务器自动重试。已提交但未确认的操作可以返回503，状态码不意味着数据库一定未创建账户。

备选 sqlc 能生成类型安全的 pgx 查询代码，适合查询数量增加及稳定 schema 的阶段；现在一条 INSERT 无法抵消生成器、配置与 CI 同步成本。保留 repository 边界，后续可在不改 API/specs 的情况下切换。[sqlc 的 pgx 支持](https://docs.sqlc.dev/en/latest/guides/using-go-and-pgx.html)、[pgconn 错误模型](https://pkg.go.dev/github.com/jackc/pgx/v5/pgconn#PgError)。

### 7. 请求、响应与错误约定

请求只有 `email` 和 `password`；验证细则见 registration spec。密码推荐最少 15 个 Unicode code points（当前无 MFA）、最多 128 个且不超过 512 UTF-8 bytes，不 trim/normalize/truncate，不强制大小写数字符号组合，不调用外部密码泄露检查。15 字符下限参考 [OWASP Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)；上限是本项目的有界输入策略。

解析最多 4096 bytes（包括尾随内容），先验证原始UTF-8和JSON字符串中的每个Unicode转义：四位十六进制、high surrogate紧跟low surrogate、拒绝孤立/反序/缺损代理项；不得只检查解码后的utf8.ValidString，因为encoding/json会替换非法代理项。合法U+FFFD不能被误拒绝，合法代理对按一个码点计数；密码禁止裁剪/截断/Unicode归一化。再拒绝重复键、未知键、缺失字段、null、非对象和多值；不能仅用 `DisallowUnknownFields` 后忽略重复键或第二个值。允许 `application/json` 或带 UTF-8 charset，拒绝其他媒体参数/编码；先验证 method，再媒体类型，再有界 body，再字段，再哈希/插入。固定注册路径支持所有 method 的入口以统一输出 405（Allow: POST）；HEAD 只发状态与头。

成功体：`{"data":{"id":"<uuid>","email":"alice@example.com","status":"active","created_at":"<UTC RFC3339>"}}`。

错误体：`{"error":{"code":"validation_failed","message":"Invalid registration fields.","fields":{"password":"invalid_length"}}}`。`fields` 仅用于 422，只含 email/password 字段名；空字符串（邮箱按规范化后判断）使用 `required`，其后按长度 `invalid_length`、语法 `invalid_format` 的优先级为每个字段返回一个原因码。JSON 字段结构缺失属于 400，无 fields。固定头 `Content-Type: application/json`、`Cache-Control: no-store`、`X-Content-Type-Options: nosniff`；不设置 Cookie、Location 或令牌。不输出 password_hash、email_verified_at、updated_at。

| HTTP | error.code | 固定 message | 行为 |
| --- | --- | --- | --- |
| 201 | 无 | 无 | 只有确认持久化后返回 data |
| 400 | invalid_request | Invalid registration request. | JSON/字段结构不合约 |
| 405 | method_not_allowed | Method not allowed. | Allow: POST |
| 413 | request_too_large | Request body too large. | 不执行哈希 |
| 415 | unsupported_media_type | Unsupported media type. | 不执行哈希 |
| 422 | validation_failed | Invalid registration fields. | 固定字段原因码，不回显输入 |
| 409 | email_conflict | Unable to register with this email. | 不返回已有用户信息 |
| 503 | registration_busy | Registration is temporarily busy. | Retry-After: 1 |
| 503 | service_unavailable | Registration could not be confirmed. | 连接不可用/中断，或DB前处理期限到达；不承诺未提交 |
| 503 | database_timeout | Registration could not be confirmed. | DB期限/确认的statement timeout；不承诺未提交 |
| 500 | internal_error | Unable to complete registration. | 缺users等部署错误、密码模块/其他内部缺陷；不返回底层详情 |

推荐 409 是明确的易用性优先取舍，**不是防账号枚举方案**。备选为新建/重复均返回相同 202/通用文本且不带用户数据，需让客户端接受无法确认创建的语义；若后续同时引入邮件验证，可配合邮件形成更完整的隐私流程。本轮没有邮件/验证，不采用虚假的“邮件已发送”或同样 201 却未创建账户的语义。无论选择哪个方案，都不能承诺仅靠耗时填充实现恒时网络 API。409 的风险和状态码稳定性必须作为审核重点；以后改变此行为需要显式 API 契约更新。

### 8. 超时、错误安全与现有生命周期

处理阶段从进入注册 handler 起设置 5s request-derived context，数据库子预算最多 2s 且不得超出总截止时间；读取 body 仍受现有 HTTP ReadTimeout 约束，5s context 不能保证中断正在进行的 body Read 或 Argon 调用。保留既有服务器超时；哈希后检查 context，未取消再开始 DB。数据库写结果超时/连接丢失可能已经提交，返回安全错误，不自动重试或回滚猜测。

新代码的 slog 只记录固定事件、原因类别、HTTP 状态和耗时，不记录请求体、邮箱、密码、PHC、盐、DSN、SQL、原始驱动错误/stack。验证错误不逐次输出敏感输入。可预期错误统一映射；注册 path 的边界恢复未写响应前的 panic，仅记固定类别并返回 500，不输出 panic 值。已写响应后的异常关闭连接，不拼接第二个 JSON。Go 内存中密码仅按需短暂存在，不能承诺被 GC 语言彻底清零。

沿用Runner的关闭预算和request context，不让信号context立即取消已接收注册；在预算内完成或由服务器强制关闭。健康响应不改JSON envelope，/readyz不检查users：缺迁移时可能ready=200但注册=500 internal_error，部署顺序和集成测试负责发现遗漏。API进程不持有DDL权限是部署建议，具体生产角色配置留到部署工作。

### 9. 可执行验收与文档

单元测试覆盖两个 Specs 的所有输入边界/错误码、取消与容量释放、具名约束错误映射、日志/响应 sentinel 脱敏；小接口注入 repository/hasher，真实密码测试验证 PHC 参数、随机盐、不同密码不匹配及随机源失败。安全错误判断断言稳定字段，避免测试复制实现。

集成测试以 `integration` build tag 隔离，`go test -tags=integration ./... -count=1` 必须有 `TEST_DATABASE_URL`；无 URL 或连不上就失败，不跳过。测试 URL 必须不同于 `DATABASE_URL` 且库名严格为 `devpilot_registration_test`，连接后通过 `current_database()` 再确认；同一个 DB 不得以换用户/端口字符串绕过库名保护。CI 创建仅测试用的该库，不并行运行共享 schema 的 suite。所有 drop/down/cleanup 只限该库，测试不得回退到应用 DSN。不要读取或打印开发 `.env`。

真实PG18使用固定版本Goose CLI和同一SQL目录完成up/up、up/down/up、坏SQL事务回滚、直接非法SQL、主键v7、时间戳、真实PHC、并发邮箱201/409及旧凭据不变。password_hash数据库测试仅断言NOT NULL/1–1024 bytes且不存在参数/格式CHECK，模块测试断言完整PHC与具体暂定参数；参数升级后的有效编码不被schema拒绝。HTTP测试用真实root router：连接失败503 service_unavailable、缺表500 internal_error、可控锁等待触发DB子期限的503 database_timeout分别验证。提交不确定用fault fake验证不自动重试/不保证失败，不伪称普通断网能证明零提交。

新增占用计数、两次计算中途取消仍饱和、实际返回后可复用、异常释放和廉价拒绝不占槽测试；真实参数资源基准记录总内存而非只统计哈希缓冲区。active/null测试和文档核对证明没有邮箱已验证的推导；未来信任消费者的验收约束保留在Spec，不为测试新增认证业务。unit/race、现有进程/健康行为均回归。CI独立PG18 job显式运行integration tag，不以默认测试代替集成；公开部署前置条件通过发布文档/评审核对，不在此实现限流。

Apply时新增 `docs/api/openapi.yaml`（OpenAPI 3.1）和 `docs/database-migrations.md`，更新README：覆盖完整错误表及未知提交、ASCII兼容限制、密码code points/bytes双限、409枚举取舍、active不能证明邮箱所有权、真实槽位释放/非反滥用说明、实测基准、公开部署前必须实现并评审的注册限流与防滥用控制、原生Goose/受限Down/串行迁移和测试库准备。OpenAPI字符数不能完整表达UTF-8 byte上限及精确邮箱子集，需文档和合约测试补足，不依赖通用email format代替产品规则。

## Risks / Trade-offs

- [409 暴露邮箱占用] → 明确记录并请审核接受；不能通过不回显邮箱就宣称防枚举，后续可独立改为通用接受响应。
- [active且未验证可能被后续功能误用] → active只表示未禁用，后续所有邮箱信任决策必须依赖独立验证证据，Spec和文档明确禁止仅凭active推导可信邮箱。
- [哈希消耗内存/CPU、不能立即取消] → 廉价校验先行、2槽无队列、实际计算返回才释放、真实参数资源基准；不得为性能目标擅自降参。公开部署前必须另行实现注册限流与防滥用控制，两个槽不能替代这一前置条件。
- [ASCII/全小写策略排除 EAI 邮箱并可能合并区分大小写 local part] → 写入 API 契约，国际化/保留原文需独立迁移设计。
- [原生迁移CLI可以执行破坏性Down且不自动识别环境] → disposable库才允许日常Down；共享/生产日常发布只用status/up，例外恢复单独授权、验证备份并审阅恢复计划，不虚构CLI保护开关。
- [并行发布同时迁移] → 共享数据库只由单一发布步骤串行执行Goose；不自行扩展锁机制或假定原生CLI默认锁定。
- [数据库不校验PHC格式] → 密码模块与测试承担完整性；数据库保留NOT NULL和1–1024-byte上限，允许审核后的参数升级，不声称数据库能检测非空明文。
- [数据库错误包含邮箱/凭据] → 按 SQLSTATE/具名约束分类，响应和日志只使用安全投影并以 sentinel 测试。
- [PG18 UUIDv7 及依赖兼容性] → 在真实 PG18 CI 运行；固定兼容 Go 基线的库版本，不隐式升级工具链。
- [客户端重试时第一次已提交] → 唯一约束保护、不覆盖旧密码，连接失败/DB超时只说无法确认结果，错误码不保证未提交；客户端取消不写503，不增加幂等键协议。

## Migration Plan

1. 审核并确认本设计；仅收到新的 Apply 指令后实施，先在独立测试库验证 migration 和测试。
2. 部署时先备份目标库（有数据时），由单一迁移步骤用固定版本原生Goose执行status/up/status，再部署注册API。禁止API启动自动DDL；共享/生产日常发布不运行down/down-to/redo/reset。
3. 验证201/409、连接不可用503、缺表500、DB超时503与既有health/readiness；公开暴露前另须完成注册限流与防滥用控制及部署评审。此Change完成不等于已获准公开部署，也不搭建部署平台。
4. 应用回滚保留新增表和数据，schema修复优先前向migration。只有明确确认可丢弃的库才常规执行单步down再up；共享/生产破坏性例外需要单独授权、验证备份和恢复计划审核，不提供无损回退承诺。

## Review Confirmations

用户已有条件接受范围及active/null、ASCII归一化、暂定Argon2id参数/2槽，修订现已将附加条件落实到Specs和任务。用户已明确授权：完成原始Unicode/取消语义修订并通过严格校验后进入Apply。

最终审核对照：

- HTTP状态/code以第7节表为准：特别是缺users=500、DB超时=503 database_timeout、连接故障=503 service_unavailable；未知提交不承诺失败。
- 密码15–128个Unicode code points且最多512 UTF-8 bytes；JSON总body最多4096 bytes，所有廉价校验在哈希前。
- 重复邮箱保持409，大小写/首尾ASCII空白归一化后以DB具名唯一约束裁决，现有凭据/状态不变；明确仍存在枚举取舍。
- Goose使用原生CLI，常规共享/生产发布仅status/up，发布串行；PHC完整性归密码模块，DB仅非空/长度。

最后修订通过一致性和严格校验后即按授权Apply；完成后创建PR等待独立审查，不自动合并或归档。注册限流与防滥用控制留作独立后续工作，但它们是公开部署前不可省略的前置条件。

无需阻塞本轮规划的实现细节：合适库版本的具体 patch、基准机器的硬件信息和 CI PG18 固定镜像 digest，均在 Apply 中验证并记录，不改变本轮需求或工程路径。

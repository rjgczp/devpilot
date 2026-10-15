# Design

## Context

2026-10-14（Asia/Shanghai）核验：开始工作时工作区干净；成功 fetch 后 `main` 与 `origin/main` 同为 `a38472dabd8e06e6f9a030e759fa5d5dfb652edc`。PR #1 在 `7437101` 合并，bootstrap 在 `14922d2` 归档；PR #2 在 `1ac1eab` 合并，registration 在 `a38472d` 归档。两份历史任务表无未完成项，四份主 Specs 已存在；registration verification 记录 CI 和同步归档结果。历史 bootstrap verification 的“未归档”描述是当时验收快照，不代表当前状态。

已读取源码、测试、四份主规格、历史任务/设计/验收记录、配置及 API/迁移文档。现有 Go 1.24.3、pgx v5.8.0、x/crypto v0.48.0；原生 Goose v3.26.0 和 PG18 集成环境继续复用。`internal/password` 只有 Hash；`registration` 自己拥有两个槽；`user.Store` 只有 Create；`app.Runner.Workers` 已支持受管理任务。增加 migration 后，旧集成夹具的单步 Down/版本 2 假设会失效，必须一起修正。

动机/排除范围见proposal。2026-10-14，用户正式批准Apply并明确保留本轮清理Worker；第11节的拆分建议作为评审取舍保留，不改变已批准实施范围。实际实现/测试结果见verification.md，不以设计文本冒充证据。

## Goals / Non-Goals

**Goals:** 数据库决定会话有效性；浏览器只保存随机 bearer secret；密码验证、数据库与后台清理均有明确资源预算和故障行为；三个新端点构成可在真实 PG 上验收的纵向切片。

**Non-Goals:** 不引入会话缓存、通用认证框架、跨站前端协议、滑动续期、Refresh Token或密码自动升级；不增加账户管理接口。公开限流属于独立发布前置交付。Workspace 的授权决策不在本轮执行。

## Decisions

### 1. Token：32-byte CSPRNG，完整 SHA-256 摘要

每次成功登录使用 `crypto/rand` 读取 32 bytes，形成 256-bit 随机值；Cookie 编码为 unpadded Base64URL，恰好 43 ASCII 字符。验证先检查长度、字符集合、严格解码及重编码一致性，再对**解码后的 32 bytes**计算 SHA-256。数据库只存 32-byte `bytea` 摘要，以参数化等值查询查找；整个摘要都是索引键，没有截断或前缀匹配。客户端发送数据库摘要不能直接认证，因为服务器还会解码并重新哈希客户端值。

无需对高熵随机令牌运行 Argon2：密码抗离线猜测与随机 bearer secret 是不同问题。摘要查询依赖 PG 等值匹配；不宣称 SQL/网络整体恒时。若后续采用 public ID + secret verifier 的双段格式，应用比较 secret 摘要必须 constant-time。本轮单随机令牌 + 完整摘要查询足够，避免引入双段协议和密钥轮换。HMAC/pepper 可进一步隔离密钥，但当前随机空间已足够，增加密钥管理不值得。Token RNG 失败返回 500，不插入、不签发 Cookie；摘要唯一冲突保守返回 500，不自动重试。

仅接受当前环境下恰好一个同名 Cookie；重复同名（即便值相同）、损坏或非规范值均视为无有效认证。不接受 query、JSON、Authorization 或用户自选令牌。原始 token、摘要、Cookie/Set-Cookie、密码、PHC、邮箱、完整请求头/体不能进入日志；token 只在成功 Set-Cookie 中出现，不进入 JSON。

安全思路参考 [OWASP Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)；具体长度、单摘要索引和本轮取舍是项目设计决定。

### 2. 表结构、24h 绝对过期与有界清理

新增 `00002_create_sessions.sql`，不改 `00001_create_users.sql`：

| 字段/索引 | 推荐定义 | 用途 |
| --- | --- | --- |
| id | uuid PK DEFAULT uuidv7() | 内部行身份，不是认证秘密 |
| user_id | uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE | 依附用户身份 |
| token_hash | bytea NOT NULL UNIQUE，octet_length=32 | 完整 SHA-256 摘要 |
| created_at | timestamptz NOT NULL DEFAULT statement_timestamp() | 数据库时间 |
| expires_at | timestamptz NOT NULL，CHECK expires_at>created_at | 创建时数据库时间 + SESSION_TTL |
| revoked_at | timestamptz NULL，非空时 CHECK revoked_at>=created_at | 幂等撤销记录 |
| user_id / expires_at | 分别建立普通索引 | 用户关联与过期清理 |
| revoked_at | 非空部分索引 | 撤销记录清理 |

`SESSION_TTL` 默认 `24h`，可配置范围 `5m`–`24h`（含边界）。本轮只有绝对有效期，任何读取都不刷新时间、不写 last_seen；默认 24h 是 MVP 体验/风险取舍，不宣称满足高敏感业务的最佳实践，后续敏感功能需独立引入重新认证和闲置期限。数据库 `expires_at > statement_timestamp()` 且 `revoked_at IS NULL`，并 JOIN `users.status='active'` 才有效。Cookie 期限仅帮助浏览器，服务器始终检查时间。事务起始时间可能陈旧，认证不使用 `now()` 判断过期。

通过现有 Runner 管理一个清理 worker：每 15m 尝试一个最多 1000 行的批次，单次 2s 子期限；删除已经过期或 `revoked_at <= statement_timestamp()` 的记录。使用有界候选选择和行锁 `SKIP LOCKED`，多实例可以并行，各实例不重叠执行自己的批次，不增加 leader/Redis/调度服务。立即清理撤销记录不提供安全审计保留承诺；当前没有审计产品需求。失败仅安全分类日志，等待下一轮；无立即重试、无耗尽式循环。清理失败或落后不影响查询拒绝过期/撤销会话。无表时 worker 可以失败但 API 仍可启动。退出取消 worker 并在原总期限内等待，然后关闭共享池。

备选只提供人工 DELETE 很简单，但易无限堆积；请求路径清理会放大延迟；原方案选择小型受管理 worker。上面的完整方案和现有Worker要求/任务本次保留，最后审查建议拆分的依据与影响见第11节；最终已批准本轮保留，不拆分或跳过任务；Apply须验证真实PG索引/批次及Runner退出；不虚构吞吐SLO。

### 3. Cookie 与开发/生产配置

| 属性 | production（默认） | development（显式 HTTP 例外） |
| --- | --- | --- |
| 名称 | `__Host-devpilot_session` | `devpilot_session` |
| Domain | 不设置，host-only | 不设置，host-only |
| Path | `/` | `/` |
| Secure | true | false |
| HttpOnly | true | true |
| SameSite | 显式 Lax | 显式 Lax |
| Max-Age / Expires | 根据已确认 expires_at 与当前时间计算剩余秒数/UTC日期，不超过数据库有效期 | 相同 |

选择 Lax 保留用户从外部链接进入网站的体验；它不替代 CSRF。Strict 更保守但阻止部分外部导航的认证；None 会扩大跨站风险，没有本轮需求。选择 Path=/ 满足 `__Host-` 约束并适合后续业务路径；Path 不是授权隔离措施。拒绝 Cookie name/domain/path/secure 的任意覆盖，避免不安全组合。前缀语义参考 [MDN Set-Cookie](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie)。

新增 `AUTH_MODE`（只允许 production/development，默认 production）、必填 `AUTH_ORIGIN`、`SESSION_TTL`。AUTH_ORIGIN 必须是一个规范 origin（scheme + host + optional port），无 userinfo、路径（包括末尾 /）、query、fragment、通配符、列表或空值；验证合法端口、ASCII 主机/IPv6 格式，拒绝歧义编码。HTTP Origin 比较使用解析后的 scheme/host/effective-port 完整元组，不使用字符串后缀。错误只标识变量名与固定原因，不回显值。

production 必须 HTTPS Origin；Go 本身可继续 HTTP 监听在受信任 TLS 入口后，但 Cookie Secure 永远 true，不读取请求 Host、Forwarded 或 X-Forwarded-* 来决定 origin/安全属性。入口必须强制 HTTPS、保护内部监听、禁止绕过 TLS、避免记录 Cookie；是否建立 TLS/代理由部署工作承担，本轮只文档化。

development 必须 HTTP Origin 且主机为 `localhost`、回环 IPv4 或 `::1`；`HTTP_ADDR` 必须显式 IP 回环地址，拒绝 `:8080`、0.0.0.0、公网/LAN IP 与需 DNS 解析的监听名，端口须与 Origin 匹配；localhost 可对应回环监听。示例 `AUTH_MODE=development`、`AUTH_ORIGIN=http://127.0.0.1:8080`、`HTTP_ADDR=127.0.0.1:8080`。开发模式不允许 HTTPS，开发者测试 HTTPS 可用 production 安全策略。本地端口不会隔离 Cookie，其他本地服务须视为可信环境，不能把开发例外用于公开部署。

新增必填配置是显式启动兼容变化，迁移文档/README/进程测试/CI需要同步。没有自动由“请求看似 HTTP”降级；切换模式时只读本模式 Cookie，旧模式 Cookie 不参与认证。

### 4. CSRF：精确 Origin + 固定自定义头，覆盖登录与退出

两个 POST 先验证 method，接着要求**唯一一个**非 null 且合法的 Origin 与 AUTH_ORIGIN 完整匹配，和唯一 `X-DevPilot-CSRF: 1`。缺失、重复、多 origin、恶意后缀、同站不同子域/端口、Referer-only 均返回 403 `csrf_rejected`，在读取凭据、哈希、数据库和 Cookie 修改前拒绝。若提供 Sec-Fetch-Site，只接受唯一的 `same-origin`；无该头仍需满足 Origin/自定义头。

这是一种同源 JSON API 协议，自定义头利用浏览器跨源请求预检限制，固定值不是秘密或一次性 CSRF token；同时校验 Origin 提供明确边界。无 CORS allow-origin/allow-credentials，OPTIONS 返回端点 405，不能把跨域预检放行。CLI/curl 必须显式带两个头，缺 Origin 不豁免；不回退到 Host 或 Referer，不新增 CSRF bootstrap 端点。登录尚无 Session 也会被 CSRF，因此必须覆盖；退出只有有效防护后才执行撤销，不能用 GET 退出。GET me 无写副作用、无续期、无需该自定义头。

参照 [OWASP CSRF Prevention](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html) 的 API 自定义头与来源检查。若未来引入跨源前端、表单、OAuth，需要重新评审协议，可能改为 session-bound synchronizer token。本轮不更改无 Cookie 副作用的注册请求契约；现有注册仍需要独立反滥用控制。

### 5. 登录、失败一致性与账号枚举边界

登录 JSON/media/4096-byte/原始 Unicode/重复键/类型等严格规则复用注册；抽取窄范围凭据解析/邮箱验证帮助代码，禁止为复用引入通用认证框架。邮箱沿用 ASCII trim/full lowercase 子集。登录密码接受 1–128 Unicode 码点、最多 512 UTF-8 bytes，不 trim/normalize/truncate；15 字符是**新密码创建**政策，验证无需再施加创建下限。非法字段 422 固定 fields，仅与输入有关，不查询账户；结构错 400，媒体错 415，超长 413。

合法请求取得登录流程槽后读取显式内部凭据投影（Public + password_hash）；只有邮箱未找到才是正常缺失，DB 错误不能伪装不存在。未知邮箱使用预生成且通过测试验证的当前参数 dummy PHC；坏 PHC 先安全分类，再同样 dummy。已有有效 PHC 的账户，无论 active/disabled，都执行一次真实 Verify，之后综合 password match、found、active、PHC validity 决定。所有这些认证失败统一 401 `invalid_credentials` / `Invalid email or password.`，相同 body/headers，不签发或删除 Cookie，不透露 disabled/not_found；dummy 匹配结果永远不能成功认证。固定 dummy 不在每个请求生成，避免加倍成本；其密码无需保密，只有真实用户可进入成功路径。

不宣称网络恒时：SELECT、调度和数据库缓存仍有差异；测试验证每类恰好一次相同成本 Argon 调用，不用脆弱毫秒阈值证明恒时。现有注册 409 明确暴露邮箱占用，所以登录防枚举只缩小**登录端点**额外泄露，不能声称全系统已防枚举。参考 [OWASP Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)。

成功返回 200 `data`，只含现有 `user.Public` 的 id/email/status/created_at，并发安全提交后签发 Cookie；不修改 users 的 hash/status/verification/timestamps。已有客户端 token 从不作为新 token。一次成功登录新增独立会话，其他会话不自动撤销；浏览器原 Cookie 被覆盖，旧行仍可存活直到自身退出/过期。避免重登录轮换/撤销在并发中的额外事务语义；不承诺单设备单会话，也没有多设备 UI。

### 6. PHC 严格解析：本轮只接受现存参数组合

解析前先检查非空和不超过 1024 bytes；严格要求 6 个 `$` 段、argon2id、v=19，以及现有规范参数顺序 `m=65536,t=3,p=1`，无额外/重复字段、符号、空白、前导零、指数、溢出或尾随数据。盐/输出使用 RawStdEncoding.Strict，先限制编码长度为 22/43，再解码检查 16/32 bytes，重编码相等；拒绝 padding、URL alphabet、CR/LF 和非零 padding bits。

**允许列表就是当前组合**：memory min=max=65536 KiB、iterations min=max=3、parallelism min=max=1，固定盐/输出长度，Argon 入参的 uint32/uint8 转换只能在边界验证后发生。比“允许所有算法合法参数”更窄，既保留既有全部注册记录，也建立可预测成本；未来参数升级先扩展经过资源评审的允许列表与 dummy 策略。本轮不接受低成本历史参数，不自动 rehash。DB 仍仅保证 PHC 字段存储长度，不增加参数 CHECK，保持已有主规格的参数演进余地。

可以分两步解析/Verify，使登录在坏 PHC 情况下不调用攻击者参数，并以合法 dummy 保持一遍计算。派生出的固定 32-byte key 用 `crypto/subtle.ConstantTimeCompare` 比较，不能比较 PHC 字符串或用普通 bytes.Equal。错误不携带 PHC；malformed/over-budget 输入不得触发 Argon 分配或 panic。加入解析 fuzz 和边界测试，合法盐重新计算与 Hash 互通，真实错误密码不匹配。

备选区间如 64–128MiB/t=1–6/p=1–4可支持未来记录，但没有现存需要，并产生耗时差异/更高峰值；本轮拒绝这项复杂度。依据 [Go argon2 API](https://pkg.go.dev/golang.org/x/crypto/argon2) 的类型/内存单位和 [RFC 9106](https://www.rfc-editor.org/rfc/rfc9106.html) 的参数定义，边界值沿用项目现有参数而非复制 RFC 推荐档位。

### 7. 共享资源预算、超时与公开防暴力破解门槛

`cmd/api` 只构造一个密码工作预算，注册 Hash、登录 real/dummy Verify 全部使用它：每进程最多两次 Argon 调用，无等待队列，defer 在真实返回/异常退出后释放。注册保留 `503 registration_busy`，登录占用失败为 `503 authentication_busy`，都带 Retry-After: 1。不能给两个 handler 各两个槽形成四次计算；不能在 Hash 外再套一个同预算 acquisition 造成双重占用。处理顺序为廉价 method/CSRF/media/body/field 检查后再获取资源。

登录另外有每进程两个流程槽，从首次 user SELECT 前到 session 事务确认/失败后一直保持，包括验证后数据库等待。第三个合法登录立即 authentication_busy，不先查邮箱、不排队。Argon 槽只保护计算，所以一个登录创建会话时另一个注册仍可计算。清理、me/logout 共享 pgxpool，数据库调用最多 2s 子期限；不另建无限连接池或后台请求任务。

登录总处理 context 为 5s，每一数据库操作最多 2s且受总截止时间限制；me/logout 总预算 2s。context 不强制中断 body Read/Argon，HTTP 超时保留原值；取消后不能先释放正在运行的槽再启动下一次计算，检查前后取消，不再开始新查询/事务/写 Cookie。默认两个计算约 128MiB 工作缓冲，GC/框架额外内存仍在，非 RSS 硬上限；Apply 用混合注册/验证与真实参数基准记录 latency/allocs/RSS。

公开部署必须另行交付并评审：可信来源 IP 的速率/突发限制、canonical account 键的持续失败预算与渐进延迟、跨实例一致策略、撞库/分布式低速攻击监测及告警、429/恢复窗口验收。账户键应覆盖不存在邮箱以免形成枚举信号；防止硬锁账户让攻击者制造拒绝服务，明确可信代理范围而非信任任意 X-Forwarded-For。可由受控 ingress 与后续应用功能组合实现，不默认引入 Redis/CAPTCHA。槽位/5s超时不等于限流。当前注册反滥用前置条件也必须继续满足；本轮只记录这些门槛，不把未实现的控制列为已通过。

### 8. 撤销、状态竞态和数据库故障

密码验证不持有数据库行锁。验证成功后开启短事务（READ COMMITTED），按 id 重新 `SELECT ... FOR SHARE`，核对仍 active 且 password_hash 与已验证快照完全一致，再 INSERT Session 并 commit。FOR KEY SHARE 不阻止仅更新 status/password，所以不能用它。状态/凭据变化返回相同401；锁等待受 DB/总期限约束。disable 先提交则登录不得创建；login 先提交则之后 `/me` 的 active 检查立即拒绝禁用账户。此锁只延续到创建 commit，不跨 Argon 计算。

`me` 每次查数据库 JOIN 当前用户，返回同样 Public，不缓存 active，不 SELECT *。缺失/损坏/重复 Cookie、无记录、过期、已撤销或 disabled 均返回 401 `unauthenticated` / `Authentication required.`，无 Set-Cookie；拒绝在 me 清 Cookie，以免并发旧请求的响应删除新登录 Cookie。过期/撤销以每次查询快照为准：撤销完成后新发起的验证必须失败，已经在撤销前读取到身份的请求可以完成，不承诺取消所有已在途请求。

未来 disable 操作应在同一事务中撤销该用户全部 Session，避免 re-enable 恢复旧会话。本轮无禁用/解禁接口，直接 SQL 只改 status 会令未撤销会话在 active 恢复时重新可用；明确这项限制，不用轮询/伪造账户管理功能掩盖。

logout 不先要求 active 身份：CSRF合格且 token 可解析则 `UPDATE sessions SET revoked_at=COALESCE(revoked_at,statement_timestamp()) WHERE token_hash=$1`，disabled/过期会话也可撤销。确认成功或查无记录后返回 204、空 body，并用相同 name/path/domain/Secure/HttpOnly/SameSite 的 Cookie 设 MaxAge<0（序列化为 Max-Age=0）、过去 Expires；没有/无效/重复 Cookie 也幂等204清本模式 Cookie，不访问DB。重复 Cookie 不挑选其中某一个撤销。其他会话不撤销。

撤销 DB 失败返回安全500/503，**不清 Cookie、不声称退出已完成**，保留客户端后续重试当前 token 的能力；可能已 commit 的撤销则重试幂等。Cookie 删除只是浏览器状态，不是撤销成功证据。并发 logout/logout 幂等；logout 与 login 的 Set-Cookie 响应可能逆序导致新 Cookie 被删除，客户端以后应串行认证变更，本轮不引入多标签协调/UI协议。

依赖故障与凭据失败区分：连接不可用503 `service_unavailable`、期限/已确认 statement timeout503 `database_timeout`、缺 users/sessions 等部署错误500 `internal_error`。不可因 DB 失败发401或默认认证通过。SQLSTATE57014无已确认超时证据保守internal_error；使用现有安全分类而不泄露原始错误。login提交未确认不签发Cookie、不自动重试，最多留下无人持有 token 的会话直到TTL清理；提交已确认但HTTP断连也无法保证浏览器收到了Cookie。原始请求取消不再写响应，不把context取消当作回滚证明。

三个端点及204均带 Cache-Control:no-store、X-Content-Type-Options:nosniff；JSON成功/错误带 Content-Type:application/json，204不发送JSON。405附准确Allow（login/logout=POST，me=GET），HEAD无body。固定错误消息沿用现有envelope风格；未写出前panic只记固定类别并返回500，已写出后终止连接，不暴露panic值。

固定响应表（依赖错误只表示无法确认结果，不承诺回滚）：

| HTTP / code | 适用路径 | 固定 message |
| --- | --- | --- |
| 200 / data | login、me | 无message，仅Public DTO |
| 204 / 空body | logout | 无message |
| 400 / invalid_request | login | Invalid login request. |
| 403 / csrf_rejected | login、logout | Request origin could not be verified. |
| 405 / method_not_allowed | 三个端点 | Method not allowed. |
| 413 / request_too_large | login | Request body too large. |
| 415 / unsupported_media_type | login | Unsupported media type. |
| 422 / validation_failed | login | Invalid login fields. |
| 401 / invalid_credentials | login | Invalid email or password. |
| 401 / unauthenticated | me | Authentication required. |
| 503 / authentication_busy | login | Authentication is temporarily busy. |
| 503 / service_unavailable | 三个端点 | Authentication operation could not be confirmed. |
| 503 / database_timeout | 三个端点 | Authentication operation could not be confirmed. |
| 500 / internal_error | 三个端点 | Unable to complete authentication operation. |

logout不需要JSON请求体，也不解析凭据或其他body字段；它只消费受保护请求的Cookie。不存在身份不阻止幂等退出，但Origin/header要求不变。公开限流未来的429不属于本轮API状态表。

### 9. 分层与未来 Workspace

推荐 `internal/auth` 管理三个 handler、CSRF边界和登录编排，`internal/session` 管理随机token/摘要、PG持久化/cleanup；扩展 `internal/user` 内部凭据读取，Public DTO继续不带hash。密码验证及共用预算留在 `internal/password`；凭据输入共用帮助逻辑可置于窄范围 `internal/credentials`，仅用于避免复制已验证的Unicode/email规则，不拆分通用框架。根路由新增显式路径，保持 health 与无关404行为。清理接入现有Runner，不加自建shutdown流程。

会话查出的 user UUID 是后续身份基础，认证不是 Workspace 授权：未来每项 workspace资源操作必须用该UUID查membership/角色，拒绝客户端提交的user_id作为可信身份，不能以email、active、Session id或“有登录Cookie”推导租户访问。邮箱所有权仍未验证，不写email_verified_at。将来有写操作时需按其事务边界重验账户/会话/权限，单次middleware检查不保证之后的授权写与撤销原子。本轮只交付可复用身份结果，不建Workspace表、RBAC或假授权接口。

认证结果只表示数据库查询时点观察到的有效身份，不能转换为整个业务操作期间不变的授权租约。未来Workspace必须分别定义可信user UUID、资源归属、membership/role、允许动作及撤销/禁用/权限变更与业务提交的先后语义。仅将普通权限SELECT和业务UPDATE放进同一个READ COMMITTED事务仍不足以排除TOCTOU；对应Change需选择与权限变更协作的行锁、条件写/版本协议或合适隔离与冲突处理，并用真实PG调度夹具验收。不能将本轮创会话的FOR SHARE逻辑泛化为业务授权原子性。参考[PostgreSQL事务快照](https://www.postgresql.org/docs/18/transaction-iso.html)、[行锁冲突](https://www.postgresql.org/docs/18/explicit-locking.html)；逐资源动作权限检查参照[OWASP Authorization](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)。

会话/用户/成员关系锁只能协调相应数据更新，不能阻止时钟推进导致expires_at到达。未来功能还须定义时间有效性以写入语句、重新检查或提交前哪个时点为准，不能承诺整个长操作期间权限始终有效。数据库外的外部副作用也不由入口认证或一段数据库事务自动保护。本轮不选择Workspace锁协议、不实现membership表或事务授权服务、不承诺取消已经验证的在途请求。

### 10. 可执行验收与 CI

单元覆盖严格输入/原始Unicode、CSRF source/header组合、Cookie完整属性/重复读取、RNG失败、严格PHC/fuzz、true/false/dummy路径、错误/日志脱敏、共享槽/流程槽的取消与异常释放、未知commit、固定TTL不续期和并发语义。通过调用计数/锁存器测试资源边界，不以睡眠阈值证明安全。OpenAPI合约测试覆盖全部错误与成功投影。

真实 PG18 仍仅使用 `devpilot_registration_test`（名称暂不重命名，复用既有已审查保护）；显式tag缺URL/Goose/PG版本必须失败。套件串行操作schema，迁移测试扩展完整1→2链：从旧schema保留users升级、up/up、单步Down仅删sessions且保留users、完整可丢弃库roundtrip、失败版本3回滚不误记。fixture helper显式到已知基线，不再假定一次down删users；坏SQL目录包括真实已应用的1/2再新增3。所有Down/drop仅在隔离库。

端到端用真实路由/Argon/PG/HTTP Cookie jar：register→login→me→logout→旧token拒绝；production用TLS test server，development用回环HTTP；多个独立Session与撤销隔离；禁用后拒绝、锁协调login/status/password更新、到期边界、真实撤销竞态、数据中无明文token、cleanup批量约束、连接/锁超时/缺表及同进程恢复。原有注册201/409/无Cookie、health/信号测试全量回归。

CI push增加`codex/add-user-login`，保留main/PR；固定既有PG18 digest/Goose版本，gofmt覆盖cmd/internal/tests，vet、unit/race和明确integration tag，加入integration race（同一隔离库顺序运行）。每项Requirement映射实际测试或明示操作评审；记录真实命令/结果/资源测量，不在规划阶段标记验收完成。

### 11. 最后审查：清理拆分、24h合约与注册隔离

**评审时曾推荐将自动清理移至独立 `add-session-cleanup`；用户最终明确批准本轮实现Worker，因此保留Requirement及任务6.1/6.2，不创建独立Change。下表保留两方案的评审比较，不作为延期授权。** 数据库WHERE条件在每次查询强制检查时间、撤销和账户状态，物理删除不是有效性判定。新增场景必须证明Worker从未启动或已停止且过期/撤销行仍在时，真实PG仍拒绝身份。

| 项目 | 本轮保留Worker | 推荐拆分获批准后 |
| --- | --- | --- |
| 认证安全 | DB时间/撤销/active谓词强制 | 完全相同，无逻辑失效窗口 |
| Schema/API/默认TTL | 现有sessions迁移、三个端点、24h | 不变；现有索引可保留供后续清理使用 |
| 本轮实施/CI | 验收15m/1000行/2s、SKIP LOCKED、多worker和关闭 | 移出任务6.1/6.2及worker专属测试；保留无worker失效、并发与故障验收 |
| 后台生命周期 | Runner管理清理并共用池 | 本轮不接入清理Worker，不新增后台任务 |
| 存储/运维 | 周期回收，仍可能积压 | 过期/撤销/未知commit孤儿行持续增长，需要容量预算、告警和清理责任 |
| 后续交付 | 无独立清理Change | 单独验收批次、索引计划、保留/审计、多实例锁和总退出预算 |

拆分降低后台调度/行锁/退出与认证切片耦合，使容量和保留政策独立评审。代价是暂无自动空间回收；公开部署容量审查需明确增长预算、告警和受控清理计划。不能省略expiry/revoke查询，不能将“未物理删除”视为有效。操作若需清理仍遵守既有环境/数据保护规约；本轮不新增运维CLI、不执行清理SQL。后续独立Change可保留原15m/1000行/2s作为起点评审，而非未经测量的容量保证。

**24h合约不变：** 无SESSION_TTL时数据库expires_at-created_at恰好24h，覆盖范围仍5m–24h；me不改created_at/expires_at、不发Set-Cookie，无refresh端点、Refresh Token或滑动续期。Apply中的README/OpenAPI/contract测试须一致明示；真实PG测试验证默认差值、重复me前后行值、保留过期行+强制发送Cookie的401及并发失效。批准Apply后按本节同步API文档与测试；实际运行证据记录在verification.md，不以设计文本代替执行结果。

**注册回归冻结与故障隔离：**

| 边界 | 必须保留/新增的验收 |
| --- | --- |
| HTTP/输入 | 原201/409、400/405/413/415/422、503 registration_busy/service_unavailable/database_timeout、500及message/fields/headers；原始Unicode、15–128码点/512-byte、4096-byte、规范化、HEAD/Allow不变 |
| 用户/密码 | 同原Argon参数/盐/输出，无Cookie/token/Session/mail；重复并发仅一行，不改已有hash/status/email_verified_at/时间 |
| 共享槽 | 混合Hash/real/dummy Verify实际同时数≤2，无队列/双重获取；取消未返回前不释放，故障/panic释放后两端可复用，busy code按端点区分 |
| 登录专属故障 | injected Verify panic、坏PHC、token RNG失败、sessions缺表/创建失败不泄漏槽或设置跨端点故障状态；users/密码依赖健康且计算返回后注册仍成功 |
| 共同故障/等待 | login流程槽及session DB等待不占Argon槽；共享池耗尽/DB全面故障可影响注册有界DB阶段，不承诺存储故障隔离或公平调度 |
| 可用性/防滥用 | 廉价无效注册不拿槽，health/me/logout不获取密码槽但仍受HTTP/DB预算；持续登录可能竞争资源，公开部署需分端点限流，不虚构预留槽或无饥饿保证 |

现有 `TestStrictRequestsAndCheapRejection`、`TestRepresentationAndMethods`、`TestUnicodeRawEscapes`、`TestValidationBoundaries`、`TestCancellationKeepsSlotsUntilHashReturns`、`TestErrorResponsesAndSlotRelease`、`TestServerDeadlineAndUnknownCommit`、`TestClientCancellationInStoreAndRealHTTP`、`TestOpenAPIContract`、真实PG注册/unique/恢复测试及普通/race都须保留覆盖。注入可改为统一预算，不能删断言、降低输入安全或用Mock替代原真实PG证明；混合负载用同步屏障/调用计数，不以波动时延阈值证明隔离。

## Risks / Trade-offs

- [Cookie是bearer secret，24h无idle期限] → TLS、HttpOnly/host-only、服务器期限/撤销；高敏感业务另行评审更短期限与重新认证。
- [注册409仍可枚举] → 登录统一失败只能限制新增泄露，不声称系统完全防枚举；修改注册隐私契约需要独立Change。
- [资源槽不能挡持续攻击/跨实例滥用] → 公开前必须交付限流、防撞库与运营验收；禁止静默降Argon参数。
- [主机同源的XSS可发起认证请求] → CSRF方案不解决XSS；未来前端需独立安全设计。本轮没有前端。
- [绝对过期清理可能落后、重登录旧行仍可用] → 数据库每次校验期限，有界worker与公开限流容量评审；无设备管理承诺。
- [瞬时撤销与在途请求不可能自动原子] → 明确快照语义，未来业务写按事务设计权限检查；当前仅身份查询。
- [直接SQL解禁能恢复未撤销Session] → 禁用期间始终拒绝；未来禁用事务同时revoke-all，操作文档注明限制。
- [必填AUTH_ORIGIN改变启动配置] → 在监听前明确失败，示例/CI/所有进程测试同步；不允许隐式HTTP降级。
- [DB故障/提交未知] → fail closed且与401区分，Cookie只在已确认commit后签发/清除，不自动重试。

## Migration Plan

1. 规划产物审阅后按新的 Apply 请求实施，在`codex/add-user-login`分支工作；不更改已应用users migration。
2. 在专用PG18测试库完成全部schema/HTTP/并发验收，更新文档和配置示例，记录verification与CI证据。
3. 有数据的部署先备份，由单一受控步骤串行Goose status/up/status，再配置AUTH_MODE/AUTH_ORIGIN/TTL及HTTPS入口后部署API；/readyz仅连通性，不能代替会话schema验收。
4. 公开上线之前额外交付并评审登录限流/防暴力破解和注册反滥用；Change实现完成不代表获准公开部署。
5. 应用回滚保留users/sessions/迁移历史；恢复到旧API意味着它不再提供登录路由，不执行自动Down。共享/生产日常禁止down/down-to/redo/reset；例外破坏性恢复仍需独立授权、验证备份与恢复方案。本轮不执行发布、合并或归档。

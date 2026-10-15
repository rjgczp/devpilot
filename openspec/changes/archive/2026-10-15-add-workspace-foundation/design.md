# Design

## Context

动机与业务范围见 proposal.md。2026-10-15（Asia/Shanghai）只读核验：工作开始时 `main` 干净，HEAD 与本地 origin/main 均为 `99f3f516db284ef31b70fa9131d3653dd61cd40a`；远端 `git ls-remote origin refs/heads/main` 返回同一 SHA。首次沙箱网络查询失败，随后只读查询成功，没有 fetch、reset、切分支或覆盖文件。

三个归档目录分别是 `2026-10-13-bootstrap-go-backend`、`2026-10-14-add-user-registration`、`2026-10-15-add-user-login`；历史任务均已勾选，历史 verification 是其当时的验收记录，不作为本轮测试证据。已检查六份主 Specs、归档 proposal/design/tasks/verification 的相关规则、源码、测试、OpenAPI、迁移与 CI。

关键实现基线：Go 1.24.3、net/http、pgx/v5 v5.8.0、PG18、Goose v3.26.0；users/sessions 迁移为 1/2。`session.CookieDigest` 严格拒绝重复/非规范/带凭据空白的 Cookie；`session.Store.Lookup` 以数据库 statement time 联合校验 active/expiry/revoked。`auth.Handler.csrf` 与 Cookie 名称选择目前为私有方法，不能假定已有可复用认证中间件。`user.Classify` 含注册专用 email 冲突映射，Workspace 不应误用为通用唯一冲突业务错误。服务已支持共享 pgxpool、有界 worker、readiness 仅连通性。

集成测试有具体迁移耦合：registration suite 假定最新版本为 2、两次 down 删除 users，并构造版本 3 的失败迁移；session suite 用最新 up 后一次 down 假定删除 sessions。新增版本 3 后这些假设必须显式修正，不能只添加新测试。测试库沿用受保护的 `devpilot_registration_test`，不借本轮重命名或放宽保护。

## Goals / Non-Goals

**Goals:** 每次返回的 Workspace 数据有可追溯的可信身份及成员约束；创建事务对身份状态变化有明确顺序；以小规模真实 PG 测试证明拒绝越权及原子性。避免在数据层提供易被误用的无身份详情方法。

**Non-Goals:** 不引入通用权限引擎、请求全程身份租约、数据库外副作用或跨页一致快照；不把数据库管理员、任意直连 SQL、SQL 注入或整套应用失陷视为当前应用授权能隔离的攻击者。用户已确认成员管理单独 Change，本轮没有加入他人 Workspace 的入口，也不承诺 owner 的管理操作已经可用。

## Decisions

### 1. 两张表、UUID 资源标识、非唯一显示名称

| 表/字段 | 设计 | 约束与原因 |
| --- | --- | --- |
| workspaces.id | uuid PK，DEFAULT uuidv7() | PG18 原生生成，不依赖扩展；客户端不能指定 |
| workspaces.name | text NOT NULL | CHECK char_length 1–100、octet_length ≤400、无 ASCII C0/DEL、无边缘 SP/HT/CR/LF；允许重名 |
| workspaces.created_at | timestamptz NOT NULL，DEFAULT statement_timestamp() | 数据库创建时间 |
| workspaces.updated_at | timestamptz NOT NULL，同上 | 初始与 created_at 相同；无自动更新时间 trigger，本轮无更新 API |
| workspace_members.workspace_id | uuid NOT NULL，FK workspaces(id) ON DELETE CASCADE | 成员依附租户，删除仅可在本轮隔离测试中发生 |
| workspace_members.user_id | uuid NOT NULL，FK users(id) ON DELETE RESTRICT | 不因用户级联删除使 Workspace 静默失去 owner |
| workspace_members.role | text NOT NULL，CHECK IN ('owner','member') | 服务创建时显式赋 owner，不接受请求角色 |
| workspace_members.created_at | timestamptz NOT NULL，DEFAULT statement_timestamp() | 加入时间；不需要独立更新时间 |
| workspace_members PK | (workspace_id,user_id)，命名 workspace_members_pkey | 同一用户同一 Workspace 只有一个角色；不同 Workspace 可有不同角色 |
| workspace_members_user_workspace_idx | (user_id,workspace_id) | 用户列表和 UUID 顺序分页；复合主键已覆盖 workspace 前缀，无重复单列索引 |

CHECK/FK 使用明确名称（如 workspaces_name_check、workspace_members_role_check、workspace_members_user_id_fkey），便于直接 SQL 约束测试及错误分类；不将约束名返回客户端。Workspace 不保存重复的 creator_id/owner_id，也不建成员独立 UUID：成员复合键已是其唯一身份，避免维护两套 owner 事实。

每次创建恰好一个 owner；schema 允许同一 Workspace 多个 owner，以免无产品要求就引入“唯一 owner”的限制。本轮没有 owner 晋升/降级/移除入口，因此产品创建路径保持至少一个 owner。**普通 FK/CHECK 不保证每个 Workspace 始终至少一个 owner**，任意受信任 SQL 仍可破坏此跨行条件；不声称已有数据库最后 owner 防护。用户 FK RESTRICT 阻止删除任何有成员关系的用户，不仅 owner；未来账户删除需显式处理成员与所有权，不改变 sessions 原有 CASCADE 定义。

备选将 owner_id 放在 workspaces 再与成员交叉引用，会增加冗余/循环约束；owner 唯一部分索引仅能保证“至多一个”，不能保证“至少一个”。当前无需复杂 trigger 或延迟循环外键。后续成员管理 Change 再审查最后 owner、多 owner、退出和转移规则。

名称是展示字段：按 workspace-access spec 只裁剪边缘 ASCII 四种字符；长度以裁剪后 Unicode scalar 计数，拒绝所有 Unicode Cc 和剩余边缘 Unicode whitespace，保留中间普通空格/大小写/Unicode，不做 NFC 归一化。422 顺序为 required → invalid_length → invalid_format。DB 保证基础长度/ASCII边界/控制约束，应用保证更完整的 Unicode 展示政策，不能把数据库检查宣称为完整 Unicode 验证。ASCII控制检查应覆盖 U+0001–001F、U+007F，U+0000 由 PostgreSQL text 自身拒绝。

不引入 slug 或名称唯一约束：当前路由只需不可变 UUID，不需要全局命名争抢、保留字、重命名重定向或跨租户名称冲突反馈。UUIDv7 有时间可推断性，不是权限秘密。[PG18 UUID 文档](https://www.postgresql.org/docs/18/functions-uuid.html) 确认原生生成支持；选用数据库生成沿用现有用户/Session 约定。

### 2. 三个 API 与完整响应契约

| 方法/路径 | 输入 | 成功 |
| --- | --- | --- |
| POST /api/v1/workspaces | Cookie、同源证明、仅 {"name":"研发团队"} | 201，Location 指向新资源，data 为 WorkspaceDTO |
| GET /api/v1/workspaces | Cookie，limit/after 可选 | 200，data 数组，pagination.next_after 为 UUID 或 null |
| GET /api/v1/workspaces/{workspace_id} | Cookie，规范 UUID，无 query 选项 | 200，data 为 WorkspaceDTO |

WorkspaceDTO 仅 `id,name,role,created_at,updated_at`；role 属于请求者，非 Workspace 全局角色。时间为 UTC RFC3339，与原 API 一致。无 owner/member 名单、邮箱、人数、Session、内部配置。成功示例：

```json
{"data":{"id":"019a5abc-1234-7123-8123-123456789abc","name":"研发团队","role":"owner","created_at":"2026-10-15T08:00:00Z","updated_at":"2026-10-15T08:00:00Z"}}
```

列表空结果为 `{"data":[],"pagination":{"next_after":null}}`。limit 默认 50，1–100 的无符号、无前导零十进制；after 只接受规范小写带连字符 UUID，不强制版本 7 以保留迁移兼容。ID 升序 `id > after`，最多查询 limit+1 条，返回前 limit 条；有额外一条才令 next_after 为最后返回 ID。无 total、无 Offset、无 cursor 签名或全遍历快照保证。after 是纯排序值，不要求它存在或属于调用者，否则会引入存在性侧信道。跨页新建/成员变化可能影响遍历，只承诺每页当前授权，UUID 时间顺序不等于事务提交顺序。

创建/详情拒绝未知 query 参数；列表拒绝未知键、重复键、非法百分号编码及空/错误 selector。GET 不解析或利用请求体提供身份/过滤条件；不接受 Authorization/query user_id 替代身份。路径 ID 格式错误对已认证调用者与不存在资源相同 404；额外路径段按未注册路由处理，不触发租户查询。格式校验不得把未经验证的 UUID 字符串拼进 SQL。

响应状态、固定消息：

| HTTP / code | message | 适用 |
| --- | --- | --- |
| 400 invalid_request | Invalid workspace request. | JSON结构、query参数/编码 |
| 401 unauthenticated | Authentication required. | 有效形状请求缺失/无效身份 |
| 403 csrf_rejected | Request origin could not be verified. | POST 来源/证明失败 |
| 404 workspace_not_found | Workspace not found. | 已认证详情的非成员/不存在/非规范UUID |
| 405 method_not_allowed | Method not allowed. | collection Allow: GET, POST；detail Allow: GET |
| 413 request_too_large | Request body too large. | POST超过4096 bytes |
| 415 unsupported_media_type | Unsupported media type. | POST媒体/编码规则 |
| 422 validation_failed | Invalid workspace fields. | fields 仅 name 与固定原因码 |
| 503 service_unavailable | Workspace operation could not be confirmed. | 连接/处理期限故障，包括未知提交 |
| 503 database_timeout | Workspace operation could not be confirmed. | DB子期限/已确认statement timeout |
| 500 internal_error | Unable to complete workspace operation. | schema、约束/未知错误、panic |

错误 envelope 为 `{"error":{"code":"...","message":"..."}}`，仅422可增加 `fields:{"name":"..."}`。本轮没有名称409、成员权限修改403、幂等冲突或新429。所有响应带 no-store、nosniff；JSON Content-Type: application/json；HEAD 与对应405相同状态/Allow但无 body。GET pattern 不隐式放行 HEAD，OPTIONS 405 无 CORS；路由需避免 Go ServeMux GET/HEAD 及路径重定向默认行为改变明确契约，相关路径采用显式方法分发并测试。不改变无关路径及原 health/auth 响应。

校验顺序：方法 → POST CSRF → Cookie语法 → 廉价请求媒体/结构/字段 → DB认证与授权 → 成功。无效/缺失 Cookie 可以直接401；格式正确但未知的 token 仍可能先收到输入错误，正常有效输入的各种身份失效统一401。详情路径格式判断不得探测资源：完成 live identity 后对格式错误返回404。所有 Workspace 响应均无 Set-Cookie，包括401和创建成功；保持原 Session绝对TTL、无刷新及旧请求不清除新Cookie语义。

### 3. Owner / Member 权限与成员管理拆分

| 操作 | 有效账户（无此Workspace成员关系） | owner | member |
| --- | --- | --- | --- |
| 创建新的独立 Workspace | 允许，成为新Workspace的owner | 允许，权限针对新Workspace | 允许，权限针对新Workspace |
| 列出自己的 Workspace | 允许，只含自己的成员关系，可为空 | 允许 | 允许 |
| 读取此 Workspace 详情 | 404 | 允许 | 允许 |
| 读取别人的 Workspace | 404 | 404，owner只对其所属Workspace有效 | 404 |
| 修改/删除 Workspace、管理/退出成员、变更角色/owner | 无接口 | 无接口 | 无接口 |

本轮角色读取权限相同；owner 表示创建时的所有权事实和未来治理身份，不因角色存在就新增写权限。没有一个全局 owner 身份可以跨租户授权。禁用账户或失效 Session 没有上述权限，正常有效请求统一401。创建发生在租户成立前，不需要也不能检查“已有租户 owner”。

用户已确认成员管理另开 Change。届时可从 **仅一个候选入口** `POST /api/v1/workspaces/{workspace_id}/members` 开始评估：当前 owner 添加明确已注册用户 UUID，目标角色固定 member；操作者身份只能来自 Session，target user_id 仅表示目标。必须独立讨论用户同意、目标账户发现/隐私、重复加入、目标禁用、限额、成员移除/退出与最后 owner；不能凭未验证邮箱认领身份。不在本轮文档写成可调用的已承诺 API，不提供运维 SQL 充当加入产品流程。

### 4. 授权读取：身份、成员关系和资源在同一快照

`internal/workspace` 的对外业务读取方法必须携带当前 Cookie 摘要/可信身份上下文与资源选择；禁止无身份 `GetByID(id)` 返回租户数据。Cookie摘要是内部认证输入，不可打印或暴露。数据层显式选择 DTO 字段，不 SELECT *。参数化绑定摘要、UUID、分页参数。

一次读取 SQL 以 live identity（sessions JOIN users；摘要匹配、active、revoked_at IS NULL、expires_at > statement_timestamp()）为根，再通过 membership.user_id=identity.id 且 membership.workspace_id=workspaces.id 读取资源；详情还约束指定 workspace_id，列表按 user_id 和排序边界约束。采用身份根记录加 LEFT JOIN/LATERAL 的窄结果，使**同一次语句快照**能区分：无身份→401；有身份无详情→404；有身份列表无成员→200空数组。不要把“0 rows”统一为404或先做无约束workspace存在性查询。

数据只从带成员约束的查询投影产生。可早做身份检查以廉价拒绝，但不能复用它跳过实际资源语句中的 live identity 与membership。非法UUID只查询身份后返回统一404，不做强制uuid转换触发500。未知/非成员采用同样响应，避免显式泄露，**不承诺网络恒时或数据库管理员不可观测**。

读语义是语句快照：成员删除/降级或 revoke/disable 在快照前提交则生效；已有授权快照可以完成。本轮不在 GET 上加成员/Session行锁，不为只读接口阻塞成员维护，也不缓存权限。多页分别重新验证，旧cursor和先前响应不能授予后续访问。

### 5. 创建事务、锁顺序与过期检查时点

采用 READ COMMITTED 短事务，整段 Begin/查询/插入/Commit 共用最多2s DB预算，外层处理预算5s。共享pgxpool现有容量；不占Argon槽，不新增认证流程槽、后台写任务或自动重试。只有确认Commit后返回201/Location；回滚/连接释放路径也须有界，取消的连接不能泄回池中继续未完成事务。

创建协议：

1. 从严格Cookie摘要查出候选 session.user_id；这是可信数据库身份线索，尚不是最终授权。此读可在事务内完成，不接收客户端user_id。
2. 按 user UUID 锁 `users FOR SHARE`，读取当前status；没有行或非active→401。锁必须阻止非键 status 更新，不能用 FOR KEY SHARE。
3. 按摘要和该user锁 `sessions FOR SHARE`，读取当前行并确认未撤销；不存在/所属不符→401。锁用户后锁Session，避免与未来先user后revoke-all的禁用事务反序。
4. **锁取得后启动新的工作区插入语句**，以受锁用户active、受锁Session未撤销及 `expires_at > statement_timestamp()` 为条件门控INSERT，零行→401。必须使用这个新语句的数据库时间，不能用事务开始时间或锁等待前SELECT的时间；不能仅在Go中根据旧expires_at判断。
5. 使用返回的数据库Workspace ID插入当前user的owner成员，提交。任一明确失败回滚两行；不发布中间结果。

FOR SHARE 阻止相关行 UPDATE/DELETE，与现有 logout 的 Session UPDATE、账户status UPDATE 和cleanup DELETE协调；多个create持共享锁可并行。现有logout不锁user、cleanup只锁Session并SKIP LOCKED，不形成由它们引入的反向user依赖。若未来一个事务锁多个user，按UUID排序，再锁Session；未来成员写需要先统一user/session身份锁，再按Workspace/成员的稳定顺序取锁，具体协议必须由后续Change验证。

| 并发顺序 | 结果 |
| --- | --- |
| revoke先提交，create再锁Session | 看见revoked或已被cleanup删除，401，无workspace |
| disable先提交，create再锁user | 看见disabled，401，无workspace |
| create先锁保护行 | revoke/disable等待；create先commit/rollback，再允许状态变化；变化后的新请求401 |
| create等待锁期间过期 | 后取新插入语句时点，拒绝401 |
| 插入门控时有效，commit前到期 | 可以提交；有效性定义在插入语句时点，锁不冻结时间 |
| 创建后身份失效但201尚在网络传输 | 不撤销已提交业务；后续读取重新验证身份 |

这是数据库内新建资源操作的具体协议，不宣称阻止所有在途操作，也不以“用了事务”替代锁协调。未来修改已存在Workspace资源必须在同一事务同时验证资源归属、成员role，并用与成员变更冲突的锁/条件协议；本轮只有创建，无需要已有成员授权的更新接口，不预建虚假的通用写授权框架。

选择依据：[PG18 行锁冲突](https://www.postgresql.org/docs/18/explicit-locking.html) 和 [事务快照](https://www.postgresql.org/docs/18/transaction-iso.html)。普通READ COMMITTED SELECT不能保持权限到提交；SERIALIZABLE需增加重试/冲突语义，当前少量确定行锁更易审查。该判断是本项目设计取舍。

### 6. 未知提交、安全故障与协议复用

第二次插入失败且回滚确认时两行均不保留；Commit网络断连/超时/取消不能断言未创建。不自动重试、按名称去重或客户端UUID补偿删除。连接正常的错误响应使用“could not be confirmed”；原请求取消/断连不再尝试响应。客户端可先列表检查，但同名不是可靠请求关联标识；显式重试可再创建第二个Workspace。本轮不承诺exactly-once，不引入幂等键表。如果产品要求未知结果安全重试，需要在Apply前修订为另一个明确契约。

创建的两行要么一起commit要么一起不提交，未知结果仅表示调用者不知道是哪一种，不允许数据库出现一行的半提交。真实网络故障难稳定复现确认丢失，测试采用真实PG事务配合受控提交结果故障点，分别模拟“底层已commit但调用者收到连接错误”和“commit前确认回滚”，断言服务调用计数不重试、响应不虚假成功、列表反映实际存储；明确这是受控故障注入，不伪装成真实断网证据。

在 `internal/auth` 或窄的共享协议文件中提取现有CSRF验证和环境Cookie名称策略，保持现有登录/退出测试；继续使用已有 `session.CookieDigest`，不新写宽松解析器，也不建立一个仅凭middleware UUID就授权的框架。POST完整复用AUTH_ORIGIN scheme/host/effective-port、唯一Origin及X-DevPilot-CSRF:1、可选Fetch Metadata same-origin；GET无写副作用；不开CORS、不信Host/Forwarded、不回退Referer。生产/开发Cookie安全属性、Session TTL与cleanup均沿用主Spec。

DB错误使用安全分类，Workspace包装需把意外唯一/FK/CHECK错误映射500，不能复用注册的email_conflict输出；DB连接503、明确DB期限503、缺schema500、未证实为timeout的57014仍500。锁等到2s子期限视database_timeout，无自动重试。处理期限在未开始DB前耗尽为service_unavailable。依赖恢复后可重试读/显式新建，无全局故障状态。

日志仅固定事件/原因/状态（必要时固定route标签），不含请求路径原文/query、Cookie/Set-Cookie、token/digest、name、email、password/PHC、完整请求头/体、DSN、SQL/PgError/SQLSTATE、panic值。成功HTTP响应可含调用者有权读取的name，不记录日志。捕获敏感panic值后写前500、写后中断连接，沿用既有安全策略。

### 7. 多租户原则与 RLS 取舍

**本轮不启用RLS。** 当前只有单一API访问入口、两张租户表、三个可完整审查的资源查询。应用显式identity+membership约束可与现有Session协议、错误分型、短事务锁协调一起验收；没有现成的独立DB执行角色/迁移角色、池连接租户上下文或RLS测试基础。引入RLS会增加身份上下文设置和跨租户列表/创建策略复杂度，而不会自动解决角色检查、会话撤销或权限竞态。

RLS作为纵深防御能降低遗漏WHERE导致的暴露风险；当前选择显式授权意味着遗漏条件仍有风险，必须以窄存储接口、查询评审和真实跨租户拒绝矩阵控制。当前共享开发数据库角色可以直接读取其他租户，不能宣称数据库物理隔离或保护任意SQL执行者。

官方规则需纳入后续评审：表owner通常绕过，superuser/BYPASSRLS绕过；FORCE约束普通owner但不能约束这些特权；唯一/FK完整性检查也不由RLS隐藏。若引入应分离迁移owner与非owner/NOBYPASSRLS运行角色，按操作设置USING/WITH CHECK；用事务局部可信身份上下文，缺失时拒绝，并验证连接池复用不串租户。RLS中查询membership也需要并发设计，不能宣称RLS天然解决TOCTOU。[PG18 RLS文档](https://www.postgresql.org/docs/18/ddl-rowsecurity.html)。

当出现多个数据访问服务/分析工具、表与查询数量明显增长、团队难以完整审查所有边界或有数据库层隔离要求时，单独Change引入RLS并验证运行角色绕过、上下文缺失、池复用、备份/迁移与策略并发。该触发条件不代表当前已提供数据库强制隔离。

未来tenant-owned项目、监控、告警等实体原则：非空workspace_id FK；每个读取/更新/删除同时绑定可信成员身份与资源workspace_id，用户归属不能代替租户归属。租户内唯一字段用(workspace_id,业务键)，父子实体用(workspace_id,parent_id)→父表(workspace_id,id)的复合FK（父表提供对应UNIQUE）阻止跨租户关联；更新/删除即使只收到全局ID也必须带授权租户条件。后台作业携带经过服务端校验的workspace_id和明确服务权限，不接受请求任意指定。平台级users/sessions不因未来租户表而变成单租户账户。本轮不创建这些未来表、索引或任务框架。

### 8. 验收与 CI

所有新Requirement含Success/Exception/Security/Concurrency场景；user-authentication delta保留原四个场景，不削弱原约束。新建fixture用户A/B/C及Workspace X/Y/Z：A owner X、member Y；B owner Y、member X；C无成员；Z仅B可见。逐端点断言身份、角色、存在性、跨租户路径替换及cursor替换。成员关系只由受保护测试夹具写入，不增加产品或运维成员管理入口。

单元覆盖严格JSON/Unicode/媒体/4096bytes、名称边界、分页/UUID、全部响应、CSRF零DB调用、Cookie严格解析复用、no-Set-Cookie、方法/HEAD/OPTIONS、故障/取消/panic、调用计数和隐私哨兵。存储测试不能只断言SQL字符串含workspace_id，必须真实PG证明A不能读Z，并验证未知资源与非成员完整错误相同。

PG18集成覆盖schema约束与索引；0→3、已有数据2→3、up/up、v3 down只删workspace表、1/2独立迁移回归、失败v4原子回滚；身份锁双方顺序、等待后过期、读快照前后成员移除/role变化、事务第二写失败、未知commit两种存储结果、数据不可半观察、并发同名创建和重复pair唯一性。并发使用受控连接、屏障及锁等待观察，不凭固定sleep证明顺序。所有破坏性fixture仍仅限通过testdb保护的专用库，schema suite不并行。

CI沿用固定PG18 digest和Goose，不无关升级；未来开发分支默认 `codex/add-workspace-foundation`，在现有push列表增加该分支并保留main/PR。运行gofmt、vet、unit、race、显式integration及integration-race。初次失败可按合理测试耗时评估总timeout，不删旧用例或静默skip；缺URL/Goose/PG18必须失败。README/OpenAPI/迁移文档同步，运行健康/注册/登录/Session/cleanup回归，记录Requirement→实际用例/证据映射，不能将规划通过记为实施验收通过。

## Risks / Trade-offs

- [无成员加入入口] → 已由用户确认拆分；本轮可创建多个Workspace，模型和授权支持共享成员，完整加入体验后续交付。
- [无RLS，遗漏查询条件仍有风险] → 窄接口、同语句授权、代码审查及真实跨租户矩阵；多个数据入口前重新评审。
- [没有全DB最后owner不变量] → 无产品成员写入口、创建事务保证初始owner、用户删除RESTRICT；后续成员管理单独设计。
- [非幂等创建遇未知结果会重复] → 不自动重试，明确错误文案/列表恢复与产品确认；名称不能当幂等键。
- [锁竞争与时间推进] → 2s事务预算/稳定锁顺序/门控新语句时点；不承诺任意commit时点仍未过期或HTTP响应可撤销。
- [没有配额/请求频率控制] → 有界请求/分页/DB时间不等于防滥用，沿用既有公开部署前认证反滥用要求，Workspace创建频率/配额在公开发布评审另行明确，不隐式交付限额系统。
- [用户删除被所有成员关系阻止] → 与账户数据保留一致，后续删除产品显式处理；不静默级联丢失Workspace所有权。

## Migration Plan

1. 本轮仅完成规划和严格校验，等待独立技术评审。用户确认范围不等于批准Apply。
2. 新请求批准Apply后实施版本3和对应代码/测试/文档；修改旧migration测试基线，保持已应用迁移1/2字节内容不变。
3. 仅在隔离PG18测试库实际验收迁移链和事务协议；本轮不执行任何真实迁移。
4. 将来共享环境发布前备份，由单一受控步骤status/up/status再部署API；readiness不检查schema，需独立迁移验收。
5. 应用回退保留Workspace及成员数据，旧API可继续使用users/sessions；正常发布不down/reset，schema修复用forward migration。破坏性恢复仍需独立授权、已验证备份与恢复方案。

## Product Review Decisions

以下是本提案已选定的推荐契约，待用户/独立评审接受或提出修订，不能在Apply时自由另选：

1. **已确认：** 成员管理单独Change，本轮只含创建/列表/详情/成员授权基础。
2. 名称允许重名，以UUID为标识、无slug；名称1–100码点/400bytes和明确裁剪/控制字符规则。
3. owner/member本轮拥有同样的所属资源读取权限，无改名/删除/治理接口；schema允许未来多owner但不提供设置入口。
4. 非幂等创建：未知结果不自动重试，客户端重提可能产生第二个同名Workspace；如要求安全去重需先修订本Change。
5. 默认50/最多100的UUID分页，无总数或跨页快照；当前没有业务配额。
6. RLS暂缓、显式应用授权；用户删除RESTRICT与最后owner数据库约束的已知边界。

这些决策均已反映在spec/tasks中，评审如改变契约须先同步所有规划产物，不以“以后决定”进入实现。

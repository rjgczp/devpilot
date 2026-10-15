# Design

## Context

动机见 proposal.md。2026-10-15（Asia/Shanghai）只读核验：初始工作区干净；成功 `git fetch origin main` 后，`main=origin/main=70d53c7267b6e433be16ea020b4c2a1bd9b7d9ee`，左右差异 0/0。四个归档为 2026-10-13 bootstrap、2026-10-14 registration、2026-10-15 login 和 workspace-foundation。归档验收是历史证据，本次未运行业务测试或连接数据库。

已核对八项主能力清单、相关主 Specs 全文、归档 proposal/Workspace design/verification、实际 store/handler/routes、迁移、OpenAPI、测试与 CI。Go 1.24.3、pgx/v5 v5.8.0、Goose v3.26.0 与固定 PG18 镜像沿用。现状：`workspace.Store.Create` 先无锁发现 session.user_id，再 User FOR SHARE → Session FOR SHARE，新 INSERT 语句检查 expiry；List/Detail 以一个语句快照联合验证身份和成员；logout 只 UPDATE session；cleanup 只锁 session，SKIP LOCKED。已有 `auth.ValidCSRF`、`auth.CookieName` 和严格 `session.CookieDigest`，无须新造认证解析器或信任中间件缓存。

版本 3 已包含成员复合 PK、owner/member CHECK、Workspace CASCADE、User RESTRICT 及 `(user_id,workspace_id)` 索引。FK/CHECK 不保证至少一个 Owner。现有集成测试含可复用真实 pgx 事务观察器、`pg_blocking_pids` 屏障、提交结果注入；Workspace 迁移测试明确断言版本 3，注册失败迁移夹具用版本 4。本方案无迁移，因此不调整这些版本假设。

用户在本轮明确确认：**本轮列表、移除和退出；加入另做需用户同意的 Change**。这解决了范围决策，不构成 Apply 授权。

## Goals / Non-Goals

**Goals:** 提供可独立验收的成员读取与撤销；任何新请求使用实时权限；最后一个 Owner 不能通过支持的产品写路径离开；并发顺序可解释且有 PG18 验证计划。

**Non-Goals:** 不将本切片描述为已完成加入闭环，不提供添加/邀请/接受/重加或角色更新接口；多 Owner 只通过已有数据或受保护测试夹具覆盖。无数据库外副作用、全请求身份租约、跨页快照、任意 SQL 隔离或数据库管理员防护。完整排除项见 proposal。

## Decisions

### 1. 加入与同意：已选择较小交付

直接添加 UUID 存在以下产品风险：目标用户没有机会拒绝；其账号被关联到陌生 Workspace 并向该租户成员暴露 UUID/role/加入时间；可能带来骚扰、虚假归属和未来计费/通知副作用；Owner 的线下“已同意”声明不能由当前系统验证。未来资源权限或通知增加后，风险继续扩大。active 与未验证邮箱不能证明真实邮箱所有权或同意。

目前目标用户可合法从自己的注册响应、login/me 得到自己的 UUID，自愿在可信外部渠道交给 Owner；Owner 无权查询全局用户目录。即使知道 UUID，也不代表同意当前 Workspace；UUIDv7 可推测时间且不是访问密钥。不新增按邮箱查找、不信客户端 email→user_id、不给 Owner 全局搜索，不以日志、DB 查询或运维 SQL 作为产品发现流程。

备选比较：

| 方案 | 取舍 | 本轮结论 |
| --- | --- | --- |
| Owner 直接 POST 已注册 UUID | 简短但无可验证同意；成功/冲突仍泄露某些账号事实 | 不采用 |
| Owner 创建待接受申请，目标通过自己 Session 接受 | 能绑定接受者与目标租户；需生命周期、存储、防重放、过期及拒绝策略，不必依赖邮件 | 后续独立 Change 研究，不假定已批准具体邀请设计 |
| 先列表、移除、退出 | 可独立撤销权限，无加入与再加入能力 | 用户已确认 |

后续加入必须由目标当前有效 Session 表达针对具体 Workspace 的明确同意，最终角色固定 member；客户端传 owner 一律拒绝。无需因此立即实现邮件、完整邀请链接或邮箱验证，但接受记录/撤销/有效期等会改变模型，不能偷偷放进本轮。

**延期的直接 POST 候选契约，仅供审查取舍，不属于本轮 Spec/任务：** 请求若曾获批准，应严格仅 `{"user_id":"规范UUID"}`，拒绝 role/actor_id/email；Owner 事务内确认 active 目标，201 返回窄 MemberDTO；重复关系 409 `membership_exists` 且不改 role/created_at；不存在和 disabled 共用 404 `member_target_unavailable` / `Member target is unavailable.`；先授权 Owner 再查目标，Member 403、非成员404，避免未授权枚举。即便如此 Owner 可从201/409推断存在，错误合并不消除此残余泄露，且 UUID 分享不是已验证同意，因此不交付。POST 并非 exactly-once，未知提交后显式重试可能409；如需可靠请求重放还需独立幂等设计。不得把这个候选当作未来接受流程的现成 API。

原验收中的“Owner 添加、重复添加、目标不存在/disabled、退出后重加、添加与退出竞态”迁至后续加入 Change，**本轮不伪称已覆盖为产品成功用例**。本轮验证 POST 始终405且零 DB 调用；已有成员通过隔离测试夹具构造，不提供任何生产 seed/SQL 加入指南。

### 2. Owner / Member 权限矩阵

所有格子均以 active 账户及有效 Cookie Session 为前提；失效身份在格式正常的允许方法请求中统一401。

| 操作 | Owner | Member | 非成员/不存在 Workspace |
| --- | --- | --- | --- |
| 读取成员列表 | 200 | 200 | 相同404 workspace_not_found |
| 添加/邀请/授予角色 | 无接口405 | 无接口405 | 无接口405（不查询存在性） |
| 移除其他普通 Member | 204，含已不存在的目标关系 | 403 membership_forbidden | 404 workspace_not_found |
| 移除其他 Owner | 403 membership_forbidden | 403 membership_forbidden | 404 workspace_not_found |
| 主动退出自身关系 | 其他 Owner 数量≥1时204，否则409 last_owner_required | 204 | 404 workspace_not_found |
| 退出后重试 | 已失去成员资格，404 | 已失去成员资格，404 | 404 |

不允许 Owner 移除其他 Owner，以免本轮隐含所有权治理；Owner 自退可支持 schema 已允许的多 Owner，当前只由夹具构造。两个 Owner 互相移除均403；两个 Owner 自退仅一个可成功。Owner 自退与另一个 Owner 移除普通成员是不同操作，不将最后 Owner 冲突误用于所有403。

Owner 不变量是“存续 Workspace 至少一条 role=owner 的成员关系”，**不等于至少一名 active Owner**。账户禁用不删关系，最后 Owner disabled 可能使租户无法治理；恢复/转让属后续运维与账户生命周期设计，不以静默改角色/自动删除解决。若产品要求始终有 active Owner，需另行修订禁用协议与规划。

### 3. 两个接口与严格输入

| 路径 | 输入 | 成功 / 方法 |
| --- | --- | --- |
| GET /api/v1/workspaces/{workspace_id}/members | Cookie；可选单一limit/after | 200；Allow: GET |
| DELETE /api/v1/workspaces/{workspace_id}/members/{user_id} | Cookie + 同源CSRF；无query、无body | 204空体；Allow: DELETE |

候选三个路径中保留两个，不发布 POST。`POST /members` 固定405/Allow: GET，不新增JSON请求契约。角色变更 PUT/PATCH、HEAD/OPTIONS 和其他方法均405，HEAD无body、OPTIONS无CORS。集合 HEAD 不隐式转GET，避免 ServeMux 方法模式改变契约；额外段、空段、编码斜杠、尾斜杠按精确路由拒绝，不重定向后执行变更，不改已有资源路由行为。

退出复用 DELETE 的目标 UUID，自身 UUID 来自现有 `/auth/me`；服务器仅以 Cookie 查操作者，绝不从路径拿身份。相比额外 `/members/me` 或 `/membership`，复用一个资源删除契约可少一个路由与别名优先级；将来的UI可把自身UUID放入路径。路径 user_id 只是待删除关系键。

UUID 均要求36字符小写十六进制、标准连字符，不强制v7、不trim、不接受大写/紧凑形式。无效 workspace selector 在实时身份确认后同无权限/未知 Workspace 返回404；不得强转非法UUID触发500。目标 user_id 无效：在确认调用者属于Workspace后返回404 `member_not_found`，不查 users。Member 指向其他规范UUID：先403再做目标查询，真实存在/未知/disabled完全相同。Owner 对任何规范但不存在的关系204，不查全局用户是否存在或active；disabled普通成员仍可移除。

GET 分页沿用既有语法：limit默认50，规范十进制1–100；after规范UUID，对 user_id 升序 keyset；未知/重复键、非法百分号编码、空/不规范selector→400。游标仅排序，允许未知/其他租户UUID，不做存在性检查。最多返回limit条、取limit+1，无total；每页重新授权，变更期间无全遍历一致性或补齐保证。

DELETE 的任何query→400；非空body→400，无 Content-Type 要求，因为无JSON。必须有界检查至多1字节确认非空，客户端读体也受原5s预算/取消约束；未开始事务即拒绝，禁止读取无界body。GET body不用于身份、过滤或写入，沿用只读语义。所有变更都不接受 actor_id、role、session、workspace_id 等额外输入。

校验次序：方法 → DELETE CSRF → Cookie语法 → 廉价query/body校验 → 实时数据库身份 → workspace成员授权 → 目标格式/角色规则 → 目标关系 → 写入。格式正确但数据库未知Cookie可能先遇到输入错误，沿用既有边界；无效workspace与目标selector不能绕过实时身份401。数据库故障不能伪装404。

### 4. 响应、隐私与删除幂等

MemberDTO严格仅 `user_id,role,joined_at`；joined_at取membership.created_at、UTC RFC3339。不包含email/name/status、password/PHC、session/摘要、owner个人资料或全局账户创建时间；不改现有 WorkspaceDTO。disabled用户仍作为已有成员出现，但无status字段，避免泄露账户管理状态。

```json
{"data":[{"user_id":"019a5abc-1234-7123-8123-123456789abc","role":"member","joined_at":"2026-10-15T08:00:00Z"}],"pagination":{"next_after":null}}
```

分页越界为data:[]/next_after:null；合法Workspace按不变量通常至少有Owner，但不能用总数/空页暴露越权租户。数据根必须区分“已授权但页空”和“无身份/无membership”，不能以返回行数判断身份。

| HTTP/code | 固定message | 条件 |
| --- | --- | --- |
| 400 invalid_request | Invalid workspace request. | 参数、编码、DELETE非空体 |
| 401 unauthenticated | Authentication required. | 无效身份 |
| 403 csrf_rejected | Request origin could not be verified. | DELETE同源证明失败 |
| 403 membership_forbidden | Membership operation is not permitted. | Member移除他人或Owner移除其他Owner |
| 404 workspace_not_found | Workspace not found. | 未知/非规范Workspace、非成员 |
| 404 member_not_found | Member not found. | 已授权Workspace下非规范目标UUID，与全局存在性无关 |
| 405 method_not_allowed | Method not allowed. | 方法不支持，精确Allow |
| 409 last_owner_required | Workspace must retain an owner. | Owner自退会留下零Owner |
| 503 service_unavailable | Workspace operation could not be confirmed. | DB连接/处理预算失败、未知提交连接故障 |
| 503 database_timeout | Workspace operation could not be confirmed. | DB子期限、已确认statement timeout |
| 500 internal_error | Unable to complete workspace operation. | 缺schema、deadlock、序列化/未知错误、未证实超时的57014、panic |

错误严格为error.code/message，不回显ID、输入或内部诊断。响应带no-store/nosniff；有body时application/json，204无body/无JSON Content-Type，无Location。新接口任何结果都无Set-Cookie，不续期/清Cookie，不影响并发登录。日志只记录固定事件/原因/状态，可用固定路由标签；不记录原始路径/query、UUID、成员表、名称、email、headers/body、Cookie/token/digest、DSN/SQL/raw error/SQLSTATE/panic值。

DELETE 在状态层面幂等：Owner 再删已不存在的关系仍204。Member/Owner自退后再请求因失去成员资格为404，而非缓存204；这保持实时授权，[HTTP幂等允许重复请求响应不同](https://www.rfc-editor.org/rfc/rfc9110.html#name-idempotent-methods)。失效Session仍401。重复删除不能删除新一代关系的 exactly-once 语义本轮不承诺：未来若允许重加，旧DELETE可删除新关系，后续加入Change必须评审关系版本/条件删除；本轮没有重加入口。

无幂等键表或自动重试。只有确认Commit才204；超时/取消/断连/未知Commit不证明回滚。原客户端取消后停止发起后续业务或响应；尽力回滚并释放/丢弃连接。已提交但响应丢失的Owner可重新读取列表或显式DELETE，退出者可从自身Workspace列表确认已失去访问。后续请求401也可能由Session失效造成，不能等同删除确认。写前panic固定500、写后panic中断连接，敏感值不进入日志。

### 5. 读取的可信租户根与快照

沿用identity CTE：由Cookie digest联合sessions/users过滤active、未撤销、expires_at>statement_timestamp()；同一语句中匹配actor membership与指定Workspace，再LATERAL读取该Workspace成员页。身份根存在但无actor membership→404；无身份根→401；空页不能混淆两者。只选择DTO字段，不SELECT *、不全局查询用户/Workspace存在性、无缓存、无无身份GetMembers(workspaceID)捷径。

成员删除/降级、revoke/disable在读取语句快照前提交则立即生效；快照已建立的读取可以完成并返回当时授权数据，即使响应在撤销后送达。后续新请求/新分页快照重验，旧cursor/DTO/中间件UUID不授予权限。GET不加阻塞治理操作的行锁，无撤回在途响应承诺。[PG18 READ COMMITTED 快照](https://www.postgresql.org/docs/18/transaction-iso.html) 是此边界依据。

### 6. 写事务与稳定锁顺序

采用 READ COMMITTED，整段Begin、pool/行锁等待、检查、DELETE、Commit最多2s，嵌套现有5s处理预算。每次操作一个Workspace；所有支持的成员写及未来角色/加入写必须遵守同Workspace串行协议。各类资源顺序：**User（如多行按UUID升序）→ Session（如多行按ID升序）→ Workspace（如多租户按UUID升序）→ Membership（按user_id升序）**，禁止后取反向资源或锁升级。

本轮 DELETE 的具体协议：

1. 从digest无锁读取候选session.user_id，仅是身份线索。不得接收actor参数，也不据此授权。
2. 锁操作者 `users FOR SHARE`，读取并确认active。删除无需读取/锁定目标users，不因disabled/不存在用户产生全局存在性反馈。未来添加若锁目标用户，必须在任何Session/Workspace锁之前按UUID顺序一次取得全部User锁。
3. 锁摘要对应且user_id相同的 `sessions FOR SHARE`，确认存在且未撤销。FOR KEY SHARE不足以阻止status/revoked_at非键更新。两锁一直持有到结束。
4. 非规范workspace selector在身份有效性检查后404。规范selector先以actor membership限制候选Workspace，锁 `workspaces FOR NO KEY UPDATE OF w`。零行也要在新语句确认身份有效后才能404，避免等待后已过期仍报权限404。候选成员结果不是最终授权，禁止仅凭锁语句的join旧快照写入。
5. 取得Workspace锁后，用**新的语句**按身份/Workspace范围重读actor及target memberships，按user_id排序 `FOR UPDATE`。首先确认actor仍在Workspace，之后检查角色/目标；若需等成员锁，等完再发新语句做最终门控。Member删他人不必查询目标，可确定待返回403，但仍须通过最终新语句的身份有效性检查后响应；所有返回均不披露Workspace外信息。
6. 最终新语句重新验证actor active、session未撤销、`expires_at > statement_timestamp()`、actor membership当前role、target同Workspace条件。此时所有保护锁已获取，不允许在最后门控后再等其他业务锁。同一Workspace锁内的新快照统计当前Owner数：任何本应允许的删除/no-op若发现原来零Owner，固定500完整性错误且不写；目标为自身Owner且数量=1则409；不在锁外先count后delete。权限错误/未找到/幂等204也以此时有效身份与权限为前提。若actor在等待期间已被移除→404；降为member后删他人→403。
7. 在上述新语句/已受保护的紧邻DELETE门控中删除确切 `(workspace_id,target_user_id)`，绝不执行仅user_id删除。不存在的目标关系：已授权Owner可no-op；目标其他Owner固定403。检查context并Commit；只有确认Commit才发204。失败回滚，禁止内部重试。

推荐将最后身份时间校验直接放在DELETE或no-op决策语句，避免把门控时间藏在早期SELECT中；输出401/404/403/409的分型必须来自同一受保护状态。expiry是在取得全部可能等待的锁后的最终变更/决策语句时点有效，随后到期仍可提交；锁只保护行状态，不冻结时钟。后台清理可略过被锁Session，不能使过期Session有效。[PG18时间函数定义](https://www.postgresql.org/docs/18/functions-datetime.html#FUNCTIONS-DATETIME-CURRENT)区分事务开始时间、语句开始时间与实际时钟，因此必须在等待之后另发新语句。

选择 `FOR NO KEY UPDATE` 是为了与成员FK检查的Workspace KEY SHARE兼容，同时与其他成员写的相同Workspace锁互斥；本轮不修改Workspace主键或updated_at。避免先持Workspace KEY SHARE再升级FOR UPDATE导致不必要升级死锁。Membership锁统一先排序，状态查询不能依赖JOIN的优化器锁顺序；应以可审查的独立语句明确每一层锁。计数查询可利用PK的workspace前缀，无额外Owner索引或全表锁。

两个Owner自退：都先取得自己的User/Session共享锁；一个取得Workspace锁，count=2并删除自身提交；等待者取得锁后新快照count=1→409，最终必留一Owner。若首个回滚，第二个仍可成功。两个Owner互相删均403。Owner删Member与Member自退：Workspace锁串行；Owner先删→退出404；Member先退→Owner204。重复Owner删同一Member都204。不同Workspace仅可能因相同身份/Session状态变更竞争，不按全局tenant锁串行。

依据 [PG18 行锁冲突与一致取锁顺序](https://www.postgresql.org/docs/18/explicit-locking.html)。本项目选择短事务锁；SERIALIZABLE仍需定义冲突/重试，单独一条普通count/DELETE或普通事务不能保证跨行不变量；只锁现有Owner行不能锁“零Owner”集合。此处是需要PG18实测的设计，不是已获得的证明。

### 7. 与既有写入、角色变化及直接SQL的关系

| 路径 | 现有/要求的锁 | 兼容与风险 |
| --- | --- | --- |
| 注册 | 新User插入、email唯一性 | 不锁既有Session/Workspace，未提交用户不参与本轮删除 |
| 登录 | User FOR SHARE，后插新Session | 同方向；无Argon期间行锁，成员操作不占Argon槽 |
| 当前logout | 单Session UPDATE | 成员写先锁Session则logout等；logout先commit则成员401；不在Session后等User |
| cleanup | Session FOR UPDATE SKIP LOCKED，最多1000/2s | 无User/Workspace后向依赖，跳过成员写持锁行；先删则401 |
| Workspace创建 | User SHARE → Session SHARE → 新Workspace/初始Owner | 与成员写一致；新Workspace未提交时不可见，不锁已有租户 |
| 禁用（现无API） | 未来User UPDATE → 其Sessions按ID顺序撤销 | 必须先User，后Session；仅status改回active可能恢复未撤销Session的既有限制保持 |
| 移除/角色变化 | Workspace NO KEY UPDATE → Membership升序，在任何所需身份锁之后 | 先提交者影响后续门控；成员写先保护则变化等待。角色变化仅受保护PG夹具/未来经评审路径 |

不得让维护事务先Membership、再Workspace或先Session再User；未来多用户禁用须全体User排序后再Session排序。直接SQL修改role而不取Workspace锁会绕开同Workspace计数序列，不能算支持的写入协议；测试改变role时也必须明确采用协调协议。若为了验证未协调角色变更防御，行锁至少保证先持Membership者先结束，但不能据此声称所有裸SQL都安全。unexpected 40P01/40001返回安全500并回滚，无自动重试；测试受支持并发不得出现这些错误。

最后Owner依赖**所有受支持writer**：窄store接口、审查SQL路径、无生产seed/后台writer、运维变更必须先锁Workspace并验证不变量且不能后取User/Session。现有任意数据库写角色仍能直接DELETE/UPDATE/TRUNCATE破坏不变量；不修改实际数据库角色、不声称应用规则强制约束管理员。限制直连权限和审阅运维是运行规约，若要求数据库内强制不变量，则需另做版本4与完整方案。

数据库备选：普通CHECK不能安全约束跨行Owner数量；部分唯一索引最多保证“至多一个”；简单触发器count同样有并发问题；延迟约束trigger还需一致Workspace锁、提交时快照、创建时暂时零Owner、Workspace CASCADE/多行更新及TRUNCATE策略，且表owner/超级用户可绕过部分保护。此复杂度不在本轮悄然引入。参考 [PG18 CHECK跨行限制](https://www.postgresql.org/docs/18/ddl-constraints.html)。不使用RLS，继承当前应用隔离边界。

### 8. 主契约兼容与迁移

`workspace-access` 修改CSRF从POST扩展至DELETE、预算从创建事务扩展至成员事务，去除“无公共成员管理接口”的历史阶段限制；保留原所有场景并扩展，不扩大旧Workspace DTO。新名单投影由workspace-members独立持有，主投影Requirement应明确约束原创建/列表/详情响应，避免其“无其他用户”场景与新名单矛盾。

`workspace-persistence` 修改“只创建初始Owner、无其他writer”的阶段描述，允许经授权删除；新增最后Owner与写序列、未知结果契约。基础字段、PK/FK/CHECK、atomic create、Session门控和迁移规约不变。`user-authentication` 的最后Requirement已覆盖身份/租户权责，不必重复delta；Session主契约也不改。主Specs本轮不直接编辑，仅创建delta，后续归档再同步。

**无需新migration/index**：PK `(workspace_id,user_id)`支持列表、目标及Owner计数的租户范围；现有用户前缀索引继续支持Workspace列表。没有加入记录/幂等表/计数器；无需只为角色计数建立重复索引。迁移1/2/3保持原字节，SHA256见本轮检查记录：1=`e09f6476b25ce0a60cbd6607486a9b4208f2c3413957c83e54110e0e661b5490`；2=`27ce132375459ea1e061aac1e2c00de1010c9dc8f94c21489e7c24243f02c96a`；3=`327651e7727c512d80062792bab0ea874f1f63320b98ca6b0491dd8e6f94223d`。

### 9. 验收与CI计划

所有delta Requirement均含Success/Exception/Security/Concurrency。执行阶段记录Requirement→测试→实际证据映射；本轮只校验规划，不把历史测试当本次PG证明。

| 验收组 | 本轮必须覆盖 |
| --- | --- |
| 列表 | Owner/Member、单/多Workspace、最小DTO、disabled关系不暴露status、分页边界/空页/游标、未知参数 |
| 删除/退出 | Owner删Member、Member自退、Owner自退（2→1成功/1→0拒绝）、目标disabled仍可删除、Owner删不存在/随机用户ID204、非规范目标404、重复删除、自退重试404 |
| 权限/隔离 | Member删他人403（存在/不存在同响应）、Owner删其他Owner403、非成员/未知/非规范Workspace同404、伪造操作者/角色、跨租户路径与target ID替换、退出后旧Cookie读列表/详情/Workspace列表与再删 |
| CSRF/协议 | DELETE沿用完整Origin/头/FetchMetadata矩阵、零DB调用、Cookie缺失/重复/非规范/撤销/过期/disabled，POST与其他未支持方法405/Allow、HEAD/OPTIONS、body/query拒绝、no-Set-Cookie |
| PG18并发 | 两Owner自退两种获锁顺序（204/409）、两Owner互删403/403、重复删除、Owner删除与Member自退双顺序、角色变化与权限门控双顺序、不同Workspace隔离 |
| 身份/时间 | User/Session/Workspace/Membership各层等待后expiry；revoke/disable/cleanup双方顺序；持锁到期但门控已过可commit；新请求始终重新检查 |
| 故障 | pool/锁超时、安全503/500、schema缺失、40P01/40001分类、真实事务中删除后确认回滚、底层已commit但丢响应/commit前断开两种注入、取消/敏感panic/日志哨兵、连接恢复 |
| 全回归 | 健康/关闭、注册/登录/me/logout、绝对TTL/Cookie、清理、atomic create和Workspace读、原迁移链与约束测试 |

并发用独立真实连接、开始/到锁/门控/提交屏障，观察 `pg_blocking_pids`/pg_locks/pg_stat_activity，数据库时钟推进边界，不能只sleep后猜测。真实PG运行生产SQL；故障注入仅安排屏障/提交回执，区分真实锁证据与模拟传输失败。role变化通过guarded夹具执行相同锁协议，无新公开PATCH或owner授权接口。

沿用 `testdb.Open/ValidateURL` 限定 `devpilot_registration_test` 且区别DATABASE_URL；不读开发.env，显式集成缺URL/Goose/PG18失败，所有schema套件串行。未来分支push加入 `codex/add-workspace-members`，保留main/PR及原分支；PG18 digest、Goose和工具链不升级。CI执行gofmt、git diff --check、go vet、`go test ./... -count=1`、`go test -race ./... -count=1`、`go test -tags=integration ./... -count=1 -timeout=120s`、`go test -race -tags=integration ./tests/integration -count=1 -timeout=120s`，普通/race集成串行共享隔离库。若新增测试耗时确有证据需要增加总timeout，可评审调整而不减单请求预算或skip旧用例。

明确延期清单：Owner成功添加、重复添加、目标不存在/disabled加入失败、退出后再次加入、并发添加/移除与接受竞态属于后续加入Change；本轮对这些请求只验405、无副作用。不得使用fixture插入冒充已交付加入成功。

## Risks / Trade-offs

- [没有加入/再加入] → 用户已确认小切片；后续需目标有效Session明确同意并固定member，不能运维SQL补产品入口。
- [成员UUID向同租户成员可见] → 最小投影，无邮箱/账户status；待产品确认该可见性，数据一旦返回无法撤回。
- [同Workspace串行写增加争用] → 短事务2s、分页有界、不同租户独立；不为吞吐牺牲不变量。
- [应用协议无法阻止任意SQL破坏Owner关系] → 明确支持writer边界及维护顺序；若需要DB强制先修订规划/新增迁移。
- [disabled最后Owner仍占Owner关系] → 不承诺active Owner不变量，账户治理另行评审。
- [在途读取和已通过门控的写可以完成] → 明确快照/锁顺序与有效时点，后续请求重新验证，无权限缓存。
- [未知提交或退出后404不等于原操作回滚] → 不自动重试、不返回虚假204，提供重新查询/显式重试说明。
- [未来重加与旧DELETE竞态] → 加入Change必须考虑条件删除/关系版本，当前不承诺exactly-once。
- [公开部署防滥用未实现] → 保持既有认证公开发布前置要求；本切片不增加无界查询或全局用户探测，容量/请求预算不宣称完整限流。

## Migration Plan

1. 本次保存规划与严格验证，等待独立技术评审；产品范围已确认，但其余决策仍为推荐。明确新请求启动Apply后才实施。
2. 实施无需数据库升级，显式检查现有版本3，不改自动迁移/readiness规则；隔离PG18测试中验证原迁移链与哈希保持。
3. 部署新API前完成全部PG18/race回归和文档核对，保证所有支持成员writer使用统一锁顺序；不引入后台维护writer。
4. 回退应用保留当前schema及已完成成员变更；旧版本不会自动恢复已删除成员。删除后的用户在旧Workspace读接口同样失去访问，不能通过回滚应用绕过撤销。不使用down/reset或补偿添加恢复权限。

## Product Review Decisions

已确认：本轮不添加成员；后续加入需要用户同意。以下具体推荐供独立评审，未经接受不得在Apply中自由改选：

1. 名单向Owner/Member开放，字段仅user_id/role/joined_at，不含邮箱/昵称/状态。
2. 允许Owner在还有其他Owner时自退，禁止移除其他Owner；只保证Owner关系数量，不保证active Owner数量。
3. Owner删不存在关系204、自退后重试404；无幂等键和exactly-once保证。
4. 采用应用事务协议保证最后Owner，不做数据库强制约束或新migration；接受任意受信任SQL仍能绕开这一边界。

这些是具体评审契约，不是可在实现过程中搁置的Open Questions；评审变更时先同步全部规划产物。OpenSpec文件存在性完成不代表本次产品/技术评审通过或允许Apply。

## Apply Approval（2026-10-15）

用户明确确认六份规划产物通过独立技术评审、接受上述四项产品决策，并批准Apply及完成后创建指向main的PR。此前Propose阶段的等待描述保留为历史；当前不合并、不归档、不公开部署，等待独立代码审查。实施基线main=origin/main=70d53c7267b6e433be16ea020b4c2a1bd9b7d9ee，原未提交规划完整保留，分支codex/add-workspace-members。

# Tasks

用户于2026-10-15正式批准Apply；以下任务仅在实施与相应验证完成后勾选。依赖顺序为1→2/3→4/5→6；每组同时交付本组测试与文档。业务契约见三份delta，具体锁与查询协议见design.md。成员管理拆分已由用户确认，其余推荐契约已选定并列在Design末尾供评审；若评审更改须先同步规划。

## 1. Workspace schema 与迁移夹具

- [x] 1.1 新增版本3 Goose SQL迁移，创建workspaces与workspace_members及命名PK/FK/CHECK、UUIDv7/default时间、用户RESTRICT/Workspace CASCADE和(user_id,workspace_id)索引；真实PG18断言默认值、版本7、初始时间相等、同名可重复、非法name/角色/null/未知FK/重复pair均被拒绝，迁移1/2内容不变。
- [x] 1.2 修正registration/session集成夹具的latest=2、单次Down删sessions、两次Down删users假设，显式验证目标版本；测试0→3、保留用户/session数据2→3、up/up、v3 Down只删除Workspace、v2 Down保留users、可丢弃库完整roundtrip；隔离库外不得执行破坏性操作。
- [x] 1.3 将失败迁移夹具改为完整1/2/3加失败版本4，验证失败DDL和版本原子回滚、无半schema；保留缺URL/错误库名/应用库/非PG18/缺Goose硬失败断言，不放宽internal/testdb保护；同库schema测试不得并行。
- [x] 1.4 更新docs/database-migrations.md的版本3、升级/保留/回退规则和用户删除RESTRICT含义；核对README、status/up-only共享发布流程、单步Down仅可丢弃库、API无自动迁移及readiness仅连通性全部一致，并以本组真实PG测试结果验收。

## 2. 复用Cookie与CSRF协议

- [x] 2.1 窄范围提取现有环境Cookie名称选择和CSRF校验供Workspace与auth复用，继续使用session.CookieDigest；原auth/registration/config/token单元与合约测试保持通过，生产/开发Cookie、24h默认TTL、无续期、未知token与重复/带空白Cookie规则均不变。
- [x] 2.2 为Workspace POST添加唯一Origin/自定义头及Fetch Metadata组合测试，拒绝缺失/null/重复/后缀/子域/协议/端口/Host和Forwarded伪造/Referer-only；验证403前数据库调用计数为0、无Cookie副作用，GET无需CSRF且只读，OPTIONS不授CORS。
- [x] 2.3 在README/API认证说明中记录Workspace同源POST示例、GET Cookie需求、认证不等于成员权限、非成员404与缺身份401、无Set-Cookie；文档与2.1/2.2可执行协议测试交叉核对，不引入通用权限中间件。

## 3. 原子创建与身份并发协议

- [x] 3.1 在internal/workspace提供参数化短事务创建，按design的候选身份→user FOR SHARE→session FOR SHARE→新插入语句时间门控→owner插入→Commit执行；事务全程≤2s且受5s处理预算约束；PG18验证确认成功一workspace/一owner、第二插入失败两行回滚、其他连接不可见半创建状态。
- [x] 3.2 用受控连接/屏障验证revoke/disable分别先于和后于保护锁的两种顺序、cleanup删除/跳过保护Session、锁等待后过期401及插入门控后过期可commit；断言更新确实等待而非仅观察最终行，锁超时有界且无误授权，不用FOR KEY SHARE或固定sleep替代协调证据。
- [x] 3.3 注入取消、Begin/查询/第二写/Commit故障和“底层已commit但返回失败”结果，验证不发201/Location、不自动重试、不声称回滚、无额外业务写；真实PG分别验证已提交/确认回滚两种结果，连接归还后无悬挂事务，客户端断连不再尝试响应。
- [x] 3.4 并发创建同名Workspace验证独立UUID及owner关系，重复member pair测试验证数据库唯一性且不覆盖role；创建只接受摘要导出的user，不从请求或缓存取owner，结果不修改Session/用户；同步README/OpenAPI原子性、非幂等/未知结果恢复及重试可重复的说明并通过契约测试核对。

## 4. 同快照读取与租户拒绝

- [x] 4.1 实现identity根记录与授权资源关联的列表/详情查询，live Session、active、expiry与membership在同一语句快照校验，窄DTO不SELECT *，存储接口无无身份GetByID；PG18验证401/404/200空列表正确区分、每个响应只含有权读取字段，不能先查无约束资源存在性。
- [x] 4.2 建立A/B/C与X/Y/Z多对多夹具，A owner X/member Y，B owner Y/member X且独占Z，C无成员；真实PG测试各自列表/角色、A替换详情ID为Z、C猜测X、未知/非规范UUID、伪造身份/role、游标替换均无越权数据或写入，未知与非成员的完整错误相同。
- [x] 4.3 实现UUID升序limit/after分页（默认50、上限100、limit+1），单元测试参数/重复/非法编码/未知键并PG18验证稳定数据遍历无重漏、外租户或不存在cursor仅作排序、无总数/存在性探测；文档逐字段规定next_after/null及跨页变更语义。
- [x] 4.4 受控PG调度验证实际资源快照前后revoke/disable、成员移除和role变化，之前变更影响新读取、之前合法快照允许完成；两页间变化不得借旧cursor绕过新权限，GET不锁身份/成员写入或续期；相应并发语义纳入API说明，夹具不变成公开成员管理入口。

## 5. HTTP契约、输入与安全故障

- [x] 5.1 实现只含name的4096-byte严格输入与名称政策，覆盖空/null/数组/多值/非字符串/重复未知字段、UTF-8/原始surrogate、chunked/尾随空白/media/encoding，及1/100码点/400bytes、ASCII裁剪、Unicode边缘空白/Cc、paired supplementary/合法U+FFFD、SQL样式字符串；单元验证400/413/415/422和固定fields顺序，写入前廉价拒绝。
- [x] 5.2 接线三个端点到internal/app和cmd/api，输出201+Location/200 DTO/分页、固定错误表与safe headers；合约测试覆盖方法/Allow/HEAD空体/OPTIONS/路径额外段、非法UUID在live身份后统一404、query规则、所有响应无Set-Cookie及无CORS，确保新route不改变已有health/auth/未知路径。
- [x] 5.3 实现Workspace安全错误分类与panic/取消处理，避免注册email_conflict泄漏到Workspace；单元/PG18覆盖DB不可达503、确定超时503、缺表500、非timeout取消500、意外约束500、断连无响应、写前panic500/写后abort、同进程依赖恢复、5s/2s预算且不占Argon槽。
- [x] 5.4 为日志/响应添加token/digest/PHC/password/DSN/SQL/email/name/头/原始error/panic哨兵测试，错误不反射输入、日志只含安全固定类别，成功仅返回授权DTO；Workspace旧响应与新login重叠时没有Set-Cookie，保留现有Session隐私回归。
- [x] 5.5 随本组更新docs/api/openapi.yaml与README的全部请求/投影/状态/固定消息/校验顺序及Owner/Member矩阵，扩展现有合约测试核对每个HTTP状态、分页、Cookie/CSRF与文档，说明本轮无改名/删除/成员接口，不把角色存在误写成已开放治理权限。

## 6. 跨模块验收、CI与评审证据

- [x] 6.1 扩展真实HTTP+PG18端到端register→login→创建多个Workspace→列表→详情，混合member夹具与独立Session，复核缺失/过期/撤销/禁用Cookie401、CSRF403、跨租户404；回归健康/注册/登录/Session/cleanup，包括workspace v3 schema存在时的全部旧行为。
- [x] 6.2 CI现有push分支列表加入codex/add-workspace-foundation并保留main/PR、旧分支，沿用固定Go/PG18 digest/Goose；执行gofmt检查、go vet ./...、go test ./... -count=1、go test -race ./... -count=1、go test -tags=integration ./... -count=1 -timeout=120s、go test -race -tags=integration ./tests/integration -count=1 -timeout=120s；依据真实耗时审查timeout，如需调整记录原因，不skip失败用例。
- [x] 6.3 对照下方Requirement映射记录实际测试名、命令、PG版本和脱敏结果到verification.md，区分真实PG证据、受控故障注入与操作/设计评审；验证无业务越权、无敏感证据、无迁移1/2修改、无未来业务表/成员管理/Redis/RLS范围扩张，并完成openspec validate add-workspace-foundation --strict。

## Requirement 验收映射

下表是计划，不是已执行证据；每项需覆盖该Requirement的四类场景，细节不得由勾选某一最终任务替代。

| Capability / Requirement | 主要任务与证据 |
| --- | --- |
| persistence / Workspaces have database identities and bounded names | 1.1、3.4、5.1；PG约束/UUID/同名并发 |
| persistence / Membership represents a constrained many-to-many relationship | 1.1、3.4、4.2；FK/删除RESTRICT/CASCADE/复合唯一/多角色 |
| persistence / Workspace and initial owner commit atomically | 3.1、3.3、5.1；真实两写事务、观察隔离、拒绝注入 |
| persistence / Creation coordinates with current identity state | 3.1–3.2、5.3；锁顺序、两种commit顺序、过期门控 |
| persistence / Unknown creation outcomes are not retried automatically | 3.3–3.4、5.3；真实commit后故障注入/回滚/断连/重复提交 |
| persistence / Workspace migrations are additive explicit and recoverable | 1.1–1.4、5.3；0/2→3、失败4、回退保留、缺表与操作审阅 |
| access / Workspace creation accepts only a bounded name | 5.1–5.2；严格输入、并发状态隔离 |
| access / Display names have a deterministic Unicode policy | 1.1、3.4、5.1；Unicode/参数化/同名 |
| access / Workspace success responses have a minimal projection | 3.3、4.1–4.2、5.2、5.4；DTO/Location/无Cookie |
| access / Workspace lists are bounded and user scoped | 4.1–4.4；分页、跨租户cursor、当前权限 |
| access / Workspace detail is authorized without existence disclosure | 4.1–4.2、4.4、5.2；相同404/IDOR/成员竞态 |
| access / Workspace reads use live identity and tenant checks together | 4.1、4.4、5.3、6.1；同快照、401与空列表、fail closed |
| access / Workspace mutations reuse the same-origin CSRF protocol | 2.1–2.3、5.2；来源矩阵与零DB调用 |
| access / Workspace responses and processing fail safely | 3.3、5.2–5.5；错误分类/预算/日志/panic/方法 |
| access / Tenant boundaries and acceptance remain verifiable | 1.3、4.2、6.1–6.3；强制PG18/跨租户矩阵及未来租户原则设计审阅 |
| auth / Authentication does not grant transaction-level Workspace authority | 2.3、3.1–3.2、4.1–4.4、6.1；身份与资源权限分开、真实PG协调 |

## Workflow follow-up

- 独立技术设计评审已通过，用户已正式批准Apply及创建指向main的PR；实施完成后等待独立代码审查，不自动合并、归档或公开部署。
- 严格保留已审查契约；如实施发现需要改变契约，先说明并同步规划，经确认后再继续。
- Apply完成不自动代表发布、PR创建/合并、同步主Specs或归档获授权；这些步骤按后续明确请求执行。

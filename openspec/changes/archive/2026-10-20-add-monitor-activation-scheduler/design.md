# Design

## Context

动机见 proposal。2026-10-19 核实初始工作区干净，处于 main；HEAD/main/origin/main 及连接器 `GET https://api.github.com/repos/rjgczp/devpilot/git/ref/heads/main` 均为 `2cfdcd6c0c094e938fc0570b67697346c3da7aee`。普通 Git 远程读取因代理 `127.0.0.1:7897` 不可达失败，连接器补核实成功；未 fetch/reset、切换分支或覆盖文件。

归档共九项：bootstrap-go-backend、add-user-registration、add-user-login、add-workspace-foundation、add-workspace-members、add-workspace-invitations、add-project-management、add-http-monitor、add-monitoring-engine；初始 active changes 为空，主规格15份。重点读过三个 http-monitor 主规格及场景、最后两阶段提案/验收、配置 Store/Handler、成员退出和 Session 清理锁协议、Migration6、执行器 reader/README、Compose、CI 和网络 runner 说明。

观察到的边界：

- `internal/monitor/store.go` 使用有界 READ COMMITTED 写事务，锁 User SHARE → Session SHARE → Workspace NO KEY UPDATE → actor Membership UPDATE → Project UPDATE → Monitor UPDATE，全部等待后由新语句门控实际 DML。PATCH 同值也递增 version；Commit 未确认不返回成功且不自动重试。
- `internal/workspace/member_delete.go` 通过 Workspace 锁串行化 owner 行数，禁止最后一名 owner 正常退出；这不等于保证每个 owner 的 user.status 永远 active。Session 撤销/清理只取 Session 锁，不反向请求业务父行。
- `internal/monitoring` 是私有休眠内核；API 不导入它，没有生产构造/开关/Worker。SnapshotReader 的完整快照不等于激活授权，进程 admission 的32/4/2/1不等于集群限额。
- `compose.yaml` 只有 PG18。`tests/execution` 的 network-none/iptables/DNS/TLS/PG 证据属于可控 fixture；并不存在可审查的生产 Kubernetes/VPC/Worker 出口部署。不得将归档的通过当作本次实现或生产验收。

## Goals / Non-Goals

**Goals:** 推荐一个可独立验收的 A 协议；将用户意图、配置版本、实际执行及出口许可分开；为配置变化、权限竞争、响应丢失和未来执行门禁给出可验证边界。以下数值和产品行为均是本次供独立评审的具体推荐，尚非 Apply 授权。

**Non-Goals:** 本轮不编写业务代码、SQL Migration 或 Worker，不执行迁移/生产探测/部署/PR/激活。A 的后续 Apply 也不实现周期扫描/领取/租约/运行记录/网络入口。Incident、Alert、通知、AI、前端、SLA、计费、自定义认证/请求体、内网监控、无界历史及无关身份重构排除。

## Decisions

### 1. 范围比较与推荐

| 评估项 | A：意图与状态基础（推荐） | B：激活、调度、受控 Worker（未批准） |
| --- | --- | --- |
| 独立价值 | 明确版本授权/暂停、审计、原子失效，API 可验收；不承诺可用性监控 | 可提供持续探测及最小结果，但必须先有生产出口、安全控制面及故障协议 |
| 用户限制 | enabled 只是意图，固定显示 blocked/scheduler_unavailable；没有健康数据 | 必须解释在途请求不可撤回、结果未知、调度延迟及公平上限 |
| 复杂度 | 单资源 CAS、现有授权事务、初始化/失效 trigger | 分布式状态机、start 门控、fencing、配额/公平性、沙箱生命周期及故障注入 |
| Migration7 | 只建 activation 子表、索引及窄触发器；无租约/结果 | 另需 run、预算、控制面、受限角色/函数及清理索引；不应预埋在 A 的7中 |
| API | 一个 GET/PUT activation 子资源，原配置响应不变 | 除意图外还需受权运行状态/结果读取；不把 worker lease 暴露为用户能力 |
| DB 竞争 | 延续 Workspace 串行写；有界 enabled 计数，无全局热点 | 批次/预算热点、回收和发布竞争，可能影响 API；需压力及锁证据 |
| 生产安全 | 没有执行入口，未达门禁仍安全关闭 | 独立出口默认拒绝、无凭据 probe、fence 可撤销性及运维职责全部是上线前提 |
| 测试成本 | PG18 迁移/权限/CAS/race + 原零目标审计及第九阶段回归 | 增加多进程故障/真实网络/配额/租约/外部 fence/真实部署级负向控制 |

**结论：推荐 A。** 当前没有证据证明 B 的生产出口；不能通过增加环境变量或在 API 内调用内核“先跑起来”。本次 deltas/tasks 只覆盖 A；本文件第7–11节的 B 研究不能被解释成实施清单。引入 Worker 必须单独 Change、独立安全评审、明确授权和具体环境验收；升级时清除全部 A 的 enabled 意图、推进代次，再要求 Owner 重新确认，避免多年后突然开始请求。

### 2. 状态机、代次和系统授权寿命

`http_monitors.version` 只表示配置修订；`activation_generation` 是独立的正 bigint、永不回绕。`desired_state` 仅 enabled/disabled，不复用它表示 claimed/running/healthy。不存在隐含的“创建即启用”。

| 输入 | 条件 | 原子提交后的状态 |
| --- | --- | --- |
| 历史回填 / 创建 | 不接受客户端启用字段 | disabled，activation_configuration_version=NULL，g=1，reason=never_activated |
| Owner enable / 恢复 | 强 activation ETag 与当前 v/g 一致；body 显式确认 v；配额/冷却通过 | enabled，授权 v，g+1，reason=owner_enabled；A 仍 blocked |
| Owner pause | 当前 Owner、正确 v/g 和 body v | disabled，授权版本清空，g+1，reason=owner_paused；暂停不受 enable 冷却/数量限制 |
| 任一成功配置 PATCH | 仍按原 monitor ETag；名称/URL/interval/timeout/status/method及同值都适用 | v+1，同时 disabled，授权版本清空，g+1，reason=configuration_changed |
| 失败、已知回滚 | 任一门控失败/溢出 | v/g/意图/冷却/审计均不变 |
| 服务重启或部署 | 不接受新命令 | 只保留意图，A 永不产生 due/run；没有遗漏任务补偿 |

同值 enable 或 pause 若用**最新** tag 提交也推进 g，属于一个新命令；相同旧 tag 重放若前一次成功则412。不是返回“上次成功”的幂等 API；一个版本最多被一个命令消费。两 Owner 同 tag 的最多一个成功；enable→pause→enable 的 ABA 无法复活旧 tag。v 或 g 达 bigint 最大值时安全500、整体回滚，无半次失效。

配置 PATCH 与 enable 共用 Monitor → Activation 锁：PATCH先提交，旧tag enable412；enable先提交，后续合法PATCH必使其失效。pause 和 PATCH 竞争，若PATCH先提交则旧pause412，但监控已 disabled；客户端需重新读取，服务端不得自动刷新或重放。

已确认授权属于 Workspace 的系统意图，不是创建者/授权者 Cookie 的长期借用。一个 Owner 正常退出，若还有有效 Owner，意图保留；前 Owner 的新写立即失权。退出不能暗中删除意图，也不能使重入 Member 恢复写权。撤销 Session 不撤销此前的 Workspace 意图。异常地没有 active User 对应的当前 Owner 时：A 仍 blocked，不伪称在运行；未来 B 必须拒绝新 start 并尽力取消。现有 owner 行数约束不足以证明 active-owner 恒真，B 必须给出用户禁用与 start 的共同保护协议，不能在锁住 Workspace 后反向锁全部 owner User；本次不重构这些身份路径。

保守失效策略的成本是改名字也要重新确认，既有客户端同值PATCH会暂停。其好处是无需证明哪些字段“影响安全”，v/g 谱系单一且不可被旧授权绕过。API 文档必须说明这个行为；产品若希望名称变更例外，应在 Apply 前重新评审规格，不能实现时自行放宽。

### 3. Migration7：独立意图表与有界审计

推荐独立 `monitor_activation_states`，不扩展 `http_monitors` 为混合配置/运行大行。扩展原表虽少一次 JOIN、可共用行 CAS，却让高频 Worker 更新争用配置、膨胀原 DTO/ETag，并混淆授权和健康；独立表以一对一 scope 为界，同时让未来 Worker ACL 更窄。代价是必须保证初始化完整、配置失效跨表原子、固定锁序。

拟议字段（**仅设计，没有 SQL 文件**）：

| 字段 | 类型 / 约束 / 语义 |
| --- | --- |
| workspace_id, project_id, monitor_id | uuid NOT NULL；PK三元组；复合FK引用 http_monitors(workspace_id,project_id,id)，ON DELETE RESTRICT |
| desired_state | text NOT NULL DEFAULT disabled；CHECK仅disabled/enabled；用户意图 |
| activation_configuration_version | bigint nullable、非空时>0；disabled要求NULL，enabled要求非空；授权的版本而非当前配置的替代 |
| activation_generation | bigint NOT NULL DEFAULT1、CHECK>0；每次命令/配置失效+1，不因暂停/冷却/部署归零 |
| next_due_at | timestamptz nullable；A CHECK(next_due_at IS NULL)，不是可执行任务 |
| next_enable_allowed_at | timestamptz nullable；enable成功设置为max(原值,本次DB语句时间+60秒)；pause/PATCH不得清空或缩短 |
| last_transition_kind | text NOT NULL；never_activated/owner_enabled/owner_paused/configuration_changed |
| last_actor_user_id | uuid nullable FK users(id) DELETE RESTRICT；owner命令为可信actor，默认/配置失效为NULL；不引用Membership或Session |
| created_at, updated_at | timestamptz NOT NULL；DB时间、updated_at>=created_at且不递减；回填以本次迁移时间创建状态，不改原Monitor时间 |

CHECK绑定 desired_state/授权版本/transition_kind/actor 的允许组合：enabled只能owner_enabled且actor及next_enable_allowed_at非空；owner_paused为disabled且actor非空；默认/configuration_changed为disabled且actor为NULL。跨表 `authorization_version=current_version` 不能用跨表 CHECK，enable 的最终 DML 在持有 Monitor 锁时比较；配置失效 trigger 是持久化保障。禁止普通 SQL 修改子表 scope/created_at，禁止单独删除状态行绕过代次/冷却；状态guard对实际变更要求g恰为old+1、updated_at非递减、next_enable_allowed_at不得清空/缩短（旧值NULL可初始化），拒绝重置/跳回；不建立裸 monitor_id 的子引用。

必要索引：PK三元组支撑 scope JOIN；部分索引 `(workspace_id,monitor_id) WHERE desired_state='enabled'` 支撑配额计数；`last_actor_user_id WHERE last_actor_user_id IS NOT NULL` 支撑用户 FK 检查。A 没有 due/lease/history 扫描，不预建无用索引；未来 B 增加实际 due partial index 时另评估。

Migration7 在一个 Goose事务内为迁移窗口锁住 Monitor 的并发 DML，建表/约束，按所有历史 Monitor 回填 disabled/g1，安装 AFTER INSERT 初始化和 AFTER UPDATE 配置版本失效触发器，再原子提交；避免回填与新建之间缺行。不能用异步懒创建把缺行解释为可启用。Monitor 创建在同一提交内有默认行；失败则不留Monitor。配置版本失效 trigger 更新已存在 activation，缺行/溢出即报错，配置与意图一起回滚。它覆盖旧API二进制的合法PATCH；Store 不再第二次增加 g。普通SQL修改配置字段却不推进version、或version实际变化不是old+1，应由新增窄guard拒绝；不把此guard当作完整Owner授权。对同值且不改version的无关SQL赋值不伪造新的配置修订。

trigger只沿 Monitor→Activation，不读取/锁User/Session/Workspace/Project，不访问目标网络。配置失效 actor=NULL，避免对任意其他User新增反向FK锁；Owner命令的actor已在最早取得User SHARE保护。初始化不复制URL。DBA/超级用户可停trigger或修改数据不在普通SQL防护保证内。

**运行字段取舍：** `lease_owner/lease_until/run_id/attempt_state/fence/execution_snapshot` 在 A 均不建，不能用未验证的空字段暗示可靠调度。最末状态一行覆盖，不保存每次命令历史或探测结果；retention为Monitor生命周期，无周期cleanup，行数正好随Monitor数增长，不随命令次数增长。这不是不可抵赖的全量审计。仅保留最后命令actor/原因/时间/g以解释当前意图；不公开actor/Session/URL。

### 4. API、ETag、错误和权限

相对 `/api/v1/workspaces/{wid}/projects/{pid}/monitors/{mid}` 新增 `/activation` 子资源：

| 方法 | Owner | Member | 非成员/混合scope | 无效身份 |
| --- | --- | --- | --- | --- |
| GET /activation | 200 | 200 | 404 monitor_not_found | 401 unauthenticated |
| PUT /activation | 正确条件时200 | 403 monitor_forbidden | 404 monitor_not_found | 401 unauthenticated |

用 PUT 同一资源表达启用、暂停和恢复，避免三个动作路径的重复协议。PUT body仅允许两个必填字符串：`desired_state`（enabled/disabled）及 `configuration_version`（规范正十进制bigint字符串）。即使暂停也要显式确认当前v。最大1024原始字节；复用现有JSON scanner原则，拒绝null/重复/未知键、额外JSON、错误类型、无效Unicode、指数/前导零版本。单application/json可附UTF-8 charset，不接Content-Encoding；结构400、字段枚举/范围422、超限413、媒体415。GET拒绝body；GET/PUT均不接受query；其它方法405 Allow: GET, PUT，HEAD不带body、OPTIONS不授予CORS。路径别名/RawPath/点段等404无重定向。

GET和确认PUT返回data，字段精确为三元组、`configuration_version`（当前v）、`desired_state`、`activation_configuration_version`（字符串或NULL）、`activation_generation`（字符串）、`next_due_at`（NULL）、`next_enable_allowed_at`（UTC时间或NULL）、`last_transition_kind`、`created_at/updated_at`（UTC RFC3339Nano）、`execution_state=blocked`、`blocked_reason=scheduler_unavailable`。disabled和enabled都表示执行入口未开放，disabled还明确没有意图。没有URL、创建者、actor、lease、result、health、运行次数或“监控中”字样；旧配置DTO仍不添加状态。

两者强ETag为 `"monitor-activation:<mid>:<current_configuration_version>:<activation_generation>"`，最大128字节。这个tag覆盖整个A稳定表示；字段是持久化值和常量，不放实时倒计时/可变外部gate/其它owner状态，避免同tag表示不同内容。PUT必须单一强tag；wildcard、weak、重复、多值、非规范UUID/正bigint、超长为空400。GET带If-Match或任意请求带其他条件头400，无条件缓存语义。

语法校验后的SQL门控顺序：身份401 → 完整scope/成员404 → 当前Member403 → 缺tag428 precondition_required → tag的mid/v/g或body v不匹配412 precondition_failed → enable配额409 activation_limit_reached → enable冷却429 activation_rate_limited（Retry-After=向上取整剩余秒数） → 溢出/不变量500。错误固定 `{error:{code,message}}`，仅422含固定fields；错误不带data/ETag/Location或当前版本。CSRF与路径/方法拒绝保持既有前置安全语义，不能据此跳过Store授权。

Cookie-only、精确Origin、proof、Sec-Fetch-Site必须复用现协议；无Host/Referer/Forwarded/Authorization替代。请求预算5秒、DB读/整写事务2秒、早拒绝drain有界；响应no-store/nosniff，无Set-Cookie。依赖/确认超时503，schema缺失/内部错误500，取消不继续响应。只有确认Commit后200；未知Commit可已生效但不给成功，客户端先GET再决定新的命令，不自动重放/猜测。旧tag只有明确回滚时可能成功重试；已提交则412。既有配置monitor tag、返回字段和Project version不受意图命令影响。

### 5. 实际授权、锁顺序及配额

Owner命令锁：User SHARE → Session SHARE → Workspace NO KEY UPDATE → actor Membership UPDATE → Project UPDATE → Monitor UPDATE → Activation UPDATE。创建和配置PATCH延续原链，trigger只追加Activation。按最终锁等待后一个**新语句**读取active User、token digest绑定Session且expires_at>statement_timestamp()、当前Membership owner、完整三元组与v/g；这些条件必须出现在实际UPDATE门控中，不能只靠Handler/锁前SELECT。identity解析不授予权限。状态GET用单语句快照关联同样实时身份/完整scope。

User和Session锁保护撤销/禁用；Workspace保护退出/成员改动及跨Monitor激活计数；Membership generation及当前role沿用现有语义，不把“退出又加入Member”当旧Owner。退出、撤销、Session清理先提交则新门控失权；意图写先保护可以先提交。任一级等待中Session过期，由最终新语句时间拒绝，不能使用事务开始时间 `now()` 冻结时效。

每次命令只锁一scope。同类多行若确有需要按稳定ID排序。没有Activation→Monitor/User/Workspace反向路径，没有子锁后读写其它actor，无弱锁升级。Session清理 `SKIP LOCKED` 只操作Session，没有等待Activation的环；撤销只改Session。Project PATCH、邀请、成员写沿既有Workspace保护串行化。FK/trigger隐式锁也纳入PG锁观测；死锁/取消整体失败，不自动重放业务请求。A不引入全局quota行锁。

推荐每Workspace最多100 enabled，enabled→enabled不增加计数但仍消费g和冷却；Workspace锁后新语句以部分索引统计，任何enabled写必须持相同Workspace锁。不存在同时第100/101个都通过的count-before-lock。pause/PATCH减少意图数且不受配额/冷却限制，不能把紧急暂停挡在429后。每Monitor enable最少间隔60秒；数据库持久化next_enable_allowed_at且配置/代次变化/重启不重置，两个实例共享。失败请求不消费g/冷却。API进程还复用有界请求/连接池，不能声称这给未认证请求提供了分布式QPS保障；网关按身份/来源的通用滥用限流属于部署要求。A 无网络调用，此限额约束意图存储和管理滥用，不冒称限制真实目标流量。

### 6. A 的持久化调度边界与运维停止

A不扫描enabled记录、不生成到期任务，next_due_at始终NULL。重启、停机、未知意图Commit和配置更新都不会产生补偿任务。常量blocked不是可被环境变量/DB开关解除的状态。即使误改desired_state也不能从不存在的调用路径产生GET。API依赖图继续无monitoring，所有意图/配置路由列入零目标网络审计。

需要独立紧急停止能力，但A本身通过无生产执行入口实现恒停止，不新增可解锁开关或运维HTTP API。未来B必须具备控制面 stop及独立网络出口切断两条路径；控制面审计记录操作者、固定原因、单调epoch、时间和确认状态（无目标细节），有界保留30天/总量上限，清理受限批次。恢复不能自动enable旧意图，应重新评审并重授权；单纯“stop写入DB”不能保证失联Worker已停止。

### 7. B 研究：领取、开始、租约与故障（不在 A Apply）

候选模型：一个Monitor最多一个非终态run，run_id数据库UUID、完整scope、configuration_version、activation_generation、scheduled_at、claim fence、lease_owner/lease_until、状态leased/started/completed/canceled/unknown；start次数单独记录，不能把claimed当已发GET。fence是单调bigint，与activation_generation分别维护。DB生成UTC租约/到期时间，Worker仅用单调时钟计算剩余deadline；不比较不同机器wall clock决定有效性。

建议协议待后续验证：

1. 多实例每秒tick、每批最多8项、每tick候选最多32 scope，扫描有界且每实例在途数有上限；不是为每个due monitor创建goroutine。先无锁读候选hint；claim事务必须父先子后，不可先对Activation SKIP LOCKED再回头锁Monitor。可以先锁候选Workspace NO KEY UPDATE SKIP LOCKED（单次最多一个Workspace），按既有顺序锁Project/Monitor再Activation/run，锁不到立即跳过；分别取得最终所需锁，不靠JOIN文本顺序推断锁顺序。所有等待有超时。
2. 在短事务里复验enabled、授权v/g等于当前、到期、全局stop/部署许可、容量，分配UUID/fence、保存必要的不可变执行快照并确认Commit；释放全部DB资源后才允许后续执行。未知claim Commit，原Worker不执行，持UUID作只读对账也不自动开始，留给受限恢复器。
3. 另一个短start事务重新锁相同父链与Activation/run，复验owner存活策略、v/g、fence、lease、stop和配额，CAS leased→started；**start事务提交是“执行开始”的线性化点**。确认Commit才向隔离执行单元交付一次性许可；start Commit未知则不DNS、不dial、不GET，标记待对账/unknown，不能因看到started就擅自重发。
4. 未started租约建议15秒、可在到期后仅有界重领一次（新fence/新owner），旧owner必须start CAS失败；首次claim起总寿命最多60秒，超出则关闭并重新安排未来一个周期。started租约建议timeout+15秒、一次attempt不续期、不重领重试GET。硬网络沙箱期限建议timeout+5秒，任何回收必须先获得外部网络许可撤销/沙箱终止证明；只有lease_until已过不等于旧进程消失。
5. 完成事务以scope/run/v/g/fence/owner和合法状态CAS保存唯一终态。响应丢失时可幂等重写**同一结果**，不得重做网络。运行结束/安全回收后next_due取max(DB当前时间+interval，持久化cooldown)，绝不逐个补跑历史tick；数百万遗漏周期也只有每Monitor一个未来到期点。backoff/无容量不排队。

上述SKIP LOCKED只适合工作领取，给出不完整视图并不保证公平，也不跳过表锁；不能用于用户读取/实际授权。SQL应在显式父锁后新语句重新判定，避免READ COMMITTED单语句关联快照和锁等待后更新行混用。依据 [PG18 SELECT](https://www.postgresql.org/docs/18/sql-select.html) 与 [PG18事务隔离](https://www.postgresql.org/docs/18/transaction-iso.html)；本方案是据这些语义作出的设计推导，尚无B实现证明。

| 故障 | 建议处置与保证边界 |
| --- | --- |
| claim前崩溃/事务回滚 | 没有已确认许可；后续有界扫描可领 |
| claim提交确认丢失 | 原Worker零执行；恢复器仅恢复未start任务并增fence，次数有界 |
| start提交成功但确认丢失 | 可能started但未GET；原Worker禁止执行，后续归unknown，不把记录当可重放指令 |
| GET已发、完成写前崩溃 | unknown，远端可能有副作用；同run不重发，不伪造失败结果 |
| 完成提交确认丢失 | 按run/fence对账；只重试相同终态存储，下一周期不补偿这个GET |
| 超时与lease回收竞争 | 旧fence不能写；回收started槽前须证明旧执行无网络能力，否则隔离占槽并告警 |
| DB不可用/网络分区 | 停止新claim/start；已确认start按本地硬期限和外部出口许可结束，不能离线续租 |
| Worker失联/暂停后恢复 | 不信其本地time或旧token；失效fence只防DB写，网络许可须在外部同步拒绝 |

**不承诺HTTP端到端exactly-once。** claim可对同一未started任务有界at-least-once交付；start CAS对一个run至多一次，started的网络尝试采用at-most-once交付策略（可能0次），结果写入按同一run/fence幂等。未来周期仍会再次访问同目标，不是目标语义的at-most-once；不同Monitor也可同URL。GET可能产生副作用，不能因“幂等方法”补偿未知执行。

### 8. B 研究：暂停与 v7→v8 的竞态证明

普通数据库行锁在事务结束释放。依据 [PG18行锁](https://www.postgresql.org/docs/18/explicit-locking.html)，start、pause和PATCH若按同一父链冲突锁串行化：pause/PATCH先提交则后来的start在新语句观察disabled或v/g失效，CAS失败且零目标DNS/连接；start先提交则它已被定义为在途。没有任何业务行锁跨越DNS/TCP/TLS/HTTP。

| Owner变化时Worker位置 | 期望行为 |
| --- | --- |
| 尚未claim | 不可领取 |
| 已claim v7，尚未start提交 | v8/Pause锁后使g前进；旧start失败，清理claim，不访问网络 |
| start事务正在等待Monitor | 等待后新语句必须看到v8/disabled；不使用锁前snapshot继续 |
| start已确认，尚未DNS/dial | 已属在途；通知/轮询尽力取消、交付前/拨号前再检查减少窗口，但不能宣称这些检查原子覆盖发送 |
| 已TCP/TLS，尚未GET | 关闭连接可阻止尚未写入的负载；与写入竞态无全局零字节保证 |
| 已发送GET、等待响应 | 取消/关闭不能撤回远端副作用；结果可丢失/unknown |
| 旧v7/g结果晚到 | 只允许写对应run；若未来有current pointer，必须在同事务匹配当前v/g且enabled，不能发布为v8/新g健康 |

**关键限制：** start提交与真实socket系统调用之间仍有窗口，start在pause之前提交的任务可能在pause之后才dial/GET。数据库CAS、二次SELECT、LISTEN/NOTIFY或本地mutex都不能消灭它；通知不可靠，仅是取消优化。若产品把“尚未开始”定义为“尚未向网络发送”，则单凭PG短事务无法满足“pause返回后绝不新发GET”的强语义。B必须引入可同步撤销且确认的外部出口许可/执行栅栏，或者产品明确批准以上以start提交为界的语义；两者未决时保持B关闭，不能靠持业务锁做网络I/O。A完全无网络，不受此未决条件影响。

### 9. B 研究：共享预算、公平性与公网滥用

未来预算建议集群32、Workspace4、Project2、Monitor1，及全局32/s burst32、Workspace4/s burst4、Project2/s burst2，均由PG持久化许可/token bucket维护，原单进程admission继续作为额外保护。Monitor持久化last_start/next_allowed不少于3600秒，变version/g/interval不清空；last_start+新interval与旧next_allowed取最大。网络前被批准的尝试即消耗rate，不因blocked/canceled返还速率。

候选统一锁序在业务父链/Activation/run之后追加 GlobalBudget → WorkspaceBudget → ProjectBudget（同类ID排序），全部参与者不得持budget再请求业务父行。API不拿这些执行预算；claim/start/完成/回收都先声明scope再沿同链取预算，避免全局budget→Workspace与API路径成环。粗粒度全局行将串行化短事务，是32并发MVP可接受的研究起点；压测不达标就调范围或另评估分片，不能未审批引入Redis/队列。只用 `count(active_leases)` 的无锁快照不成立。

许可账本可严格限制有效逻辑槽，但过期租约/fence**不能证明物理网络并发**。僵尸worker仍可能发包；未证明其外部网络许可撤销前不得返还槽，即使牺牲可用性。启动前还须核验许可新鲜度、stop epoch和沙箱硬期限。DB时钟回拨不缩短next_allowed，异常向前跳变不能直接安全回收started槽；以 [PG18时间函数](https://www.postgresql.org/docs/18/functions-datetime.html) 的statement time作授权门控、Worker monotonic作本地预算，额外处理时钟跃迁/主从切换。B验收必须在多进程真实接收端统计流量，而非只看counter。

公平性建议有界Workspace轮转/持久cursor，每批最多每Workspace2项、每Project1项，忙租户最多4 active；每tick只取有限候选，不能按全局due排序让一个Workspace永久占据候选页。压力验收在明确租户/负载上限下证明其它租户可取得剩余资源，不承诺无限租户严格公平。A的100 enabled上限沿用；执行侧还需批准的最大活跃Workspace/预算状态容量，容量满安全拒绝。全局同origin风险不能只靠租户配额解决：公共目标可能由多账户/重复Monitor高频访问；B上线须另批准跨租户origin速率预算及受限target-key保留/隐私策略，未形成可验收政策前保持关闭。

### 10. B 研究：最小运行记录、谱系与清理

A不保存任何运行记录。B若需去重/恢复，推荐有界 `monitor_runs`，不建健康历史分析表。至少包含完整scope复合FK、唯一run_id、configuration_version、activation_generation、scheduled/claimed/start/finish时间、monotonic duration、fence/lease owner/until、attempt_state、nullable final http_status、nullable success/fixed failure_code、retention截止。未start/unknown不能伪装成success=false的HTTP失败；started_at表示授权开始，真实网络起点另以执行器观测记录，不混为同一事实。

必要执行快照在同一claim事务来自锁住的完整配置；method/interval/timeout/expected_status可以持久化，**不持久化URL（尤其path）、DNS答案、headers/body或raw errors**。URL只在受信controller内存中冻结并交给一次性probe；controller崩溃后未start任务可在v/g仍匹配时重新读取，started未知任务不能为恢复URL而重发。run_id/v/g/fence足以标识谱系，无需保存秘密配置历史。

研究保留：每Monitor最多32条终态且不超过24小时，以先到者清理；另有最多1条非终态。新claim前在同scope有界清理/检查，清理失败或记录已满拒绝新建，不能依赖定时清理最终追上增长。非终态过期要先按外部栅栏证据关闭为unknown，不能直接删除并释放容量。终态清理按 `(finished_at,run_id)` 每批最多100、单事务<=2秒，保留足以拒绝旧fence的单调状态，删除历史不能使旧run可重新插入。配合active上限、activation数量和显式总记录预算给出总空间界。未来最小current结果若被批准，必须匹配当前v/g，配置变化清空pointer；A无current健康字段、接口或结果DTO。

### 11. 生产出口门禁与职责

保留第九阶段应用层DNS/IP检查、IP绑定、TLS原域名验证、header-only GET、无重试/redirect/proxy和资源上限，不复制另一套弱执行内核。生产采用独立controller与独立probe进程/容器：controller仅有调度最小DB角色；probe不持任何DB/用户Cookie/会话摘要/云凭据，无Docker socket/宿主高权限挂载，禁特权和NET_ADMIN，可用有界本地IPC传递一次性快照。网络身份不能与API共享。

未来controller角色应无用户密码/Session摘要/配置写/DDL/role继承/superuser/BYPASSRLS；只授予scope配置列、窄调度状态及必要函数，不给任意SQL写意图权限。若使用SECURITY DEFINER，固定search_path/限定schema、撤销PUBLIC EXECUTE/TEMP等隐式权限、参数化完整scope并审计owner。probe网络namespace不应为访问PG而开放内部数据库段；controller的精确DB通道与probe的公网出口分开。

| 可在仓库交付/复验 | 必须依赖具体生产环境且当前未提供 |
| --- | --- |
| A没有生产执行路径；依赖图、schema gate、零网络审计 | Worker实际网络身份/路由表/防火墙/NAT/代理及VPC/集群控制权 |
| 现有安全内核和独立Linux fixture正负控制 | 固定resolver精确IP/TCP53准入，resolver自身可信运维 |
| 最小角色/IPC/无凭据probe的测试模板（B另行批准） | IPv4/6默认deny、仅批准TCP80/443；内部公网/metadata/服务网段清单 |
| 可审核策略清单和缺条件fail-closed测试 | NAT64/6rd/透明proxy/DNAT/内部公网路由/服务网络真实阻断 |
| 隔离fixture中故意绕过应用策略，审计attempt与目标零负载 | 重建/扩缩容/策略重载/节点迁移/控制面故障后持续一致性、外部许可强制撤销 |

B生产部署准入必须绑定实现SHA、镜像digest、规则/路由/IPv4和IPv6清单、resolver、环境内部公网与转换deny、最小部署权限、独立停止演练、实际负向/正向控制及审查人。deny-first策略在进程启动前生效，政策缺失/漂移即停止新start并撤销出口；单个 `EGRESS_APPROVED=1` 或测试namespace通过不是证明。允许公网80/443前也需确认目的路由没有把原生公网形状地址转为内部资源；不能只靠IP位判断。

A没有可解锁执行的门禁字段；后续即便全部安全条件齐备，也必须发布经批准的新实现、清除A意图并重新授权，不自动从本提案升级为B。

### 12. 验收分层、真实 PG18 与 CI

本次Propose只运行OpenSpec/文档一致性/改动范围检查，不把下表计划勾为通过，不启动PG/探测器或网络fixture。

| 类别 | A Apply 必须验收 | B 后续另行验收 |
| --- | --- | --- |
| 纯单元/contract/race | strict JSON/tag/路径/错误优先级/准确DTO、v/g溢出、状态机、隐私sentinel、零executor依赖 | run/fence/时钟/预算状态机；仍不能代替PG或真实网络 |
| 真实PG18独立连接 | 下列屏障测试、迁移、scope FK/CHECK/trigger、权限实际DML | 多进程claim/start/finish/reclaim、预算、故障及外部fence联动 |
| 受控Linux真实网络 | 扩展第八阶段配置审计覆盖GET/PUT activation与PATCH失效，目标DNS/TCP/GET均0；保持原白名单/构建来源校准；全保留第九阶段安全网络回归 | pause与网络阶段屏障、GET后kill、lease/物理容量、出口故意绕过正负控制 |
| 部署出口验证 | A无生产网络能力，不能给“生产SSRF已安全”结论 | 对实际K8s/Docker/VPC环境单独验证，环境不可访问即未验证且禁执行 |

真实PG18用现有guarded disposable DB、固定Goose3.26.0，不触及应用DB；migration suite串行。独立连接/显式barrier与pg_locks/pg_stat_activity证明两种提交顺序，不用sleep-only、mock或Go race替代：

1. 历史6数据升级7全disabled，无配置时间/version/秘密变化；新创建有且仅有默认子行。
2. Owner enable/pause/恢复与Member/outsider/双租户/混合三元组；直接Store绕Handler仍实际授权。
3. 每一级User/Session/Workspace/Membership/Project/Monitor/Activation锁等待期间Session过期；撤销/清理/禁用/退出/移除/重新入Member两种顺序均断言401/403/404且零越权写。
4. 两个Owner同v/g，enable/pause重复、响应丢失/已知回滚/真实Commit丢确认、ABA；最多一个消费旧tag，不返回未知成功。
5. 配置PATCH与意图写两种顺序、同值/name/URL/全部字段，v与g原子变更；状态行缺失/代次溢出/trigger故障导致整次回滚，旧API兼容失效。
6. 同Workspace第100/101项并发enable，两个实例对同scope冷却，pause/PATCH不被限制、重启/配置变化不能重置冷却；不同Workspace可并发，检查锁图无反向等待。
7. 运行期无扫描/租约/结果、next_due非NULL被CHECK拒绝；未知Commit/重启/大批enabled无目标网络及任务积压。失去所有active Owner仍不可导致网络。
8. 0→7、带前九阶段数据6→7、repeat up、7中途故障回滚DDL/trigger/版本记录、隔离库7→6→0；1–6 SHA逐字节守护。旧schema测试明确up-to边界，原“next-version7故障”夹具前移8，不能误执行真实7两遍。

未来B额外矩阵：多Worker同到期任务及15秒租约竞争、旧fence发布、新旧g并发；claim/start/finish每个未知Commit；GET接收端barrier后kill、重启不重发；pause/配置变化分别在领取前后/start锁等待/确认后/拨号前后/GET后；start提交前阻断和提交后不可撤回分开断言；网络期间业务锁/连接占用为0；批次、公平性、跨租户物理并发/速率及长期积压；DB分区、时钟跳变和出口政策失效。测试由receiver及系统调用/包审计共同证明，不能只看DB计数。

CI在授权Apply时更新：保持verify/integration/execution-security三个独立证据域；PG normal/race全回归，新增A专项；旧第八阶段审计不能扩大网络白名单；第九阶段DNS/TLS/SSRF/资源/fuzz/PG快照及独立Linux网络全保留，明确schema7适配与历史schema6边界。新增分支触发需在届时CI列入。显式验收依赖/IPv6/namespace缺失应失败，不能skip后报成功。每项报告准确实现SHA、工具链/镜像、命令、exit、正向控制、网络/数据库/部署证据各自状态。

## Risks / Trade-offs

- [A有意图无真实监控] → 响应明确blocked、文档不称“运行中”，Worker引入强制清除旧意图并重授权。独立价值是安全控制面，不能拿它宣称业务目标B已完成。
- [所有配置变更都暂停] → 接受操作成本换取统一版本安全；需产品确认并公告客户端PATCH影响。
- [Workspace写串行/部分计数开销] → 已有锁模型加100意图上限，PG压力与索引观测，不引入全局API锁。
- [冷却/配额不能抵御跨账号公共目标滥用] → A零网络；B未批准目标聚合预算前不得开放。
- [lease/fence无法撤回HTTP或消灭僵尸流量] → 不重试started未知run、外部网络许可撤销证明、严格区分逻辑/物理保障。
- [Owner异常失效、严格pause物理语义未决] → A仍恒关闭；B评审必须补全协议，不能作为后续编码时的随意选择。
- [最后状态审计覆盖历史] → 明确只保存最新原因/actor；不用于合规全量追责。完整事件审计另提需求。
- [生产环境不可访问] → 本次无生产安全通过结论；缺证据不降级为API执行。

## Migration Plan

以下仅供A在独立评审和明确Apply授权后实施：冻结1–6哈希，先实现/测试7的事务性创建、回填、初始化、失效guard；配置API旧二进制仍可安全PATCH，意图API要求schema7，缺schema固定500且不自动迁移。发布由运维串行执行migration后再上线API；启动/readiness保持原职责。回滚API不回滚意图数据/安全trigger；共享环境前向修复，Down仅用于销毁隔离库，避免回退后丢失撤销谱系。迁移过程不发DNS/HTTP、不生成run，不自动enable。未来B需另一个迁移/发布计划清除A意图、推进g和显式重确认。

Migration1–6基线SHA256（后续Apply及本次收尾都比对）：

| 版本 | SHA256 |
| --- | --- |
| 1 | e09f6476b25ce0a60cbd6607486a9b4208f2c3413957c83e54110e0e661b5490 |
| 2 | 27ce132375459ea1e061aac1e2c00de1010c9dc8f94c21489e7c24243f02c96a |
| 3 | 327651e7727c512d80062792bab0ea874f1f63320b98ca6b0491dd8e6f94223d |
| 4 | 0374475bb78a985a9baf3de72055032bcc08e6f18512b850442a59940f20fdfb |
| 5 | 4715106905a92991c7f1dcb2da08afb7fd93217716bfa83eb4a33bb8145ac519 |
| 6 | 26ac26c66da96b0f16076342f0f11258bb6ea92be5f71a6b015c97a3075ee4e2 |

## Review Decisions and Validation Status

评审前的具体推荐已落实到A规格，没有把影响A实现的决定留给编码者。产品负责人需确认：选择A及blocked含义；任意PATCH失效；PUT同时绑定v/g且重复新命令消耗g；100 enabled/Workspace与60秒enable冷却；已确认系统意图不随原Owner退出或Session撤销消失；最末审计而非全量事件；未来B必须清除旧意图重新确认。任何选择变化应先更新本Change规划，再另行Apply。

B不构成批准事项。未来独立评审须决定start提交线性化是否满足产品暂停承诺、外部网络fence及活Owner策略、跨租户目标预算/总容量/保留策略、真实部署负责人和出口证明。未解决这些问题时不能声称B已可实现或已通过安全准入。

最终文档验证（2026-10-19）：`openspec validate add-monitor-activation-scheduler --strict --json` exit0、valid=true、issues=[]；`openspec status --change add-monitor-activation-scheduler` 四类规划工件全部完成，实施任务全部未执行。全库 `openspec validate --all --strict --json` exit1：16项中12通过、4份历史规格因七项既有 `Requirement text is very long (>500 characters)` WARNING未通过strict，分别为user-authentication requirements[11]；workspace-access [7],[8]；workspace-members [2],[3]；workspace-persistence [1],[8]。与初始主规格问题一致；没有新增历史问题，也没有修改主规格消警。

收尾检查仅新增本Change目录（CLI元数据、proposal、design、五份delta、tasks），tracked diff为空，Migration1–6哈希与基线一致，无SQL7文件/代码/部署改动。`git diff --check`通过，并另检查未跟踪新工件的空白、全部任务未勾选及MODIFIED保留原场景标识。PG18/网络/race/CI/生产部署均为未执行计划，不能把OpenSpec工件完成状态视为实现或验收通过。

# Verification — add-monitor-scheduler-worker

## Authorization and baseline

2026-10-20 用户正式授权两轮独立评审后的方案A全部47项Apply任务，确认全部产品决策；允许代码、独立Linux/PG18 fixture测试、证据和验收后的实现PR。生产真实互联网执行、生产Migration/部署、合并和Archive未授权。无需重复确认已批准的暂停/24h授权/弃用常量/全体g+1等决定。

实施开始于main `1d3b8266e494061beccc3ccdf47694ba4b0339c5`；HEAD/main/origin/main及实时远端main一致，工作区只有已批准规划目录未跟踪。PR10实时核实MERGED，merge `85402ab7877e67062e2d48b24e9ad9c31b721461`；十个归档与17份主规格保持。新分支 `codex/add-monitor-scheduler-worker`。完整planning/Migration1–7 SHA256、归档名单及工具版本见 `evidence/baseline.json`；基线提交就是证据所对应的已审SHA，不冒称后续代码已验收。

## Current checkpoint

当前 **本轮仅6.1–6.2实施/验收中，既有23/47已验收**。实际起始HEAD/已审远端为`e56c69a5832e88dfaac469071f9ada51a758dadc`，工作区起始干净。Checkpoint3的5.1–5.3已获独立技术评审通过；产品负责人本次补充授权才是两项目标预算EXECUTE的当前权威溯源，不能以Checkpoint2授权代替。5.4保持未完成，6.3–6.5及第7/8组不得提前实施。生产网络硬关闭，无目标DNS/TCP/TLS/HTTP，本轮仅PG控制面和无凭据匿名pipe/进程边界。待冻结SHA的真实normal/race验收通过才勾选6.1–6.2；不创建PR/合并/Archive/部署。下文旧Checkpoint段落为历史，以本节与末尾本轮验收记录为当前状态。

独立可销毁PG容器 `devpilot-scheduler-apply-pg18` 为PG18.6；不复用项目已有容器。Docker internal bridge禁止上游且本机端口发布不可用，因此在其控制网络中运行CGO=0 Linux测试二进制；每个破坏性套件先通过现有`internal/testdb.Open`再调用Goose3.26.0。工具/镜像/二进制哈希见 `evidence/toolchain.json` 及各命令receipt。

`ip netns add`在NET_ADMIN-only容器因mount权限不足失败。已实测可信外部Docker harness并发创建两个不同的Run namespace：无上游接口/路由、IPv6可用、双栈默认拒绝、nft内核限期许可到期能阻断既有TCP流后续数据，UID65534/caps0/no_new_privs不能改规则或路由。缺工具、NET_ADMIN、IPv6三个负向对照均非零失败，见 `evidence/preflight/manifest.json`。这些仅是工具预检，不是Supervisor完整终止回执、分布式故障或生产出口证明。

### 已执行的初步检查

| 测试/命令记录 | 实际结论 | 限制 |
| --- | --- | --- |
| `evidence/checkpoint1/pg-foundation.json`及.log | exit0；PG18 mixed enabled/disabled退役、up/up、可销毁down/re-up、两类g溢出/Trigger/后段DDL故障回滚；真实LOGIN拒绝秘密读取/直接DML/DDL/TEMP/owner role和授予函数；受限API LOGIN授权/status有效 | 当前未提交实现树，receipt记录准确HEAD/dirty/源码和二进制hash，不作为最终SHA通过 |
| `evidence/checkpoint1/unit-boundary.json`及.log | exit0；monitor/monitoring单元、HTTP/OpenAPI、Linux普通发布依赖/显式fixture构建标签检查 | 不是完整Go/race/PG/网络验收 |
| `evidence/checkpoint1/pg-spi-expiry.json`及.log | exit0；真实PG18十级SPI锁等待跨Session到期、Commit回滚/成功丢ACK对照、跨W mixed迁移及冻结schema7源hash | 尚不是Claim/Start锁序或网络证明 |
| `evidence/preflight/` | 两个不同并发netns正向通过；缺工具/IPv6/NET_ADMIN负向非零 | 尚无每Run完整Supervisor生命周期/跨容器周期 |

schema7历史fixture来自已审核基线`internal/monitor/activation_store.go`，仅重命名receiver/符号并置于integration构建；逆变换SHA256为`4fe9c60fcdb11901ba51b20af25e2a3682a0c54d4f00b87b883181ea79b7d591`。历史schema7测试固定up-to7，并执行真实旧值SQL；schema8生产Store固定新值，未修改前十阶段归档。故意失败的“下一迁移”已移至9且包含真实1–8，不以旧next8跳过M8。

### 已发现并修复中的历史回归适配

检查点提交`b2fdbbdd08b7400b550809794b915e727c1a6c3f`的首次全量Race和PG回归为exit1，不能记作通过：认证路由合同未登记两个已批准API；旧overflow fixture故意留下bigint最大g，导致M8 Down全体tag退役安全失败，后续清理级联失败。原始命令/源码与二进制hash/日志保留于`evidence/checkpoint1/failures/`。修复仅补精确路由合同及测试fixture清理；不削弱Down/g溢出保护、不改Migration1–7。污染的可销毁测试容器在核对容器ID、数据库名及PG18后重建；其他应用容器未触及。修复后重新执行，不隐去首次失败。

## Task evidence policy

tasks仅在对应验收通过后勾选。每项登记Requirement/Scenario、实现文件、测试名/完整命令/退出码、证据路径与提交SHA；初步检查receipt显式保留dirty，不冒称最终提交。1.1/1.2的授权及基线见`evidence/baseline.json`；1.4工具预检见上述实际正负控制。其余任务正在实施或待验收，未按代码存在就勾选。

## Acceptance status

- 当前23/47项已验收：Checkpoint1的1.1–1.4、2.1–2.5、3.1–3.5，Checkpoint2的4.1–4.6，第5组5.1–5.3。其余24项保持未勾选，包含5.4与第6–10组。
- Migration8仅执行于新建guarded PG18；Migration1–7保持原字节，未对生产执行。
- 两Owner/两Worker/真实用户PATCH的双向锁图和Claim未知提交已实测；真实3周期、完整Supervisor物理终止、网络unknown/完整容量与故障及原76项新SHA复验仍未完成；第5组预算本轮单独验收。
- 冻结SHA的全量Go unit/race与完整PG18 normal/integration-race已通过；本轮无Push/PR CI验收结论、未创建PR。正式CI/实现PR仍属于后续完整Change交付。
- 生产网络/部署：未验证，保持硬关闭。

## Continuation: database Claim/Start (preliminary)

本轮补充真实PG18双Owner同tag竞争（仅一个成功、另一个412）、User禁用/Session注销/Owner降级/移除重入Member等待后拒绝、旧schema7真实LOGIN及同一连接池跨M8读写、新授权Schedule故障整体回滚。原始本地receipt分别位于`/private/tmp/devpilot-scheduler-checkpoint1-owners/`和`/private/tmp/devpilot-scheduler-checkpoint1-legacy/`，均exit0且显式dirty；不是最终SHA验收。补齐设计已要求的status.desired_state及UTC时间。

Claim初步测试`/private/tmp/devpilot-scheduler-claim-locks/pg-locks.{json,log}`exit0：真实受限LOGIN、可信incarnation绑定、一次周期Run ID、租约/Fence/共享槽原子提交、W SKIP完整回滚释放User；W1持U1/W等待预算、U2真实PATCH等待W、W2选择U2 SKIP并回滚，保存pg_locks/pg_blocking_pids/DB时间；Owner hint禁用/降级/移除重入（含重新加入Owner但旧membership_id失效）；末级control等待跨有限授权到期拒绝且无部分Run/预算。尚未覆盖全部两Owner/两Worker组合、取消/deadlock/未知Commit或负载，相关tasks未勾选。

Start初步测试`/private/tmp/devpilot-scheduler-start-initial/pg-start.{json,log}`exit0：无Supervisor准备记录拒绝、准备后原子Start/令牌消耗、第二次Start拒绝、后台变化不改Activation tag/time；Pause先提交和Start等待Pause的真实PG锁屏障均禁止Start。这里的Supervisor准备是明确标注的管理员数据库fixture，**不构成Linux物理证明**；真实Supervisor准备函数、Probe输入、网络及终止闭环尚未完成。

本轮首次Claim测试分别因PL/pgSQL CASE表达式语法、测试text/name参数类型失败，修正后通过；原始失败receipt在`/private/tmp/devpilot-scheduler-claim-initial/`和`/private/tmp/devpilot-scheduler-claim-fixed/`，未隐去。先前9d6d182的全量Go Race为exit0，全量PG仅一条旧HTTP固定值断言失败；已修正且选定HTTP测试通过，等待当前提交全量复验。

升级、客户端权威状态/不可用处理、全部tag退役、HTTP排空与独立物理停止回退顺序见`docs/monitor-scheduler-rollout.md`。门禁全流程尚需真实fixture集成验收，文档不作为验收替代。

### Committed checkpoint 0c915c8

`0c915c8a11c08b46d39a08222e07293e4a8fc555` clean tree、相同源码清单hash `c032fd0c77b8fa88dc96b4a4c0d4ec37bb7f004dcd35a341f20c31f40fd2ff4f`：全量PG18 integration exit0（63.88s）、全量Go Race exit0、vet exit0、本Change strict exit0。完整命令/二进制hash/退出码及日志保留`evidence/checkpoint2/`。全库strict exit1，只保留四份历史规格七项>500字符告警，没有修改它们。

同一提交PG18 integration-race exit1（102.37s）：新增暂停屏障测试把连接交给辅助goroutine执行SQL/延迟Rollback，却在主线程收到结果时并发Release，Race Detector准确发现PgConn数据竞争。修复为辅助goroutine完成Rollback及Release后才通知结果；Claim屏障辅助函数也统一此所有权协议。未修改产品锁序、250ms/2s预算或安全契约。失败完整日志及receipt保留`evidence/checkpoint2/pg-race.*`，修复后必须重跑。

Task2.2已依据该提交的真实迁移及历史保护测试通过勾选（4/47）。其余尚未满足全部验收条件的任务继续保持未勾选；尚无真实执行/物理终止/周期或PR验收。

### Race fix and single-input foundation

提交`2e7169543099a5f13663f4f5f7a7266053c4efa6`干净工作区完整PG18 integration-race exit0（101.92s），源码清单hash`939a91d50d1253f90ee8dadd6f6e0af730c96a08b6b6e6f54f5f73195ed2e3f6`。见`evidence/checkpoint2/pg-race-fixed.{json,log}`；首次失败仍保留。不能把此通过用于后续尚未验证的新SQL。

后续新增fixture-only Go Start Store：确认Commit前不返回快照；通用fmt/JSON隐藏URL；URL只在有界单次pipe信封内以base64字节字段传递，最大2048字节URL的最坏转义仍小于4KiB。32并发复制快照只有一次写入能力，部分/失败写入不可恢复；未知Start Commit返回零输入能力，未知Claim保留原cycle hint供以后对账。真实Linux无网络、UID65534/caps0/no_new_privs/只读根/128MiB/CPU1/PID128/FD256下normal及Race测试exit0，见`evidence/input-foundation/`；receipt正确标为dirty树，只验证该单元层，**不是完整无凭据Probe或网络终止验收**。后续空URL拒绝和严格错误断言需要在最终实现SHA重验。

新增Supervisor-only数据库prepare_run/consume_input函数和真实LOGIN测试已写入，但其PG18测试初次与一次重试均被工具自动权限审核超时拒绝，**命令未执行、没有测试退出码**。事件记录`evidence/input-foundation/pg-input-not-executed.json`。没有安全拒绝理由；不是产品或设计决策待确认。受影响的PG18验收暂停，等待工具可执行/用户重试指示。没有尝试绕过该审核。

Claim最终control等待到期测试已加强为必须得到`P0001/candidate_invalid`，锁超时/事务超时不能冒充授权复验拒绝；该增强也待PG18重验，因此之前宽松断言仅作为初步结果，不完成tasks4.3。

当前仍4/47已验收；未打开生产网络，未创建PR、未推送或Archive。真实Supervisor物理停止/target permit/结果与unknown/保留及三个真实周期尚未完成；必须继续以方案A完整验收，不能以此基础设施成果作为完成交付。

### Explicitly authorized single bounded Supervisor retry

用户随后仅授权`TestSchedulerSupervisorOwnsOneInputCapability`一次有界重试，并要求完成后停止审核。本次测试实际执行一次，tested implementation SHA `3774a75eb7d3e733b174530d71816ed319d9f084`，运行时源码工作区干净。用例exit0、Go耗时0.16s；所有执行/锁采样/日志证据见`evidence/supervisor-bounded-retry-3774a75/`。完整命令、源/二进制SHA256、退出码及时间保存在`pg-input-once.json`，原始证据清单见`checksums.json`。

原PG18容器`db0774ff4cff96278db0eeab88ec0c2ed64f561a1c28d1671c200e4f31015103`、固定镜像、内部网络和原权限不变；Worker/Supervisor事务和语句2s、锁250ms以及Goose30s/40s保持。只新增收紧的单次测试90s/外层95s总封顶，未触发。只读观察器使用原fixture管理员，没有新增权限或修改服务端设置。测试自身的guarded Goose清理后只有版本0，无scheduler表/临时运行角色/活动事务。

此前两次错误来自自动权限审核在进程创建前超时，不能称为PG18或Supervisor执行超时；审核服务延迟/不可用只是可能原因，未获得服务内部根因。当前一次通过不能排除并发缺陷。6次pg_locks/pg_blocking_pids采样未捕获短暂Supervisor会话，没有采到锁等待不构成无锁等待证明。`postgres-supervisor.log`保留真实Supervisor LOGIN后端的重放、秘密读取/DDL/直接DML等预期拒绝；这是数据库权限测试，没有Linux Supervisor进程、Probe或物理终止日志。

仅关闭这项“未执行”的验收缺口；严格到期错误断言、其余并发/物理终止/完整周期等未因此验收。任务仍4/47、不推进Checkpoint、不创建PR、不合并或部署。生产执行硬关闭；完成后等待用户下一步审核。

## Checkpoint1 remaining acceptance — permission review stop

本轮遵循用户仅允许继续Checkpoint1、完成即停/安全失败暂停的范围，没有修改生产实现、Migration1–8或Controller/Worker/Probe代码。新增五份`tests/integration/scheduler_checkpoint1_*_test.go`只补原来缺少的真实PG18验收。未重跑已完成的2.2或全量验收，未重跑已通过的Supervisor单项；其首次工具审核超时的内部原因依然未知，保留为风险。

- `pg-contracts` exit0：精确SQLSTATE验证复合FK、身份/谱系/状态/时间/epoch/fence、unknown物理未释放的双活限制及释放后正向对照；schema7缺少新函数时零授权写入；配置撤销Trigger失败整体回滚；真实HTTP+PG18的严格JSON/1024边界/Cookie/Origin/CSRF/媒体/强条件/401-404-403-428-412；Controller/PUBLIC拒绝秘密/DML/DDL/TEMP及owner role，固定search_path/PUBLIC EXECUTE/无动态SQL检查，真实Worker LOGIN绑定incarnation。
- `pg-capacity-expiry` exit0（62.27s）：实际9999条Schedule加两次竞争Owner授权只有一个成功，总数/计数10000；满额仍可pause/PATCH；100 enabled/W拒绝第101个、释放后可授权；pause/PATCH保持逐值冷却，新version不能绕过；按实际DB时间经过完整60秒授权期限，logout与到期均不自动续期，当前Owner强条件显式续为24h并推进g。
- `pg-migration-interleave` exit1：activation-first通过；patch-first在旧角色执行配置SQL时收到`permission denied for table http_monitors`，migration-first因fail-fast未执行。没有检测到deadlock或本次DB/工具超时；没有为该失败补权限、改超时、跳过或重试。原始SQL/后端PID3034/时间/pg_locks/pg_blocking_pids与失败日志全部保留。

所有receipt记录当时implementation HEAD `3774a75eb7d3e733b174530d71816ed319d9f084`、dirty测试树、完整源码清单/二进制hash与命令，不能冒称那时测试文件已经提交。代码和证据现供独立审查，见`evidence/checkpoint1-remaining/manifest.json`。既有授权函数、Handler、Store与四组真实PG18并发/ACK故障测试相对已通过的`0c915c8`保持字节相同，等价性及hash记录于`authorization-equivalence.json`，所以不重复执行这些已证实检查；本轮补充了它们缺失的实际容量/冷却/真实到期HTTP边界。

### 新权限失败：已观察与未证明

新legacy夹具只授SELECT/UPDATE；既有配置写SQL同时包含INSERT和UPDATE两个CTE。缺少INSERT权限是主要假设，但没有迁移前相同角色/相同PATCH SQL的基线，不能断言是Migration8撤销了权限，更不能断言已经定位全部根因。PostgreSQL原始日志只保留permission denied文本，应用Store分类隐藏了原始SQLSTATE；后续若获准需增加原始驱动错误采集。

独立审查应先确认legacy夹具是否应包含第八阶段配置创建本就需要的INSERT列，再在schema7做同SQL正向基线，随后测跨迁移两个顺序。不能授予新Schedule/Run DML、新grant函数或function-owner/管理员权限。详见`evidence/checkpoint1-remaining/failure-analysis.md`及其PG18官方依据。本轮没有实施任何权限更改。

测试自身清理已完成：postflight记录guarded DB仍PG18、版本0，无scheduler表、无legacy测试角色、无活动事务；没有残留可运行授权。Checkpoint1剩余的迁移交错、旧角色能力盘点、完整兼容/零目标审计与无active Owner的Start前置条件仍未完整验收。任务不得因此全部勾选，Checkpoint2不能自动开始；等待独立审核。

### 本轮完成项及审查包

本轮新增勾选 **2.1、3.1、3.2**，累计 **7/47**；每项Requirement/Scenario/实现文件/测试/完整命令receipt/退出码/证据与SHA在`evidence/task-index.json`。新增测试及原始证据提交为`10b4fa83cf00e32b09b6221e3cfbf3de7af7b990`；被测生产实现保持`3774a75eb7d3e733b174530d71816ed319d9f084`不变。dirty运行记录没有改写为clean，也没有将后续文档提交说成已跑测试。

本轮五份代码改动均为`tests/integration/scheduler_checkpoint1_*_test.go`，代码diff见`evidence/checkpoint1-remaining/review-code.diff`，文档和失败分析另存。2.4的Controller/PUBLIC等子项、2.5的schema7安全失败、3.4的真实到期等已有局部通过，但因该任务其余场景未验收，不额外勾选。2.3/2.4/2.5旧写者与权限关联部分等待独立审查；3.3/3.5完整客户端与零目标审计未做，3.4无active Owner的Start门控仍未补验。没有将局部完成称为Checkpoint1完成，更未进入Checkpoint2。

原始证据`manifest.json`明确保留两组exit0和交错组exit1；所有文件SHA256索引与代码diff提供给审查者。当时尚未推送，也未创建PR、合并、部署、Archive或生产Migration；生产执行继续硬关闭。

## Independent legacy PATCH permission investigation

用户仅授权定位`patch-first`权限异常及必要最小修复。本轮**不新增完成任务，保持7/47，Checkpoint1未完成**。仅修改跨迁移测试fixture三行并新增真实PG18诊断测试；Migration1–8、生产API及Worker代码未改。

已先在独立0→7重建下用原失败fixture执行未修改的真实配置Store SQL，捕获原始`42501 / permission denied for table http_monitors / aclcheck_error`，`session_user=current_user=checkpoint1_legacy`，无PL/pgSQL错误上下文。配置PATCH语句含INSERT和UPDATE两个CTE；原fixture只有SELECT/UPDATE，故在Migration8前即失败。随后只给fixture补原INSERT目标列表九列的INSERT，Schema7 PATCH实际提交；同一LOGIN/backend跨8后PATCH仍成功，旧角色直接及有效表/列/函数权限未变，且实际撤销了另行创建的Schedule授权。旧角色没有新增Schedule/Run、执行授权或系统函数权限，负向SQL和全private函数EXECUTE盘点均通过。无需修改Migration8。

实际11个顶层PG18针对性回归exit0（1.66s）：含patch-first在内的三个迁移交错、Schema7原错误负向控制、同LOGIN的7/8 PATCH正向控制、旧Activation写者、Owner竞争及失权、真实HTTP Owner/Member/ETag、授权原子撤销及迁移正常/溢出/故障/回退。命令、驱动错误、原始SQL（不含参数）、完整ACL/所有者/trigger/definer快照、pg_locks/pg_blocking_pids及时间线、服务端错误日志全部保存在`evidence/legacy-patch-permission-investigation/`。初次新ACL诊断断言错误地把给其他已批准principal的授权也计为旧角色变化，exit1原样保留；修正比较范围后没有新增权限。Go默认host cache被sandbox拒绝的静态检查exit1也保留，改用既有可写GOCACHE后vet/Change strict/diff检查exit0，未提高任何权限或超时。

结论仅针对已证明无效的测试fixture。仓库不存在生产历史LOGIN/GRANT清单，不能据此宣称生产旧角色权限已盘点或提供猜测性生产GRANT。Schema7业务对象从0重建，但复用受保护测试DB已有的database/schema PUBLIC限制，完整快照明确记录，不冒称原始新集群。历史配置Store和Migration1–7与已评审main基线逐字节一致；生产实现与运行HEAD保持不变。被测HEAD为`80ba9671ded3e0d0b022e5cf4cda8cfc14dbeabe`加明确记录的dirty测试树，receipt包含精确源码清单/二进制hash；提交绑定另见`code-source-binding.json`，不将后续提交伪称为运行时HEAD。

独立分析与证据索引：`evidence/legacy-patch-permission-investigation/analysis.md`、`permission-comparison.json`、`review-code.diff`、`manifest.json`。测试清理后PG18.6/version0，无Schedule、临时旧角色、其他活动事务或锁等待。首次Supervisor工具权限审核超时的原因仍未获确定性证明；本次通过不排除其他并发缺陷。未重新运行完整验收、未推进Checkpoint2、未推送/创建PR/合并/部署或打开生产执行。完成本次限定调查后停止，等待独立审核。

## Checkpoint1 final acceptance — source freeze preparation

用户第一轮独立源码审查检查远端`codex/add-monitor-scheduler-worker`的`bd729b232e6eb206e44ca0f7cb63e31d95f55d30`，认可旧角色最小fixture修复且未发现须立即撤销的明确高危问题；正式允许仅完成Checkpoint1剩余1.3/2.3–2.5/3.3–3.5，完成后推送再停。该分支已经推送，上文各历史阶段的“未推送”只描述当时状态；本节之后的验收结果为最新状态。没有PR/合并/部署许可。

本轮仅补测试、Linux Probe测试构建限制、审计覆盖、协议参考与文档，不扩展Claim/Start/Worker实现。新增真实PG18旧写者/新授权双顺序、Owner失权/Start双顺序、最后等待跨授权到期、Session失效不借作后台身份、Down安全拒绝及完整HTTP投影/切换/保留Schema8回退协议测试。原配置/Activation审计新增授权/status场景，精确目的白名单和CGO/NSCD守护不变。保持Schema8的SQL仅为guarded-testdb维护协议参考，不是生产工具或Supervisor物理回执。

历史部署LOGIN/ACL无真实清单的限制不变；测试角色不能推导生产GRANT，生产部署身份仍须另行盘点与审查。现存Probe只在Linux executionnet测试构建，schedulerfixture命令仍拒绝启动；没有周期Runner/物理Supervisor/Result闭环。客户端测试是可执行协议参考，不声称外部客户端已经部署。真实运行/unknown/completed的HTTP投影使用明确标注的管理夹具，不冒称网络交付。

开发阶段失败保留：首次会话过期夹具将expires_at设在created_at之前，原23514约束正确拒绝；后续HTTP清单夹具缺login/incarnation也被原23514正确拒绝；再后一个HTTP迁移测试同一t.Name重复建立账号被唯一约束拒绝。均只修正测试夹具，无约束/权限/超时调整。最初编译错误为历史Store只提供Activation接口，通过仅integration构建的组合适配器解决；历史Handler去除新协议头后与已评审baseline SHA256一致，冻结Store源hash仍校验。

冻结提交后将重新执行unit/race/vet、完整真实PG18 normal/race及独立API零目标审计。真实60秒到期单独串行分区，原完整集成120秒进程预算保持，每个编译测试恰好覆盖一次，清单由脚本断言，非跳过。任务暂保持7/47，只有最终对应验收通过才更新。

冻结`851137d65d746687e4b0cb0e151e36c2f0fb96da`的首次完整PG18 normal分区exit1（56.98s）：`TestExecutionAuthorizationAtomicIntentAndRevocation`使用openDB+migrate但没有清理，紧邻容量测试初始计数断言失败。独立组合测试真实重现该授权测试结束后migration8/Schedule表仍在/1条Schedule（`isolation-root-cause` exit1）；未观察到预算越界或数据库权限不一致。最小修复仅将其改用既有workspaceDB清理协议，并保留组合隔离回归。原容量10000上限、9999真实行+两竞争最后槽断言及全部数据库保护不变。该提交的unit/race/vet/Change strict通过不作为后续新SHA通过，须重新冻结完整验收。

## Checkpoint1 final acceptance — completed, awaiting independent review

2026-10-21在修复测试隔离后冻结实现`d781d7560904ee81c4939d9520604ff2240766a4`，所有final-*运行均记录该SHA/clean及相同源码清单hash`1cae1483aefa7fce5ad5983aa25fa42b6fab6bf84e8b45773d78fa2db66605fb`。普通Go unit、Race Detector、vet、gofmt、fixture-only Linux normal/race、工具正负预检、Change strict及独立API审计均exit0。完整真实PG18 normal与integration-race各131个顶层测试全部通过，清单相同且每种构建恰好覆盖一次；每种均130项+真实60秒授权到期分区，未skip，未延长原120秒进程预算。normal64.90s+60.47s，race105.09s+61.42s。全部函数2s事务/250ms锁限制和容量/安全定义保持。

真实PG18补验覆盖全部旧g/ETag退役、Migration8异常/溢出/Trigger/DDL全事务回滚及Down拒绝和生命周期，非接流量旧写者/新授权双顺序与旧PATCH依赖，运行角色、PUBLIC/秘密/DDL/TEMP/伪造身份拒绝，以及Owner失权/Start双顺序、最终等待横跨授权到期、Session失权与有限授权独立性。HTTP测试覆盖固定Activation提示/intent-v2/旧schema7及14字段契约、运行改变不污染意图验证器、权威status与客户端失配刷新/不可用契约，保留Schema8退役tag的回退协议也在guarded DB验证。旧部署ACL无真实清单；测试通过不授权生产GRANT。

独立strace+防火墙审计在同一SHA、原隔离PG18网络运行真实配置/Activation/授权/status HTTP工作负载：child0/audit0，25个批准的DB/本机HTTP尝试、0个未批准目的尝试。故意越界校准child0/audit1，精确白名单没有新增目标例外，CGO来源及NSCD/未知IPC反例通过。工具预检复验两个不同无上游namespace、限期规则和缺依赖/权限/IPv6负向控制；不能算完整Supervisor或真实周期验收。

依据对应验收新增勾选1.3、2.3、2.4、2.5、3.3、3.4、3.5，各次CLI确认8→14；当前**14/47，Checkpoint1完成**，第4–10组33项仍未完成，没有进入Checkpoint2。详见`evidence/checkpoint1-final/task-completion-record.json`及上级`task-index.json`的Requirement→Scenario→实现→Test→命令/退出码/日志/SHA映射。

全库strict仍exit1，四份历史规格七项长度警告与旧日志逐字节一致；本Change strict通过，没有新告警或无关规格修改。首次宿主unit因sandbox禁止loopback bind而失败、前冻结提交full-PG因授权测试漏清理而失败、开发夹具约束失败均完整保留。Supervisor首次工具审核超时的内部原因仍未知；没有把数据库权限通过当作独立物理终止证明。

证据入口`evidence/checkpoint1-final/README.md`，原始trace/PG日志、精确命令、测试/工具二进制hash与共享源码清单齐备。大清单以hash共享；未加工原始JSON回执完整压缩保存，原字节hash可校验，退出码/dirty未改写。后续任务/证据/verification文档提交不重新执行相同代码；`implementation-binding.json`明确文档HEAD与实际运行SHA，唯一清单差异是验收通过后的tasks勾选，所有实现/测试源字节相同。Migration1–7与已审main字节不变，Migration8相对首轮源码审查亦未变。

最终清理确认PG18.6/version0，无Schedule、临时角色、活动事务或锁等待；原应用容器未触及。本分支此前已推送首轮审查；本轮接着推送补验代码和证据供复审，推送状态以末段及远端核验为准。没有创建PR/合并/部署/Archive/生产Migration或开放生产目标探测。维护SQL和客户端逻辑只是可执行协议参考，没有外部部署或实际Supervisor证明；完整周期/target-permit/物理kill/Result/unknown/目标预算/保留/原76项新SHA网络复验/最终CI仍是后续范围。完成本轮后停止，等待独立代码与安全复审。


## Checkpoint2 final acceptance — completed, awaiting independent review

2026-10-21用户确认Checkpoint1第二轮源码审核通过并正式授权仅4.1–4.6；随后明确允许新增`reconcile_claim`和`expire_unstarted`两项窄EXECUTE。冻结实现/测试提交为`6eb76fc93e86d7cc9038f86b74bfbd6f03b4b877`；最终normal/race、Go检查和strict回执均为该SHA、dirty=false，共享源码清单hash`c93642d57c7469533da7b9d9fa801a255d160fa80e48be14efc13e07a2d9112a`。前一冻结`7e0832f`的完整normal已通过，后来只增加PATCH最终v/g/epoch和撤销授权后回收断言，重新冻结并全量重验，不将旧结果替代最终结果。

完整真实PG18 normal与integration-race各147项（145+真实60秒授权到期+真实45秒Lease）恰好覆盖一次，全部exit0，未skip。normal为76.67/60.70/45.38秒，race为113.53/61.39/46.46秒；各进程原120秒预算未延长，所有运行函数2秒事务/语句与250ms锁预算未放宽。Go unit/race及Linux fixture unit/race使用count=1均exit0，gofmt、默认/fixture vet、diff-check与Migration1–7哈希复核通过。本Change strict exit0；全库strict exit1的四份历史规格七条长度警告与Checkpoint1日志逐字节相同，无新增警告。

实现保留逐语句Owner User SHARE→W NO KEY UPDATE SKIP LOCKED→Membership→P→M→Activation→Schedule→Run→Budgets→Reservation顺序；最终控制锁等待后由新SPI快照重验权限/完整谱系并复用单次DB gate时间。小幅后跳取持久时间最大值，超出既有2秒事务预算无法吸收的后跳拒绝；测试用未来持久时间建模，没有修改宿主时钟。36对Run转移矩阵、epoch/fence溢出、固定错误类别、65W/73M扫描的64W/8M/32次尝试及每W一次领取、游标轮转均经实测。此处不是后续1000W/24小时压力验收。

真实独立LOGIN/事务屏障验证两Owner/两Worker的相同/不同Owner、Monitor及Workspace候选；Owner失活、降级、退出/移除、重入Member/Owner的两个顺序；无锁hint不是授权凭据；候选无效/skip后全部回滚释放User；逐级锁等待不先碰后级子行。Worker1持U1/W、U2用户PATCH持U2/Session等待W、Worker2取U2后skip的顺序，以及PATCH先行、两个Worker均skip的相反顺序均保存pg_locks/pg_blocking_pids、DB时间线、SQLSTATE与最终v=2/g=3/epoch=2。独立NOWAIT探针和事务锁消失补强关系锁观测。ACK观测是提交时间上界，不冒称精确server commit timestamp。

最终控制等待横跨授权期限或stop更新均以等待后新快照/时间拒绝且无部分Run/预算；预期55P03超时及57014取消证明完整回滚。另由管理员fixture**故意违反协议**制造budget→W逆序环，真实得到40P01并核对受害者回滚；运行角色没有此表写路径。未观察到正常Worker协议锁序冲突或意外安全测试超时，不能仅凭无死锁/通过数推断所有未测交错安全。

未知Claim仅按原scope/run/incarnation读取当前有效leased，不重建输入、不新建替代Run；真实Commit后丢ACK与真实Rollback后丢ACK分别核对数据库状态。未Start租约实际等待原45秒后，仅在全部DB准备/namespace/input/target字段为空时，以`release_basis=never_prepared`回收未曾授权执行的槽；不伪造Supervisor termination_receipt。已准备但未Start保留quarantine和容量；started拒绝此路径。重复/旧Fence回收不能返还两次；PATCH已经撤销的旧v/g/epoch不能重新排程；正常未来due折叠为max(DB时间+interval,last_started+interval,not_before)，不补发积压周期。这里是DB权限证明，**不是Linux进程/namespace终止证明**；实际Supervisor资源生命周期与物理容量限制仍须后续独立验收。

仅新增上述两项已获明确授权的Worker EXECUTE，角色属性、表/列ACL、秘密读取与其它运行函数权限不扩大。真实权限测试验证API/Controller/Supervisor不能调用这两个入口、Worker不能直接写表、伪造incarnation失败。Migration1–7未变，Migration8新增状态约束/释放依据/两个窄函数；完整迁移、原子撤销、历史角色与Owner/Session/API回归在147项中重新通过。历史部署角色ACL仍无真实清单，未猜测生产GRANT。

开发期间两次fixture清理错误完整保留：第一次改写已expired终态/结束时间被23514拒绝；第二次模拟后跳后用更早DB时间清理被23514拒绝。只修正fixture以保留终态与取max时间；数据库保护未降低，guarded Down拒绝也未绕过。一次受现有testdb守护的可销毁清理源/日志仅作为诊断保存，不留自动伪造未来真实物理证明的全库清理测试。先前Supervisor工具审核超时的内部根因仍未证明，风险保留；本次通过不抹去该历史事件。

证据入口`evidence/checkpoint2-final/README.md`，包含Requirement→Scenario→源码→Test→精确命令/退出码→证据→SHA映射、完整原始回执/PG日志/锁事件与校验清单。按对应验收逐项勾选4.1–4.6，CLI确认15→20；`task-completion-record.json`记录六次完成。后续交付提交仅改tasks/evidence/verification，`implementation-binding.json`确认与冻结SHA全部实现及测试字节相同。

普通发布与API仍无执行入口，Linux发布构建边界在当前SHA验证。此前CP1的API零目标strace与第九阶段76项网络结果只保留为历史证据，本轮不声称它们在当前SHA重新运行；真实Probe、Supervisor、target-permit、周期/heartbeat、Result/unknown网络故障、目标预算、保留/磁盘及完整CI仍是未验收的后续任务。当前workflow push列表不含该开发分支，且没有创建PR，因此没有新的GitHub CI结论。交付分支仍为`codex/add-monitor-scheduler-worker`，普通推送后以远端HEAD核对及最终答复为准。测试PG已回到version0、无调度表/临时角色/其它事务/锁等待；生产执行始终硬关闭。完成Checkpoint2后停止，等待独立审核，不创建PR/合并/Archive/部署或推进Checkpoint3。


## Checkpoint3 — group5 budget acceptance and review delivery

### Authorization and exact implementation

用户仅批准5.1–5.3可独立验收部分，5.4允许数据库协议但物理回收依赖第6组；用户另明确答复“批准仅这两项窄EXECUTE”。新增：

- Worker `scheduler_private.reserve_target(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text)`。
- Controller `scheduler_private.rotate_target_budget_keys(uuid,bigint,jsonb)`。

完整签名/用途/ACL见`docs/monitor-scheduler-checkpoint3-capabilities.md`；仅上述精确EXECUTE，runtime角色属性、既有表/列ACL及秘密读取权限均未扩大。Controller增加可信incarnation注册kind，不能由此打开部署gate。函数由原非LOGIN owner持有，VOLATILE/SECURITY DEFINER、固定search_path、无动态SQL及PUBLIC EXECUTE，2秒RC事务/250ms锁预算保持。

实现变更：Migration8补充目标摘要目录、保留代次账本及原子预订/轮换函数；Start复验last_started+当前interval及持久Monitor预算not_before；固定retention_capacity安全映射；fixture-only Go HMAC目录/有限事务适配；独立PG测试harness与预算文档。Migration1–7实际逐字节和SHA256复核不变。普通API/发布构建没有新执行入口，原执行安全内核与出站审计白名单未变。

### Frozen-SHA actual verification

所有最终命令在干净冻结SHA `e05959ec7aa0e4e80981bcd59ea0d6ee4c823815`执行，源码清单SHA256为`615b7fd0fdbbbde73a5f6d66fd7e1f53b8a2934f7b4df3bc254bf48623ca0b96`。二进制、命令、退出码、原始日志及源文件清单见`evidence/checkpoint3-final/`；`implementation-binding.json`解释后续仅三个任务勾选的字节差异，`requirement-test-evidence.json`提供逐项映射。

| Actual test group | Result |
| --- | --- |
| 第5组真实独立PG18预算normal/race | 各14项，全部exit0，无skip；每项独立数据库保留未释放账本后销毁 |
| 既有全量PG18 integration/integration-race | 各147项，串行145+真实60秒授权+真实45秒Lease，全部exit0，无skip |
| 全量Go unit/race、Linux fixture unit/race | 全部exit0，count=1；Linux工具容器network none |
| gofmt、默认/fixture go vet、git diff --check | 全部exit0 |
| Migration1–7哈希 | 七项全部与已审基线相同 |
| Change strict | exit0 |
| 全库strict | exit1，仅原4份历史规格7项描述长度警告；完整输出SHA256仍为`fef4737504c8a9567664c2e4925fa8db19971473867e035eb2f8dff3ca9bd3d1` |
| 原受保护PG18最终状态 | schema0，无Scheduler角色、其它活跃事务及锁等待 |

已实现且实测：32/4/2/1全局及父级最后槽、两Owner/Worker/Workspace skip交错；9个并发Start的8次burst；W1/s burst2、P真实2秒补充；Monitor当前interval/配置v/g/新Worker连接不缩短冷却；失败回滚与已Start不退token；真实10000个budget key容量边界；跨W origin1/IP2竞争、IP真实1秒及origin真实60秒冷却；HMAC脱敏/原生地址/不可变并发目录；轮换双代共同参与、原token/not_before不清零、keep_until至少1小时及禁止提前再轮换；活跃槽不排空就拒绝轮换；目标预订真实Commit丢ACK不返回许可/不重复；最后Supervisor锁等待后复验；Owner/Member status不暴露摘要或其它租户预算。

新group5测试不写管理员physical_release时间或Supervisor终止回执。prepare/consume调用真实受限LOGIN函数但仅为数据库协议测试，没有实际namespace/进程/网络规则；每项保存未释放账本、证明Goose Down拒绝，再销毁独立标记的可销毁PG18实例。原历史测试沿用已审数据库fixture清理，其结果不作为物理释放证明。pg_locks/pg_blocking_pids/DB时间线/SQLSTATE/最终状态和各PG日志均保留；ACK观察仍只是提交完成的上界，不伪称精确服务端Commit时刻。

### Failures retained, limits and stop boundary

开发阶段五次非零结果及原日志保留于`evidence/checkpoint3-final/development/`，不可冒充冻结SHA通过：迁移相对目录错误（SQL前）；fixture参数未显式timestamptz导致42P08；既有URL规则拒绝.test测试名（SQL前，未放宽规则）；新增Controller注册kind缺失导致23514（补该注册类型，无ACL扩大）；PL/pgSQL变量/别名k歧义42702（重命名变量）。最终验收没有非预期安全超时、权限校验不一致或协议死锁。历史套件故意注入的55P03/57014/40P01仍为预期失败对照；这些通过不能证明所有并发交错均不存在问题，仍需独立审查。

历史Supervisor工具权限审核首次超时的内部原因仍无确定性证明，风险继续保留。历史生产部署角色ACL仍无真实清单，不推断或新增生产GRANT。轮换的一小时保持以实际DB期限/不可删除/提前拒绝证明，没有声称一小时wall-clock soak；目录输入上限1000项/512KiB/两代，超时失败关闭，不放宽2秒预算。1000W/24h压力、完整retention/磁盘预算和原76项网络/新SHA API strace审计尚属后续范围。

**5.4仍未完成**：当前只记录各保留摘要代次，保护未来回收正确记账；没有Supervisor物理回执验证/释放函数，未实测僵尸Probe、进程kill、已建立流切断或旧namespace不可恢复。当前只有数据库目标预算，DNS后目标钩子、独立许可安装、真实网络故障/周期闭环均未执行，不把其等同方案A完成。生产网络执行硬关闭，无公网监控许可；本轮不创建PR/合并/Archive/部署/推进第6组，推送分支后停止等待独立复审。


## Current product-owner authorization supplement and tasks6.1–6.2 scope

实际起始HEAD与远端均为`e56c69a5832e88dfaac469071f9ada51a758dadc`，工作区干净。产品负责人本次明确批准Checkpoint3两项能力：Worker独占EXECUTE `scheduler_private.reserve_target(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text)`，Controller独占EXECUTE `scheduler_private.rotate_target_budget_keys(uuid,bigint,jsonb)`。本次补充授权是当前权威溯源；Checkpoint2代码基线及其reconcile_claim/expire_unstarted授权不能代替本次授权，历史短回复只作历史上下文。当前仅修正文档，不扩SQL权限、角色属性、表列或秘密权限。

本轮仅实施6.1–6.2：Start Commit及匿名pipe/无凭据进程边界。生产执行硬关闭，子进程不得解析DNS/拨号/HTTP。6.3–6.5、5.4物理回收及第7/8组未获本轮实施授权。5.4保持未完成，无数据库状态可代替独立Supervisor物理终止证明。当前仍23/47，待本轮真实PG18/Linux冻结SHA验收后再勾选。

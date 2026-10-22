# Verification — add-monitor-scheduler-worker

## Authorization and baseline

2026-10-20 用户正式授权两轮独立评审后的方案A全部47项Apply任务，确认全部产品决策；允许代码、独立Linux/PG18 fixture测试、证据和验收后的实现PR。生产真实互联网执行、生产Migration/部署、合并和Archive未授权。无需重复确认已批准的暂停/24h授权/弃用常量/全体g+1等决定。

实施开始于main `1d3b8266e494061beccc3ccdf47694ba4b0339c5`；HEAD/main/origin/main及实时远端main一致，工作区只有已批准规划目录未跟踪。PR10实时核实MERGED，merge `85402ab7877e67062e2d48b24e9ad9c31b721461`；十个归档与17份主规格保持。新分支 `codex/add-monitor-scheduler-worker`。完整planning/Migration1–7 SHA256、归档名单及工具版本见 `evidence/baseline.json`；基线提交就是证据所对应的已审SHA，不冒称后续代码已验收。

## Current checkpoint

当前 **累计27/47，新增6.5独立fixture物理终止验收；5.4、6.3保持未勾选**。产品负责人独立审查33fb5edde369a30f9bfb902d5e3fdd3525a46586并核实Push/PR成功后，明确仅授权6.5。6.5实现提交ddcaa3f1f0b41e76c6eb932ea01585576d24b9af；另经单独授权的共享HTTP超限响应最小修复为73def21d5f6a06c9a7edd37847c2831b577740a8（原PR失败及回归见文末）。最终以同时包含两者的冻结交付HEAD真实Actions为准，开发态dirty结果不冒称最终SHA验收。本批完成独立紧停、已建立TLS流切断、pidfd终止/回收同Run主进程及孤儿、完整谱系Ed25519回执/重放拒绝、丢失监督器及节点恢复的失败关闭和运维说明。没有DB回执提交/槽释放能力，未扩大数据库函数/角色/EXECUTE/表列权限，Migration1–8不变。真实HTTP内核Probe联合执行及物理容量回收仍待分别独立复审。PR11保持Open，生产执行硬关闭，不合并/Archive/部署。

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

- 当前26/47项已验收：Checkpoint1的1.1–1.4、2.1–2.5、3.1–3.5，Checkpoint2的4.1–4.6，第5组5.1–5.3及第6组6.1–6.2、6.4。其余21项保持未勾选，包含5.4、6.3、6.5及第7–10组。
- Migration8仅执行于新建guarded PG18；Migration1–7保持原字节，未对生产执行。
- 两Owner/两Worker/真实用户PATCH的双向锁图和Claim未知提交已实测；真实3周期、完整Supervisor物理终止、网络unknown/完整容量与故障仍未完成；原76项已在本轮冻结SHA的原隔离内核fixture复验通过，不代表新Scheduler拓扑验收。
- 冻结实现691c993的全量Go unit/race、vet、完整PG18 normal/integration-race及隔离execution-security已在实际Push/PR CI通过。PR11保持Open；完整方案A、独立Supervisor及后续任务仍未验收。
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


## Tasks6.1–6.2 — frozen Start and input-only process acceptance

产品负责人本次在审查`e56c69a5832e88dfaac469071f9ada51a758dadc`后正式补充批准：`reserve_target(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text)`仅Worker EXECUTE，`rotate_target_budget_keys(uuid,bigint,jsonb)`仅Controller EXECUTE；权威溯源见`docs/monitor-scheduler-checkpoint3-capabilities.md`及`evidence/checkpoint4-final/authorization-supplement.json`。Checkpoint2历史授权不能冒充本次批准。本轮没有新增/修改SQL、EXECUTE、角色属性、表列或秘密读取权限；Migration1–8均与已审HEAD字节相同。

实际冻结实现/测试SHA：`7b761cad01706ff1abfba51bf0e13f2d091fd44a`。所有最终命令HEAD干净，源码清单SHA256=`215129b2ec76f6dbf6d9914d842e3533c1ebce69526851349fed614377fa0528`。后续仅两个任务勾选的源码清单差异，verification/evidence排除于清单，代码/测试/协议/授权文档不变。实际命令、退出码、原始日志、二进制hash及逐Requirement/Scenario映射见`evidence/checkpoint4-final/README.md`、`requirement-test-evidence.json`、`implementation-binding.json`和manifest。

| 实际验收 | 结果 |
| --- | --- |
| 新增7项真实PG18 Start/Input协议及14项相关预算回归 | normal/race各21，全部exit0，无skip |
| 既有PG18 integration/integration-race | 各147，串行145+真实60s授权+真实45s Lease，全部exit0 |
| Linux真实匿名pipe/子进程 | normal/race各2项顶层测试，12类畸形/跨谱系负例，另含真实进程重启重用已消费pipe拒绝，全部exit0 |
| 旧内核无网络PG快照合同 | normal/race各3，身份/Scope权限/连接释放/语句一致性通过；未选择live网络用例 |
| 全量Go unit/race，Linux fixture unit/race，vet/gofmt/diff-check | 全部exit0；原内核离线安全策略及发布产物边界保留 |
| Migration1–8源hash及字节对比 | 与已审HEAD完全相同，无数据库权限变更 |
| Change strict | exit0 |
| 全库strict | exit1，仅原4份历史规格7项长度警告；输出hash仍为`fef4737504c8a9567664c2e4925fa8db19971473867e035eb2f8dff3ca9bd3d1` |
| 原测试库最终状态 | PG18.6/schema0，无临时角色、其它活跃事务或锁等待 |

6.1：复用已审Start父序与受限VOLATILE函数，11级真实锁屏障保存pg_locks/pg_blocking_pids、DB时钟和最终状态；两个Owner/Worker与用户Pause/PATCH双提交顺序、Claim/Pause双顺序通过。最后control等待跨授权期限、Owner失活、Supervisor过期、紧停后新语句门控拒绝；原45s Lease实际等待30s后剩余不足timeout10+5拒绝，没有缩短Lease或延长测试超时。真实Commit成功、真实提交后丢ACK与回滚丢ACK均验证，未知Start无快照/信封，读回started不能重新Start。Start成功后Pause/PATCH不替换旧v/g/URL快照；符合已批准在途语义，未发送请求。

6.2：发送端只在Start确认后可取得私有能力；独立Supervisor既有consume_input确认提交且释放连接之后，才写4096字节固定帧与1024字节受信绑定回执到匿名pipe。任何失败均消耗发送能力，未知输入ACK不返回fd、不重连；接收端验证真实匿名FIFO、精确长度/EOF、规范严格JSON、完整谱系/摘要/期限且只消费一次。摘要只是受信fd绑定，不是用户鉴权或Probe HMAC秘密；恶意受信Controller同时伪造两管道不属于接收端证明。PG pool/pg_stat_activity实测信封接收/离线桥期间无业务事务或占用连接；本轮未进行实际网络卡点实验。

真实Linux子进程UID/GID65534、无附加组，包含bounding在内全部caps0、NNP、全部线程TSYNC seccomp；实际禁止socket（含io_uring绕过）、exec及管理/namespace操作。只有LANG/TZ环境，父进程DB/Cookie/云sentinel未进入；只有受控pipe及runtime内部fd，无源码/配置/凭据卷/管理Socket。容器只读根、128MiB/CPU1/PID128/FD256为实际cgroup/rlimit，观察真实EMFILE；资源限制覆盖整个fixture进程组，比单子进程更紧。父进程读取存活子进程/proc独立确认；URL/秘密不出现在argv/env/普通输出或Run持久化。私有桥只构造原监控Snapshot并复用离线验证，不调用或导出DNS/HTTP执行器。未来完整桥依赖6.3及独立许可。

开发期一个PG用例失败23514：紧停fixture使用了非法gate=fixture/stop=true组合，只修fixture为gate关闭与stop原子变化，原始失败与成功日志保留，未改安全约束/ACL/超时。最终SHA没有非预期安全超时、权限不一致或协议死锁；历史故障注入预期55P03/57014/40P01仍保留。测试通过不证明所有并发交错皆无缺陷。此前Supervisor工具审核首次超时原因仍无确定性证明；生产历史角色ACL清单仍缺失，不推断生产GRANT。

5.4仍未完成；数据库准备/输入消费、子进程退出及可销毁fixture清理都不是独立Supervisor物理终止证明。新PG用例保留未释放账本并确认Down拒绝后销毁各自测试库，没有伪造终止回执/physical_release时间。PG发放与独立sandbox是同一协议/SHA的分层真实证明，尚非Supervisor全流程。6.3网络钩子、6.4 namespace/veth/防火墙、6.5独立物理停止，以及第7/8组均未实现。原76项/执行网络、live snapshot及API目标strace本轮因明确禁止目标网络而未执行/未声称通过，源码及白名单不变。unknown数据库状态协调属于第7组，本轮未知确认只返回无能力并保留资源，不冒称恢复系统完成。生产执行硬关闭，不创建PR/合并/Archive/部署，不自动进入6.3；推送后等待独立复审。


## Task6.3 — default-deny target hook and protocol, partial review delivery

产品负责人审核Checkpoint4 HEAD `0fd6502f30c0882ae083773feb7f24b8fe41a524`后仅批准6.3；明确允许当完整真实联网依赖6.4时先交默认拒绝和许可协议，保留未完成状态。本轮没有借此实施6.4/6.5、5.4回收或第7/8组。任务总数仍25/47，6.3保持未勾选。

冻结实现/测试SHA `8593edccd94e1ed00348e5cb0a98895f1b71cb71`；最终命令全部HEAD干净、源码清单相同，实际命令/退出码/原始日志/二进制hash/Scenario映射及manifest见`evidence/target-permit-final/README.md`。2026-10-21至22同一冻结树执行，后续交付只补verification/evidence；没有把开发期或混合SHA的过渡运行记入最终通过数。

### Implemented and actually verified

- `internal/monitoring/executor.go`仅在原完整DNS验证和选中literal之后、原TCP调用之前插入私有钩子；nil默认拒绝，不改变DNS/IP/TLS/原域名SNI/RemoteAddr/Proxy=nil/无重定向重试/Header-only GET及原资源预算。许可等待计入原timeout，拒绝返回nil Result和固定控制面错误，不伪造目标HTTP失败；取消与Transport异步dial回调交错用原子阶段标记处理。
- fixture-only `monitoring/targetpermit`为两个匿名pipe的单次2048字节规范严格JSON协议，完整scope/run/v/g/epoch/fence/namespace/Supervisor、原域名/方案/port/选定IP/截止时间绑定，无URL Path/凭据/HMAC密钥；精确长度/EOF、匿名fd、畸形/伪造回复/串Run及重复消费均拒绝。格式化脱敏，管道等待有2秒上限并受原deadline/取消进一步收紧。
- `SchedulerInput.targetHook`私有桥绑定冻结配置及更早的原probe/hard deadline；没有新增公开执行方法/Probe构造器。`StartSnapshot.TargetController`只能从已确认、不可重建的本地Start能力派生，所有副本共享一次尝试位，任何拒绝/未知Commit均烧毁；保留已校验原Lease时间戳，不修改数据库Start协议。
- 控制面只用既有HMAC fixture目录和Worker reserve_target函数，沿用父序、VOLATILE等待后新快照/数据库时间和Owner/scope/v/g/epoch/Lease/Fence/stop/Origin/IP预算；确认Commit、归还连接之后才调用独立Supervisor接口。没有具体规则安装器；nil/断连/ACK未知/错谱系IP域名ID期限/过期都拒绝，不重试、不换IP、不重解析。真实PG提交丢ACK和回滚对照均零installer调用；即使回滚也不能用同一Start重申许可。成功DB预订只证明持账，不是可拨号或物理停止证据。

| Actual acceptance at frozen SHA | Result |
| --- | --- |
| 新2项Target Pipe＋21项Start/集群/同目标预算PG18 | normal/race各23，全部exit0、无skip |
| 历史PG18 integration/integration-race | 各147，原145＋真实60s授权＋真实45s Lease串行，全部exit0 |
| 请求截止的独立连接/Origin锁屏障补充详细PG日志 | normal/race exit0，真实pg_locks/pg_blocking_pids，服务端SQLSTATE57014；只增加日志详细度，原超时和权限不变 |
| 原76项隔离内核网络安全场景 | 全部通过；4个故意bypass反例按原合同audit_exit1被检测，未弱化规则/白名单 |
| 新5项许可缺失/拒绝/断连/未知/超时控制 | 全部通过，各1次真实DNS、0目标connect；独立receiver/trace/路由/防火墙/cgroup证据保留 |
| Go全量unit/race/vet、Linux fixture unit/race/vet | 全部exit0、count1；普通发布无执行依赖/入口 |
| Migration1–8、gofmt、diff-check | 字节与审核基线一致；全部exit0，无SQL/ACL变化 |
| Change strict | exit0 |
| 全库strict | exit1，仅原4份历史规格7项长度警告；按路径/内容对比无新增，不比较durationMs |
| 原测试PG18最终状态 | schema0、无Scheduler角色/表/其它事务/锁等待 |

网络的5个拒绝控制是原dial门控的受控callback失败；真实数据库Commit未知和实际匿名pipe协议在独立PG用例验证。它们是同一源码/SHA的分层证据，不能合写成已经完成的Supervisor/PG/Probe联网拓扑。协议witness只是检查Commit先后及ACK绑定，不安装规则、不伪造终止回执。原network-none内核fixture的显式test-only allow只存在executionnet测试，不是Scheduler许可。API零目标strace本轮未重跑，原精确白名单及CGO/NSCD守护未改。

### Retained failures, dependencies and stop boundary

开发首个PG用例因Start响应不含Claim时间戳，本地Lease完整校验在SQL前拒绝；最小修复是在确认Start后保留已经校验的原Claim Lease，数据库仍重新复验，不增加权限/读回重建。首次冻结网络concurrency旧断言把许可前取消误当网络结果错误；只在父context已取消且Result=nil时接受ErrTargetPermit，仍核算全部32个已admit结算、真实GET witness、Canceled结果及FD/槽回收。首个主机unit因沙箱禁止本地监听失败，正常审批允许本地测试监听后全量通过；Linux无网络测试也通过。失败原始日志均在development，未隐去或计作通过，未延长安全超时或放宽断言。

2026-10-21自动审批服务用量限制阻止一次只读工具版本命令，命令未执行、不是安全否决；用户继续后于22日按正常审批流程成功。此前Supervisor工具权限审核首次超时原因仍无确定性证明；历史生产角色ACL清单仍缺失，不推断生产GRANT。历史race145项分区实际117.24s，接近原120s预算但通过；没有随意延长或凭通过断言不存在并发问题。预期故障注入55P03/57014/40P01与未知Commit对照保留。

完整6.3验收仍缺：6.4的每Run默认拒绝namespace/veth/防火墙与可信规则安装通道、独立无凭据可联网Probe IPC，以及6.5的独立Supervisor物理终止。现有input-only Probe继续seccomp拒绝socket，未放开。没有完整允许的Scheduler GET、物理kill、周期或安全返槽；所有新PG用例保留未释放资源账并证实Down拒绝后销毁其可销毁数据库，不能将其作为终止证明。5.4/6.3/6.4/6.5均未勾选，生产网络硬关闭；本批推送现有分支后停止，不创建PR/合并/Archive/部署或进入6.4。

2026-10-22交付审查包时，自动权限审批未在期限完成，Git提交/推送命令未执行；只读核对HEAD仍为冻结SHA、index为空。工具明确允许一次有界重试，按原审批路径执行，不绕过检查；审批超时原因未确定，与已通过PG/Linux安全测试分开记录。原工具回执信息见`evidence/target-permit-final/development/git-approval-timeout.json`；交付结果以最终HEAD及远端核对为准。

上述唯一审批重试通过，随后提交前diff-check发现两份原始工具输出自带尾随空白并阻止提交；只将build/listeners原始文本改为gzip包装，解压字节和原SHA256保持，不关闭diff检查、不修改源码或验收结果。


## PR11 CI repair — cumulative time budget and historical snapshot isolation

本轮唯一授权是CI/验证编排修复。起始`b7742bc247cbf8fbd59c5ff591642e5f3cc0bc30`，分支`codex/add-monitor-scheduler-worker`且工作区干净，PR11实际Open。原run [39706081660](https://github.com/rjgczp/devpilot/actions/runs/39706081660) head匹配该审核SHA；原始run/jobs状态、失败及完整日志保留在`evidence/ci-repair-before/`，不将旧verify或原76+5成功冒充本轮新HEAD通过。

- integration原整组进程上限120s；超时时TestCheckpointOneRealExpiryExplicitRenewal只执行36s，其余约84s已被此前用例消耗。栈为该真实等待，不是已证明数据库锁死。保留120s/原60s授权/45s Lease及全部旧测试，改用版本化显式147项清单：原integration142项全部保留，另5项已评审fixture Claim/恢复回归纳入；核心30/30/30/30/25，真实60s与45s各自单进程。`run_integration.py`编译发现后核对完整清单，未分配/重复/失效条目或任何测试skip均失败；Go JSON须证明每项恰好运行/通过一次，并保留原./...其它包的验证。normal/race对同一guarded PG18串行执行，不以增大超时或模拟时钟消除错误。
- execution-security原步骤标签schema7但调用无限制Goose up；完整日志确认00008已成功应用、当前version8，四个旧Snapshot按原断言正确拒绝。只给独立execution-security job的空PG18测试服务使用`tests/ci/snapshot-schema`：原testdb身份/PG18/独立URL守护、检查原生Goose3.26.0、固定up-to6并断言无schema8对象；非空或schema8库拒绝，不自动Down/Drop。旧Snapshot四项原始源码不变，真实live接收端/v7-v8配置隔离及Race仍全量执行。
- workflow为本开发分支启用Push，Push与PR显式checkout同一实际HEAD并断言/记录，避免将PR合成merge SHA当冻结实现。保留三个job，未新增Scheduler生产服务或6.4部署。normal/race分组、verify、Snapshot、76+5网络和原API零目标audit分别保留原命令/退出码/Git/source/binary/log哈希及Actions Artifacts。API白名单、CGO来源和NSCD反例、原执行器网络/资源审计全部保持。

本地开发前置检查：Linux编译发现147项与清单完全相符，原142项无删除；分组覆盖守护3项、原网络分类/CGO来源7项、执行分类2项通过。主机直接运行Go unit曾因沙箱禁止本地listen而失败，不是本轮CI通过结果；设置独立可写Go cache后CGO分类测试通过，未改断言。最终CI结果以冻结提交上的实际Push/PR run及其result/receipt/artifacts为准，当前不预填通过。不勾选6.3或整个10.1（独立scheduler-security/Supervisor仍未交付），累计仍25/47。完整CI后保存实际run结果供独立复审，PR11保持Open；生产执行始终硬关闭。

### CI repair — actual frozen implementation results

冻结实现 `691c9933956aa7e1d7e6a30cfde628941b9cac06`：Push [39708326035](https://github.com/rjgczp/devpilot/actions/runs/39708326035) 和 PR [39708331695](https://github.com/rjgczp/devpilot/actions/runs/39708331695) 的 verify / integration / execution-security 全部实际 success。两套下载的原始Artifacts已逐项校验：所有命令receipt均exit0、dirty=false、HEAD匹配；源码清单SHA256统一为 `1956bbc2c6b2ad25504c5d6fe5dae5df42ff6cb101cbc10a32e1965d12fe2228`，原始日志SHA匹配。详见 `evidence/ci-repair-final/summary.json`、命令receipt索引、run/jobs元数据及Artifact文件哈希；大体积trace保留在索引链接的Actions Artifacts，GitHub报告的archive digest与本地解压文件校验值明确区分。

- normal/race各147项PG18和151项其它包测试通过，清单无遗漏或skip。真实授权组命令耗时60.91–62.24s，真实Lease组45.94–47.22s；最长核心组53.60s。仍使用原120s进程上限，未缩短真实等待，未模拟时间。每一项具体测试、命令、退出码和时间见normal/race plan及receipt索引。
- 原四项Snapshot在独立空库使用Goose3.26.0 up-to6后，normal/race全部通过，含原live isolated execution；Schema8套件不共用该数据库。未修改历史安全断言或绕过schema检查。
- 原76项隔离网络场景和5项Target Permit拒绝通过；五项拒绝各有真实DNS尝试且目标连接数为0。API配置/Activation/授权/status原零目标边界通过，故意绕过对照audit exit1如预期；未扩大目的白名单、移除CGO来源或NSCD反例。
- verify中的全量unit/race、vet及原守护通过；本地clean691c993 Change strict exit0，命令与原始日志已归档。Migration1–8、既有生产/安全内核、旧测试断言和ACL均未改。

最初推送曾被GitHub拒绝：默认Git凭据缺workflow scope；使用同账户已有workflow授权的gh凭据后推送成功，用户亦完成refresh/setup-git。未保存任何Token或设备验证码。此问题不同于历史Supervisor工具权限审核超时，后者原因未获确定性证明的风险仍保留。

本次证据交付提交只更新verification及CI证据元数据，其最新HEAD的Push/PR仍须实际完成后另报结论，不用691c993结果冒称后继SHA已测。累计25/47不变，6.3、5.4、6.4、6.5未勾选；真实独立可联网Probe、Supervisor安装/物理终止及完整周期尚未验收。未推进6.4、扩大权限、合并、Archive或部署；生产执行硬关闭。完成交付后停止等待独立复审。

### Additional authorized deadline gate fix after delivery CI failure

交付HEAD `a5b49c16355418b8c9dcf63e72907702961b6eba`：[Push39709818391](https://github.com/rjgczp/devpilot/actions/runs/39709818391) verify/execution-security成功，integration在147项PG18 normal通过后，旧单元测试`TestServerDeadlineAndUnknownCommit`失败（handler_test.go335/338，expired request inserted，201而非503）。该Push的race/API审计未执行，不能标通过。同SHA [PR39709822402](https://github.com/rjgczp/devpilot/actions/runs/39709822402)三个作业成功；这不抵消Push失败，未重试原失败流程。原命令/退出码/源码与日志hash、完整失败日志及元数据保存在`evidence/ci-repair-deadline-fix/`。

已向用户提交review-only最小diff并说明超出仅CI编排范围，用户明确回复“授权最小期限修复及回归”。既有`Handler.expired`仅检查`ctx.Err()`；真实期限已到但取消回调尚未执行时可能放行。该运行没有runtime timer trace，因此精确调度交错未被确定性证明；新增确定性单元回归模拟past Deadline/Err=nil，在旧代码稳定拒绝失败，在修复后通过，不冒称真实PG或网络验收。

最小改动`internal/registration/handler.go`在原pre-Store期限门控同时检查实际Deadline；保留标准单调时钟、公有5s/2s预算、API/Commit语义。`handler_test.go`仅新增一个回归及fmt import，全部旧测试内容字节未改（包括1ms/5ms真实等待与未知Commit断言）。开发树完整unit/race、vet、定向normal/race exit0；receipt显式dirty=true，不能替代最新冻结SHA CI。最初本地沙箱禁止HTTP监听/cache造成的失败也保留。最终HEAD实际Push/PR结果与新源码哈希须由交付Artifacts和最终审查报告核实，不能沿用691c993/a5b49c1的通过结果。

本次最小修复不涉及Scheduler网络/数据库/权限，不进入6.4。任务25/47和未验收边界不变，生产执行硬关闭；完成后保持PR11Open，停止等待独立复审。

## Task6.4 — independent namespace and timed network policy

用户明确批准仅6.4，复审基线为343540f1f23a579e35f796b27eddb48776178ab6。实现为`internal/supervisorfixture/`、`cmd/networksupervisorfixture/`（均schedulerfixture && linux）及`tests/scheduler/network_fixture.py`可信分配器；由Docker只创建network-none namespace，NET_ADMIN-only管理域通过新veth点对点连入自己持有的Run/独立guard/receiver，不使用host网络/PID、Docker bridge、SYS_ADMIN或privileged。无权限测试子进程UID65534/caps0/no_new_privs、独立PID/mount/cgroup、只读根、128MiB/CPU1/PID128/FD256，无管理管道、Docker Socket、配置源码/DB/用户/云凭据。它是恶意绕过与接收端witness，不能取代原HTTP执行器或作为完整Scheduler Probe验收。

Go管理进程在启动子进程前原子安装双栈DROP，精确Resolver TCP53及单literal TCP80/443有内核超时；完整Run/v/g/epoch/fence和两种incarnation、真实namespace inode、接口/route/RPDB/地址/规则摘要受复验。目标只允许预登记fixture域名/IP。单次CAS及管理域私有bootstrap账防重复安装/同namespace重启，首个目标SYN的conntrack mark和一日单包burst限制一个目标连接，没有ESTABLISHED放通。Resolver也有固定硬截止；规则hash忽略运行计数/动态TTL显示，但安装时独立检查实际元素/截止上界，拒绝TTL漂移，内核设置前扣除完整2s工具预算避免相对timeout晚插入延长许可。已批准DB父序、Fence和origin/IP预算代码不改。详细信任边界、命令和剩余依赖见`tests/scheduler/network64.md`。

专用`TestBudgetNetworkPolicyPG`使用networkfixture标签、现有guarded新建PG18和既有受限角色/函数，先取得真实Linux准备proof，再以Supervisor身份prepare/consume，Worker确认Start和目标预算Commit/连接归池后才安装。normal/race分别有IPv4/IPv6 TLS接收端实到/SNI与证书核验；禁止第二许可，完整scope核对数据库账仍reserved、released_at/termination_receipt及run物理释放字段NULL。没有管理员UPDATE伪造释放；销毁fixture是测试清理，Goose Down须拒绝仍持物理账的started行，不作为6.5终止回执。

开发过程保留失败，不以重试成功推断无竞态：Docker能力显示名与只读sysctl适配后仍是NET_ADMIN-only（forwarding在新namespace创建时设置）；早期bridge实验IPv4 Resolver失败，未放宽规则，改成直接veth后双栈正向成功，未将具体bridge丢包机制宣称已确定。独立策略用例的合成Commit时间一度来自Darwin，固定阶段诊断证明Linux严格future-proof门控正确拒绝；合成时间改为Linux来源，真实PG时间/门控不改，并以future-commit-proof负向继续验证。UDP connect仅本地操作的测试记账、JSON时间字符串精度及DNAT注入器语法作最小fixture修复；nft check-only独立确认两族语法后才复验。开发态有一轮因正在迭代脚本与旧工具混用而失败，不能计入最终验收；最终统一源码/工具重建。全部诊断与命令/退出码/源码/二进制/日志hash在本轮证据索引记录，实际冻结SHA结论须另核对CI。

固定源码/工具后的开发态完整6.4验收已通过，故只勾选6.4；对应实现提交为930532ce28825f5bfb4778c3e3a3acb266c92eb8。五组实际命令（Linux policy normal/race、network-adversarial、PG18 normal/race）全部exit0，同一源码清单0fa96eada4462c5b592b2fdc58175aa6031ecbcd13634026244e982448e4e68a；receipt保留真实head343540f和dirty=true。`evidence/network64-development/implementation-binding.json`逐文件核对运行代码字节与实现提交相同；这不替代交付HEAD的实际CI复验。最终Push/PR与source/log/binary哈希须以最新HEAD的Artifacts和交付报告核实，不预填通过。CI在现有execution-security中增加本任务专用步骤并上传原始trace/receiver/nft/iptables/ip6tables/route/cgroup/PG账，不创建独立scheduler-security作业或勾选10.1。原76+5、四Snapshot normal/race、完整PG18 normal/race和原API零目标审计均保留，白名单/CGO/NSCD断言不改。

剩余边界：6.3原Probe/HTTP内核完整联合运行、6.5紧停切断既有流/实际进程kill/不可伪造终止回执/节点恢复，以及5.4 quarantine回收仍未验收。管理EOF+内核到期的网络证据不能证明进程结束或返还槽；没有发出这种回执。历史Supervisor工具审核超时未获确定性原因、真实部署旧角色ACL清单缺失的风险继续保留。生产执行未验证且硬关闭。

本批源码已冻结为实现提交930532ce28825f5bfb4778c3e3a3acb266c92eb8；后续提交只整理任务和证据。开发态source/HEAD/dirty区别、原始失败与成功命令、PG真实SQLSTATE及held账、Linux receiver时间线见evidence/network64-development/。原始大体积trace/nft/route/进程证据由最新HEAD execution-security Artifacts保留并提供校验清单；不得用旧归档76项冒称该HEAD通过。开发态26条network证据条目中最后一条只说明无物理释放证明，不能作为额外完成测试；6.5及5.4不因此完成。


## Task6.5 — independent physical termination and recovery

本批授权来自产品负责人对33fb5ed的独立审查和本次明确请求，不引用早期其他Checkpoint授权扩大权限。代码仅fixture-only构建、私有管理IPC和受保护新建Linux/PG18；新部署入口、生产目标、SQL/GRANT和5.4未实施。代码/测试/运行手册是实现提交ddcaa3f1f0b41e76c6eb932ea01585576d24b9af的精确字节，详见`evidence/stop65-development/implementation-binding.json`。

### Protocol and authority

NET_ADMIN-only的原Run/Gateway管理进程增加绑定私有bootstrap账/实际namespace/完整谱系的终端revoke：先持久墓碑，原子全DROP（无ESTABLISHED例外），删除Run-facing veth、清双栈conntrack，复验独立链/无旧线/无连接跟踪后才给出网络证明。原控制进程退出或规则丢失也能由独立管理入口撤销，但任何失败不给终止回执。

独立Linux PID1 Supervisor只有KILL/SETUID/SETGID/SETPCAP，无NET_ADMIN/SYS_ADMIN/host PID或网络；启动后不可dump，单次启动fixture Probe前清空其cap bounding set、所有组并降到UID/GID65534。实际检查caps五组全0、no_new_privs、128MiB/CPU1/PID128/FD256、只读根、精确干净env、无秘密/源码配置卷/Socket；Probe读PID1私有FD/mem/environ、信号PID1、管理路由/规则/namespace/raw socket均真实拒绝。该Probe是固定stream/恶意子进程生命周期witness，不冒充原HTTP内核的6.3完整联合执行。

真实接收端先确认TLS/SNI/证书与before-stop frame。仍存活的原连接在独立网络撤销后尝试after-stop写入，接收端无新增frame；live-permit用实际nft毫秒TTL及Linux采样保守下界确认紧停先于自然到期。然后PID1以pidfd与原start_ticks捕获身份，SIGSTOP/SIGKILL全部同Run进程并收养/reap孤儿；2s内不能达到空域或任一pidfd未退出就不签。保存死子进程strace后再次确认空域，PID1自行退出，内核PID namespace不能重新fork。以上都在外部container删除清理之前完成。

外部可信Supervisor私有0700状态与Ed25519密钥不进入Probe/Worker；仅在两层网络、进程/pidfd、空域和PID1退休全部确认后签<=4KiB回执。完整scope/run/v/g/epoch/fence及两incarnation、实际PID/net namespace、独立证明摘要均绑定；Run注册、不可变回执和消费账O_EXCL+fsync，重载同受信状态不能恢复旧Run/消费已用回执。PG18测试在Start前从可信管理通道固定公钥，验证Ed25519及完整谱系；Worker自带公钥不是授权。回执只说明物理终止，不说明HTTP结果，也不自动返还任何槽。

### Actual development acceptance and failures

最终同步源码清单be7aa9c0177fc7df80ba2d5fd24934a177c6dc222d4b44d128e61f05495165e8：原6.4五组normal/race/网络/PG18全部exit0（总246.56s），新增6.5三组Linux物理终止、PG18 normal/race全部exit0（总76.43s）。Go1.24.3原生Linux及Goose3.26.0，未模拟网络/PID/数据库，不增加skip或盲目重试。6.4现含原4项policy单元及新增2项真实边界/密码学parser单元，normal/race均通过。所有开发receipt诚实保留HEAD33fb5ed/dirty=true；与实现提交逐文件字节绑定，最新冻结交付SHA仍须在Push/PR重新执行所有要求后另报结果。

6.5独立场景为：双栈真实TLS既有流紧停；故意忽略TERM/INT的主进程与直接/孤儿子进程pidfd退出/reap；原管理控制失联、局部及两层规则丢失后的独立revoke；重复/伪造/外来公钥/完整谱系任一部分替换回执拒绝；PID1 Supervisor被真正SIGKILL时无回执、持续quarantine；旧节点/新namespace/新incarnation不能重新绑定旧Run、跨namespace旧控制及旧bootstrap均拒绝。PG专用`TestBudgetPhysicalStopPG` normal/race分别实际IPv4/IPv6建立TLS、切流/杀进程，校验固定公钥的真实回执，但DB仍reserved、released_at/termination_receipt/physical_release_confirmed_at为空，所有32/4/2/1及目标容量保持原账。没有管理员UPDATE制造回执/释放，也不把容器清理当证明。

保留两个开发失败及原因边界：最初密码学unit生成的签名未递归排序嵌套map，修正合成测试编码，未弱化验证；扩展真实物理测试已切流/回收pidfd后，Docker archive未找到tmpfs trace而非零退出，不能记为通过。改由可信PID1在子进程结束后，使用已有SETUID权限的fsuid读取实际trace并有界分帧/hash传出，未增加DAC_OVERRIDE或SYS_PTRACE、未放宽停止判断；随后读取到真实strace。未将Docker归档行为的更底层原因宣称已确定。原始失败/成功命令、退出码、日志、源码及二进制哈希见`evidence/stop65-development/`，大体积核验资料在最新execution-security的`stop65-evidence/`及`network64-evidence/`Artifacts。

### Preserved boundaries and handoff

停止/恢复/无法确认时继续quarantine的操作说明见`tests/scheduler/stop65.md`。监督器失联、状态/密钥丢失、网络/进程任一证明未知时保持blocked，不能换key/新目录/新namespace或靠lease/重启/Goose Down返还容量。回执可信持久消费、父序安全释放及所有旧HMAC代次对账属于5.4，仍未验收；6.3原内核联合执行仍未验收。没有变更Start Commit暂停语义或新加强暂停HTTP接口。

CI保留现有verify/integration/execution-security、原147项PG normal/race分组、Schema6四Snapshot normal/race、原76+5网络安全、API精确零目标白名单及CGO/NSCD断言；在execution-security附加6.5 runner和raw Artifact，不提前完成独立scheduler-security任务10.1。最终全量unit/race/vet、PG18、原76+5/6.4/6.5、Change strict、Git diff及实际Push/PR状态须以冻结交付HEAD的命令receipt核验，不能预填绿色或使用旧SHA代替。本批完成后停止等待独立审查。历史Supervisor工具权限审核超时根因未确定、真实历史生产角色ACL清单缺失、生产出口未验证风险继续保留。


## Separately authorized HTTP overflow response repair after87af881 CI

87af881fae2358924102fbcb9fd891a310ec2266的[Push39740892742](https://github.com/rjgczp/devpilot/actions/runs/39740892742)三个作业成功；[PR39740899418](https://github.com/rjgczp/devpilot/actions/runs/39740899418)verify/execution-security成功，integration-race/core-04失败，不能报告全部绿色。失败是旧`TestCheckpointOneAuthorizationRealHTTPStrictContract` case31/1025：HTTP200空body而非413，进程19.58s/原120s预算，并非测试超时或Supervisor停止失败。原HEAD/dirty=false/source4854c7c2ffd88d6567e1850d6f261107577d3130d258c4470c4f61ea03cd5643/exit1/原始日志及Actions元数据在`evidence/stop65-http-repair/`；没有重试旧失败workflow。两轮旧execution-security的6.5实际签名、独立证明摘要/原始manifest另已验证通过，这不抵消PR回归失败。

已先向用户提供未应用diff、说明超出6.5-only范围；用户明确回复“授权最小超限响应修复及回归”。修复提交73def21d5f6a06c9a7edd37847c2831b577740a8只改`internal/monitor/handler.go`和新增`body_limit_response_test.go`。旧超限读取立即设过期读期限，可能令net/http后台读取消Request.Context，错误响应提交前被抑制/Abort；该次CI没有goroutine/transport trace，具体时序不宣称确定性证明。新增确定性取消屏障unit在原代码四路由均失败，修复后全部通过，明确只是该取消类模型，不能替代真实PG验收。

最小修复在超限时保留原有有界读期限与Connection: close，错误响应提交后才中断剩余transport读、避免额外drain；真实ctx取消/读失败的提前中断仍保留。limit+1读取、配置/activation/授权/status原字节上限、5s/2s预算及所有旧测试断言/等待不变。全部Go unit/race及vet exit0；原六个HTTP/PG18用例（含原失败strict-contract）在受保护新建PG18中normal/race各通过，命令耗时1.45s/5.91s，未增加重试/skip或延长任何测试上限。开发source8db479acb1360ba915b09f0c093ad0396d138c16290b648cb3729e9910765243及诚实HEAD87af881/dirty=true与代码字节绑定见该证据目录。最新冻结交付HEAD必须再次跑完整Push/PR及原零目标/安全验收，不能拿旧SHA结果冒称通过。

此额外修复不改变6.5协议、数据库权限/函数、Migration1–8或生产硬关闭；累计27/47不变，5.4/6.3继续未完成。最终报告保留原PR失败与新的实际结果，PR11保持Open，停止等待独立审核。


## Task5.4 — independently verified receipt settlement and quarantine

本批源于产品对3489a41的独立审查及仅5.4授权；随后用户明确回复“批准仅上述3项能力及信任边界”。仅新增Controller的quarantine_physical和Supervisor的accept_termination/reconcile_termination三项SECURITY DEFINER/EXECUTE，精确签名、GRANT及外部密码学验证/数据库可信Supervisor身份边界见docs/monitor-scheduler-task54-capabilities.md。无其它角色属性、成员关系、表列/秘密权限扩展，Migration1–7不变。未把既有其它Checkpoint授权当本次许可。

Migration8在准备时冻结可信登记公钥摘要；不可变原namespace/incarnation/v/g/epoch/fence绑定资源账。独立Supervisor先fsync提交真实网络/pidfd/reap/退役证明，再签并持久化回执；Go适配器验证固定公钥Ed25519、完整十项谱系和全部证明哈希/物理不变量后才获取DB连接。PL/pgSQL不验证Ed25519、不检查Linux；它只信任经session_user/incarnation绑定的独立Supervisor SQL身份，并核对原key pin/Run身份、严格4096-byte回执和唯一nonce/digest。没有把Worker自报、verified布尔或TTL当作回收许可。该可信Supervisor身份被攻破的风险不由本fixture解决。

按D6回收子序W→P→M→Activation→Schedule→Run→global/W/P/M→全部排序origin/IP保留代次→Reservation→control；不获取User/Session/Membership，等待后新SPI和一次DB时钟复验。消费与物理释放、并发计数递减在同一PG事务完成。两代目标key均释放，但不退Start/target令牌、冷却、安全保留或运行记录容量；不写attempt/HTTP结果/当前观察/next_due，不改变配置、Activation或Owner强tag。旧started物理回收后仍可被原部分唯一索引阻止后续同MonitorClaim，留待结果/unknown协议收尾，不通过修改索引提前实施第7组。

真实增强验收TestBudgetPhysicalReleasePG normal55.63s/race56.29s均exit0，同一运行源码3829b59ae0e4bb81dc9f03fa83cfac42f6b420db1c4a92dd2462303bfca61116。PID1被实际SIGSTOP后，原45s Lease真实过期，UID65534/caps0的主Probe、直接子进程及孤儿后代仍存活，global32与目标槽未返还；原<=35s网络许可不续期。显式独立stop后真实双栈网络撤销、pidfd退出/reap及PID namespace退役，提交匹配证明后一次返槽。两个消费连接/两个Worker最后槽竞争保存锁图和Commit屏障：只有一次消费、一项新Claim；未知Commit分别用实际Rollback和Commit后丢ACK，后者仅按原nonce/digest只读对账，不重新发GET/信封/许可。11个逐级真实锁等待点与控制锁等待期间的身份撤销证明新SPI复验；四个DML触发故障点全部回滚；跨租户/旧Fence/incarnation/未知key/签名或证明替换拒绝；真实Owner禁用并持User锁/PATCH新v/g后原Run仍可按旧谱系物理回收，HTTP结果保持NULL。最小角色LOGIN矩阵及原秘密/DDL/DML拒绝保留。

开发失败、原始命令/退出码/源码与二进制/日志hash见evidence/release54-development/。初始本地构建路径/src与真实挂载不一致导致Goose收集目录失败，未跑迁移；修正构建路径后实际迁移通过。下一轮SIGCONT-only管理确认在原3s超时，网络已撤销但无pidfd/退役回执，DB保持quarantined/global32及目标槽，不将容器清理记为释放。一个PID1信号假设被独立反例否定；后续无网络单进程select诊断复现暂停期间的等待行为，显式控制消息唤醒。原失败缺恢复后的syscall/wait状态，精确原因仍保留未证明风险。改为既有显式绑定Run的stop协议，不延长3s/2s/35s/45s，不增加权限/skip/自动重试；normal/race通过不抹除首次超时。

本批只勾选5.4，累计28/47；6.3与第7–10组仍未完成。运维步骤、未知ACK/监督器失联时保持blocked、禁止手改资源账/重建key/替代旧Run、手工恢复边界见tests/scheduler/release54.md。生产执行硬关闭，未合并/Archive/部署。开发态实际HEAD/dirty与实现字节绑定不能替代交付SHA；完整unit/race/vet、147项PG normal/race、原76+5、6.4及6.5与本批5.4的最终结果须由冻结HEAD实际Push/PR CI和独立Artifact哈希核验后另报，不预填绿色。Change strict已通过；全库仍仅历史四规格七条描述长度警告，未改无关主规格。

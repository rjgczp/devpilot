# Verification — add-monitor-scheduler-worker

## Authorization and baseline

2026-10-20 用户正式授权两轮独立评审后的方案A全部47项Apply任务，确认全部产品决策；允许代码、独立Linux/PG18 fixture测试、证据和验收后的实现PR。生产真实互联网执行、生产Migration/部署、合并和Archive未授权。无需重复确认已批准的暂停/24h授权/弃用常量/全体g+1等决定。

实施开始于main `1d3b8266e494061beccc3ccdf47694ba4b0339c5`；HEAD/main/origin/main及实时远端main一致，工作区只有已批准规划目录未跟踪。PR10实时核实MERGED，merge `85402ab7877e67062e2d48b24e9ad9c31b721461`；十个归档与17份主规格保持。新分支 `codex/add-monitor-scheduler-worker`。完整planning/Migration1–7 SHA256、归档名单及工具版本见 `evidence/baseline.json`；基线提交就是证据所对应的已审SHA，不冒称后续代码已验收。

## Current checkpoint

当前仅推进 **Checkpoint1：Schema/Authorization**。本轮补充真实PG18缺口验收后，在旧角色配置PATCH跨迁移场景遇到权限失败，已停止受影响任务，Checkpoint1尚未完成。此前已有的Claim/Start初步代码保持原样，本轮不扩展、不推进Checkpoint2及以后；没有可执行Scheduler/Probe完整闭环，不能将基础设施或局部通过描述为方案A完成。

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

- 当前7/47项已验收（1.1、1.2、1.4、2.1、2.2、3.1、3.2）；Checkpoint1仍未完成，受影响迁移/旧角色权限与完整API兼容验收暂停。
- Migration8仅执行于新建guarded PG18；Migration1–7保持原字节，未对生产执行。
- 真实3周期、两Worker锁图、完整Supervisor物理终止、unknown/预算/容量/全部故障与原76项新SHA复验：未完成。
- 全量normal/race、最终同SHA Push/PR CI与交付PR：未完成；尚未创建PR。
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

原始证据`manifest.json`明确保留两组exit0和交错组exit1；所有文件SHA256索引与代码diff提供给审查者。没有推送、PR、合并、部署、Archive或生产Migration；生产执行继续硬关闭。

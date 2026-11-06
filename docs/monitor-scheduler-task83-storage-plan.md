# Task8.3 — 存储测量、可信指标与准入设计（待独立设计审查）

日期：2026-11-04。设计/只读评估基线：`0462dc2e65c8ea9edd3948cf6a72d483e93f7510`。
Task8.2已由人类独立验收，36/47；8.3保持未勾选。
本文件是方案和最小权限申请，不是实施许可、实测结果或生产运行证明。
提交分支：`codex/task83-storage-design`，基于上述验收SHA；
人类后续消息《DevPilot AI — Task 8.3 设计提交》授权仅文档commit/push，不授予实施权限。
评审入口：[拟议迁移/权限精确差异](monitor-scheduler-task83-capability-request.md)。

## 1. 授权、规格映射与本轮停止点

人类消息《DevPilot AI — Task 8.2 验收签署与 8.3 启动》仅授权：登记8.2验收；
8.3设计和只读环境评估；准备必要的最小权限申请。方案完成后停止，交ChatGPT独立设计审查。
本轮不写执行代码、SQL迁移或测试编排，不启动PG/负载，不授予角色、文件权限或设备能力。
不改已验收8.1/8.2核心实现，不合并PR12、Archive、部署或开放生产互联网执行。

| 依据 | 本设计与未来验证 | 本轮状态 |
| --- | --- | --- |
| Task8.3；D9 | 表/索引/TOAST、WAL、vacuum、日志与文件系统分别测量 | 只完成设计和静态/环境核查 |
| monitor-runs / Retention has enforceable count time and capacity bounds | 1GiB调度关系预算、独立10GiB测试盘，80%拒绝新Claim | 尚无真实磁盘负载证据 |
| Disk growth is measured separately from row limits | 周期写入、清理后复用、长事务阻止vacuum、WAL/日志增长 | 待批准后实测，不从8.2行数证据推导 |
| Cleanup cannot keep up；D6 | 压力只收紧准入；保留父序、物理证明与安全写入 | 不放宽M9/M10、不强制删账 |
| Task9.1/9.4、10.x边界 | 仅复用已有受限函数/fixture测存储 | 不借此交付完整周期产品、24h/1000W公平性或全套CI任务 |

## 2. 当前事实：数值门槛存在，可信采集链尚不存在

以下是本轮源码核查结果，不是新测试结论：

- Migration8的`public.scheduler_control`包含`disk_used_bytes DEFAULT 0 CHECK >=0`，
  `disk_budget_bytes=1073741824`。默认deployment为closed/emergency_stop；默认0本身不打开生产。
- 当前有效Claim（M9替换版本）在最终新SPI门控中比较`disk_used_bytes < disk_budget_bytes*0.8`；
  M8的Start、Target Permit、Claim对账及恢复时的未来调度资格也使用该比较。
  授权函数仍为M8版本，status为M9版本，两者亦读取同一值。等于80%已经不放行，不应回退为严格大于80%。
- 全库排除历史evidence的精确字段搜索，只找到M8/M9的声明及读取；没有找到采集器、
  运行时更新该值的窄入口、`pg_total_relation_size`采集或文件系统到该字段的来源链。
  `internal/scheduler/store_linux.go`直接调用数据库Claim，不采磁盘。
- deployment的`verified_at/expires_at/manifest_digest`证明的是既有fixture部署登记，
  不是独立磁盘采样时间。没有磁盘sample_id、源设备绑定、有效期或失败标志。
- `tests/integration/scheduler_claim_test.go:schedulerDatabaseOnlyGate`由可销毁fixture管理员
  设置部署gate；它不采磁盘。既有通过证据不应被重新命名为真实磁盘保护证据。
- 运行角色没有直接UPDATE control权限。现有非LOGIN owner权限不是新增Reporter权限；
  不能用Controller/Worker报一个低数字，或在负载测试中管理员UPDATE数字，代替来源验证。

**评估结论**：现值能表达一个被配置的拒绝条件，但来源、刷新与时效性均未证实。
不能据此宣称真实磁盘达到80%后一定会拒绝Claim。数据缺失/长期停留0的安全语义必须另审。

## 3. 本机只读环境评估与缺口

原始只读回执及303个已验收核心文件的SHA256快照：
`/Users/sansea/.codex/artifacts/devpilot/task83/design-only-20261104T092002Z/`。
`readonly-assessment.json`记录命令/退出码/时间；`accepted-core-sha256.json`用于本轮结束复核。

| 只读观测 | 实际结果 | 可证明与不可证明 |
| --- | --- | --- |
| Git与远端 | 开始时干净；本地/远端/PR HEAD均0462dc2，main均5905f518 | 可绑定设计基线；不代表未来文档版另跑CI |
| Docker context/info | desktop-linux；Docker Desktop Linux aarch64；overlayfs；8 CPU，12528861184 bytes内存；日志默认json-file | 只证明本机可见配置；不证明Linux CI同架构/独立盘可用 |
| Docker ps | 无运行中容器 | 本轮没有可只读查询的运行中PG样本；未为取样启动任何容器 |
| macOS工作区df -k | 总482797652 KiB，Available53383464 KiB，显示89% | 是宿主APFS快照；不是PG挂载、1GiB关系预算或独立10GiB盘 |
| 原Budget runner | 每测试独立network=none PG18，512MiB/CPU1/PID128；未指定10GiB专盘/配额 | 容器memory参数不能当磁盘上限；原测试盘隔离未获证明 |
| workflow/runner静态搜索 | 未见8.3磁盘采集、专盘、轮转预算配置 | 不声称已检查Docker VM内部盘或生产服务器 |

当前不具备“独立10GiB盘真实负载”证据。也没有连接生产DB、读取用户业务数据或扫描宿主大目录。
Docker info默认json-file不能证明每个容器都有轮转；具体fixture须单独记录显式日志配置。

## 4. 固定字节口径与候选预算

所有准入比较使用整数bytes及numeric交叉乘法，不能用展示用MiB、四舍五入百分比或浮点。
以下预算/时间参数均为**待批准设计参数**；不能因测量超标临时扩大。

| 量 | 来源与边界 | 候选准入规则 |
| --- | --- | --- |
| R：调度关系实际占用 | DB服务端固定表清单的`sum(pg_total_relation_size)`，含表fork、索引及TOAST | B=1073741824；`5*R >= 4*B`拒绝；`10*R >= 7*B`告警 |
| F：每个必需挂载的空间 | PG所在设备/命名空间的statvfs；T=f_blocks*f_frsize，A=f_bavail*f_frsize，以PG运行UID实际可用量核验；U=T-A | 任一设备`5*U >= 4*T`拒绝；inode耗用达到80%或读数无效也拒绝 |
| W：当前WAL分配空间 | 固定pg_wal目录的文件分配块数，包含回收/预分配segment；不以wal_bytes累计值代替 | 独立1GiB预算；达到80%即拒绝并告警；不自动删除WAL |
| L：PG日志 | 固定日志目录及可信轮转器仍持有的已unlink日志inode，按inode去重的实际分配量 | 独立64MiB预算；达到80%拒绝；有限轮转且保留故障输出 |
| 临时文件与测试填充文件 | 固定测试设备，单独标签/目录；PG temp同时记录temp_bytes/temp_files | 临时文件候选256MiB预算；填充只做物理压力负向测试，不计为R |
| WAL归档与备份 | 本fixture不运行归档/备份、不配置复制槽；预检核实实际设置和目录 | 预算0，发现未登记归档/备份/槽先拒绝预检；未来非零预算及设备另审，不靠删除归档腾空间 |
| Docker/驱动日志及证据 | PG数据盘以外也必须登记其设备、限额/留存及采样来源 | Docker日志候选每容器8m×4并实测额外开销；证据另设有界盘，不能无限写宿主 |

R固定清单：`monitor_runs`、`monitor_schedules`、`scheduler_reservations`、
`scheduler_budget_state`、`scheduler_control`，以及获准后新增的两个有界storage元数据表。
每个普通表只加一次total；分项table/index用于解释，不再重复累加TOAST或索引。
新增对象须更新受审manifest，缺表、意外分区/表空间、未知对象或测量异常均拒绝。
父级用户/配置表不计入这个调度专属R，但整个数据库、共享catalog、所有其他进程仍占F，必须如实展示。

1GiB是**调度关系容量预算**；WAL/日志/临时文件各有独立预算，并由F总量兜底。
`pg_database_size`用于专用fixture数据库的总量交叉核验，不能再与R相加，不能代表WAL或设备可用空间。
在1GiB口径中，R=858993459 bytes在80%下方，R>=858993460拒绝。
文件系统阈值按实际格式化后的T计算，不能拿10GiB标称大小替代T。

采样不是存储硬配额。即使已经拒绝新Claim，Result、unknown、可信返槽、Pause/PATCH、vacuum与
其他数据库写入仍可能消耗空间。剩余20%只作为恢复余量，必须实测在途最多32个run及维护开销。
若剩余余量不足，先关闭fixture新执行并增加停止措施的独立审查，不宣称现设计能保证物理盘永不满。
WAL配置候选max_wal_size=512MiB，保持fsync/full_page_writes及原持久化协议；这是测试配置提案，
不能把它当成WAL硬上限或测试失败后随意增大的旋钮。实际segment高水位超过预算则如实失败。

## 5. 真实10GiB测试盘与观察者拓扑（未执行）

首版选受控原生Linux runner，由一次性可信**盘配置步骤**提供新建、仅本测试使用的10GiB设备及
独立文件系统。普通named volume、稀疏文件的ls大小、tmpfs或容器memory限制不合格。
不默认允许共享文件系统上的项目配额替代；statvfs未必显示该项目可用额，若改为配额方案须另审可信配额指标。
如用loop文件，须在批准后实际预分配、验证底层可用空间、格式化并记录设备容量、文件系统、
mountinfo、st_dev/UUID、可用块/inode及quota；loop/mount能力只在盘配置步骤，不给PG/Reporter/Worker。
Linux工具/能力缺失则预检非零停止，不在宿主共享盘继续灌写，也不自动退到Docker Desktop模拟结果。

PGDATA、pg_wal、临时目录、PG日志落在受审专盘；默认不允许外部tablespace、WAL/日志符号链接，
必须存在的额外设备逐一登记、采样并取最保守门控。Docker日志/证据若在其他盘，显式登记其上限和
宿主底层压力；宿主APFS或Docker VM底层空间可能先耗尽，不能只盯容器内statvfs。

继续使用固定digest的PG18及fixture镜像、无上游控制网络和原testdb破坏性保护；不接公网目标。
Reporter和Worker分离登录、分离进程；Reporter无Docker socket、无业务父表读写、无Probe控制通道。
Reporter只需受审设备的statvfs，以及pg_wal/日志/temp固定目录的文件**元数据**读取。
拟申请目录r-x/必要默认目录ACL，文件仍由PG拥有且Reporter不能读写文件内容；
不得把PGDATA、私钥、配置凭据或整块设备readable地挂给Reporter。若实际DAC无法满足，停止并重审。
不跟随未知symlink；同inode去重，记录扫描起止/错误；open-deleted计量若无可信来源则标记不完整，
不能把目录扫描的较小值声称为真实完整日志占用。F仍包含这类分配空间。

独立fixture审计者复核PG SQL量、设备元数据和日志；审计者的已有可销毁DB管理员权限不能下放给
运行Reporter。可信Reporter或宿主被攻陷时仍可能伪报OS读数；这是一项**新增信任来源**，
不能用Worker自报、签一个无来源的JSON或仅验证SHA256消除这个信任假设。

## 6. 可信采样、时效性与故障状态机（待批准）

建议新增独立storage状态和单个注册Reporter：每deployment一个state行、最多一个当前Reporter行。
注册绑定session_user、incarnation、deployment generation、PG实例/启动标识、固定设备manifest摘要；
只能由离线fixture配置登记。Reporter不能改映射、预算或重新注册自己的身份。

候选采样周期1s；总有效期5s；采集/发布须在DB发出挑战后2s内完成。协议：

1. Reporter调用`storage_begin(incarnation)`，单个RC/2s事务。DB分配单调seq和一次性nonce，
   返回DB issued_at、发布截止、设备manifest/实例绑定。仅一个pending挑战，替换旧pending不延长已接受样本。
2. 确认Commit、释放DB连接之后，采固定设备/目录元数据；用本机单调时钟记录耗时，
   不跨OS扫描持业务锁。总耗时无法满足2s则上报unavailable，不以延长截止修复。
3. 调用`storage_report(incarnation,nonce,payload)`，每包<=4096 bytes、固定字段/有界设备清单。
   DB重新计算固定关系R，核验身份、nonce/seq、manifest、采样完整性及DB截止；
   样本有效期固定为`issued_at+5s`，不能从迟到的received_at重新获得5s。
   OS payload不接受任意路径、SQL、阈值、有效期或关系字节数；允许数值也需范围/溢出/一致性检查。
4. 在同一事务保存bounded state和兼容`disk_used_bytes=R`，不修改部署gate/emergency_stop/授权。
   缺失样本不默认healthy；已鉴权Reporter主动报告失败时提交unavailable状态，返回失败状态对象，
   不用随后RAISE导致这次失效写入被回滚。无法发布时旧样本仅能用至原5s截止。
5. Commit未知不重放执行能力；只对账原nonce/seq。允许同nonce/同摘要只读幂等确认，
   不延长有效期；不同摘要拒绝，旧pending、旧Reporter和旧设备代次都不能覆盖新状态。

同一`storage_report`入口还支持**只减权限**的失败信封：nonce为NULL、固定`state=unavailable`及
有界cause枚举，仅允许已绑定session_user/incarnation将storage状态置为不可用；不采信任何低水位数值，
不更新样本有效期、不创建healthy挑战、不修改其他gate。此分支无需有效采样nonce，防止采集失败后
还必须先成功采样才能拒绝。恢复healthy必须完整经过新挑战，旧Reporter代次不能失效或恢复新代次。

WAL/日志目录扫描与R不是原子物理快照。保存采集开始/结束，取有效期内保守值，另保留实测增长速率
与最多32个在途的余量。未证实扫描成本、漏项和增长上界前，不把该候选门控称为物理硬保证。
DB统计延迟/缓存、stats_reset或LSN只能用于趋势与诊断，不能充当本协议的新鲜样本。

| 状态/故障 | 新Claim/Start/Target Permit | 已有资源与恢复 |
| --- | --- | --- |
| 首次启动、NULL、无样本、invalid/unavailable、age>=5s | 拒绝，权威投影retention_capacity | 不补0、不使用无限期last-good、不自动打开部署gate |
| 达任一80%或独立预算/设备不明 | 拒绝 | 保留账；允许原安全写入及已批准清理，不删quarantine/WAL |
| 权限不足、目录/DB统计读失败、采集慢、队列延迟、断连 | 已报告失败立即拒绝；未能报告至多使用原样本至截止 | 无数据不能解释为0；不重发GET |
| 重启、设备替换、nonce重放、乱序、时间异常 | 身份/代次不符或时钟不可信拒绝 | 新可信注册/样本前保持不可用，不沿用重启前状态 |
| 恢复 | 所有量低于70%、身份/时钟可信且连续两份有效新样本后只恢复storage eligible | 不续授权、不重置冷却、不解除emergency_stop或其他gate |

普通vacuum可能只提供页内复用，R不会降回70%；此时保持blocked，即使已清掉大量历史行。
需要重写/重建索引或调整容量时另行审查维护方案与额外空间，不能自动VACUUM FULL、提高预算或手工改指标恢复。

TTL使用数据库可信时间。既有单调gate floor/2s时钟异常边界继续保留；Reporter以单调耗时比对
挑战前后DB时差，发现跳变即标记不可信，并由独立fixture看门狗停止本地新执行、提交storage不可用状态。
**限制**：仅有数据库墙钟不能证明任意时钟冻结/回拨情况下的绝对5s。
看门狗作为Reporter可信进程组的独立进程，持相同受限身份，只调用上述deny-only失败信封使
数据库storage门控失效；它不获得deployment gate写权。必要的部署gate关闭仍由现有可信离线fixture
管理者处理，不能声称看门狗已有该权限，也不将管理者的后续操作当作本设计已完成的自动停止证明。
后续必须验证看门狗单调截止与storage失效提交/本地停止通道；若不能证明DB时间可信或可信停止，场景判失败、保持关闭，
不能把时间异常测试改成“旧样本继续有效”。本轮不声称已有这项看门狗实现或权限。

## 7. 准入接入、锁序与已验收实现的保护

本轮M1–10、Go、测试、workflow逐字节保持原样。待审方案如获单独实施许可，才以**前向Migration11**
增加storage元数据、两项Reporter函数及私有布尔准入helper；禁止回写M8/M9/M10或改M9/M10保护。
未来需要替换函数体的具体差异见[精确申请](monitor-scheduler-task83-capability-request.md)，
不能因设计提交/推送或原则通过就默认为允许修改已验收核心。

拟将原disk比较处收紧为“原阈值 AND 可信storage eligible”：
`claim`、`start_run`、`reserve_target`、`reconcile_claim`、`authorize`、`execution_status`，
以及`recover_started`中仅决定未来调度的eligible分支。签名/原EXECUTE不扩大。
恢复unknown、持久结果、可信物理返槽及清理不得因磁盘门控失效而被新逻辑整体阻断；
授权新授予/登记可以拒绝，但Pause/PATCH、紧停不依赖healthy storage。
authorization/status读取一致性及现有固定错误/不泄漏跨租户统计契约需回归。

业务操作继续父序→Run→预算→Reservation→末段deployment SHARE/登记；
最终新SPI在全部等待后取一次gate_now，并读取该snapshot下已接受样本/截止，不在锁前hint决定。
Reporter更新固定顺序为deployment UPDATE→storage state→Reporter登记，不获取业务父资源/预算。
私有storage helper只读固定state；deployment锁将发布/业务门控线性化，不持storage锁反向进入父链。
发布先提交则后续Claim见新值；Claim先持SHARE则最多按当时有效样本提交，下一次必须重验。
压力在最后采样之后出现时存在最多5s检测窗及既有在途写入，不宣称瞬时全局磁盘预订。
采样不写热点预算行；额外control竞争须量化，不得延长2s/250ms掩盖历史125ms等待风险。

私有helper仅返回boolean，以固定合法gate_now调用；不向运行角色返回原始量或其他租户信息。
NOLOGIN owner与API owner的必要调用授权必须明确列入差异；普通API/Probe不能调用采样接口。
status与Claim对账仍是只读、同语句快照，不加业务锁或更新状态；status不隐藏已有in_flight/attempt，
即使storage拒绝新工作也保留已发生事实。`authorize`需在其既有父锁/预算等待后的新语句判定，
storage不合格时在推进g/写授权之前返回既有retention_capacity，不能只令permitted=false后继续授予。

## 8. 最小权限与能力申请（全部待批准，本轮不执行）

| 申请项 | 精确范围 | 排除与审查条件 |
| --- | --- | --- |
| 新Reporter身份 | 一个NOLOGIN组`devpilot_scheduler_storage_reporter`，离线配置一个独立LOGIN/固定incarnation | 非superuser、无BYPASSRLS/CREATEROLE/CREATEDB/REPLICATION；不继承任何owner/Worker/Controller角色 |
| `scheduler_private.storage_begin(uuid) RETURNS jsonb` | Reporter-only EXECUTE；单挑战/单调seq/固定manifest，身份与2s事务约束 | 不返回业务数据，不给任意部署选择权 |
| `scheduler_private.storage_report(uuid,uuid,jsonb) RETURNS jsonb` | Reporter-only EXECUTE；固定schema<=4096字节、一次性挑战、DB自测R、只更新storage状态/兼容R；NULL nonce仅允许deny-only失效 | 不可写阈值、授权、gate、emergency_stop、物理/容量账；无任意SQL/路径 |
| 私有`storage_admission(timestamptz) RETURNS boolean` | 仅既有NOLOGIN scheduler owner/API owner内部调用 | PUBLIC/所有运行LOGIN无直接EXECUTE，不给API owner新增表SELECT |
| 两个storage元数据表 | 单deployment state、单Reporter登记，包含挑战、最近样本与上次摘要，不存无限样本历史 | 既有NOLOGIN owner持有所需最小表权限；Reporter无直接SELECT/INSERT/UPDATE/DELETE |
| 文件系统观察 | 仅受审挂载statvfs及固定WAL/日志/temp目录的metadata ACL；专门UID | 无文件内容权限、原始盘权限、Docker socket、privileged/CAP_SYS_ADMIN；ACL具体差异另审 |
| 单调失效看门狗 | 与Reporter独立进程，复用同一绑定身份及storage_report的deny-only分支，监测主采样心跳/单调截止 | 不申请第三项EXECUTE或部署gate写权；该可信进程组/停止来源仍须单独批准，数据库不可达时本地停止不能冒充DB失效已提交 |
| 可销毁盘配置 | 仅一次性创建/挂载指定10GiB新设备及配置所需能力；PG启动前移除 | 不修改现有宿主/生产盘；不能把mount能力带入Reporter/业务进程 |
| 离线测量/维护fixture | 已有testdb守护后的管理员仅采诊断统计、测试准备、VACUUM/长事务屏障/专用填充目录 | 不是运行角色授权；不授pg_monitor、pg_read_all_stats、pg_read_server_files、pg_write_server_files或pg_execute_server_program |

两个Reporter函数和helper均固定search_path、全限定对象、无动态SQL、非LOGIN owner、REVOKE PUBLIC。
未来Migration11须在fixture gate关闭时原子生效/失败回滚；旧版本缺storage协议必须保持关闭。
Down须先关闭gate/排空或可信隔离在途；不能以回退为由恢复旧默认0即healthy。
新增身份/函数权限、OS ACL、看门狗控制来源以及前向函数门控替换是独立审查事项，均未获本轮许可。

## 9. 后续真实验证矩阵与失败条件（尚未运行）

先通过预检再串行运行，normal/race分开；PG事务仍<=2s、锁<=250ms，采样<=2s/TTL5s不放宽。
长负载用分阶段编排控制总时长，不延长单次Go事务或删除原断言。

| 场景 | 真实执行方法 | 必留证据/通过条件 |
| --- | --- | --- |
| E0 专盘预检 | 固定镜像digest/架构、testdb保护、设备/挂载/配额/底层空间/PG UID核对 | 10GiB容量/格式化T/A、实际预分配、无上游与权限原件；缺能力非零失败 |
| E1 周期数据增长 | 两个既有受限Worker、32个Monitor/8W，interval>=60s；真实120分钟调用Claim/未Start到期回收形成never_prepared终态；另以小组真实HTTPS生命周期同步测量，方法见下 | 原谱系、真实DB时间、每秒R/W/L/F、每阶段实际Claim/拒绝数；不伪造HTTP成功/物理证明 |
| E2 行cap与保守blocked | 延长同一授权周期观察M100/cap或过期记录阻止Claim，持续授权时清理保守拒绝 | 记录capacity与disk拒绝原因的独立证据；不为了“稳定增长曲线”放宽M9冷却 |
| E3 冷却后清理与复用 | 停新授权、等现存真实expires/keep_until截止，再用原8.2窄接口清理、普通VACUUM，再跑120分钟 | 无修改时钟/冷却；清理行数、指针、账本与物理分配曲线；复用是空间再利用而非强求文件变小 |
| E4 高水位与长事务 | 沿用已有容量fixture准备受控历史终态规模；独立连接持旧snapshot，受限清理+普通VACUUM，结束屏障后再观察 | table/index/TOAST、n_dead_tup估计、旧snapshot/backend_xmin/锁/实际vacuum；长事务分支有硬截止且最后回滚 |
| E5 1GiB关系阈值 | 在仅本测试的固定R清单关系上制造真实索引/页膨胀至858993460以上，不改disk数字或计数，保持原行cap/Trigger | DB服务端total与独立审计一致；两Worker均无新Run/Reservation/fence/容量变化，API显示retention_capacity |
| E6 10GiB盘80% | 独立故障注入者仅写专用filler文件并fsync，测实际分配+statvfs跨阈值；不能用稀疏文件长度 | 原R仍低、F真实跨阈值，新Claim拒绝；停止后删除仅测试filler，采两份低于70%新样本才恢复storage eligible |
| E7 WAL/日志/temp独立预算 | 专用目录有限写入/轮转；受控WAL高写入与checkpoint，记录累计生成量和当前segment分配；只在可销毁库准备 | W/L/temp任一高/不完整均拒绝；max_wal_size不当硬配额；不删WAL/禁fsync/关full_page_writes换通过 |
| E8 时效与来源 | 停Reporter、延迟发布、断DB、目录EACCES、缺字段/错设备/旧nonce/错实例/乱序/Commit未知及重启 | 无效报告不打开gate；旧样本截止、全状态无意外变动；受限Worker直接SQL也不能绕过 |
| E9 等待后过期/压力 | pg_locks/pg_blocking_pids观测真实父锁/末段control等待，期间样本过期或Reporter先提交高读数 | 最终新SPI拒绝；双提交顺序与全回滚；不能以sleep-only/锁前时间断言 |
| E10 安全写入与物理资源 | 压力/过期状态下真实HTTP Pause/PATCH、原Result/unknown/可信Supervisor返槽；至少一条原真实HTTPS fixture链 | 安全写入成功且不重发GET；无证明仍quarantine；不冒称本轮实现9.1完整周期闭环 |
| E11 角色/升级/回退 | 真LOGIN拒绝直接表DML/内容读取/伪造身份；前向迁移中途失败与重复up、closed-gate down | M1–10 hash不变、无PUBLIC/Worker/API新采集权限；无部分schema/GRANT；旧版本不恢复执行 |
| E12 采样开销/时钟/日志故障 | 分别测采样SQL耗时/control竞争、单调看门狗失联/时间异常、日志积压/轮转或证据盘满 | 采样达不到期限或停止不可信即失败；保留原约125ms风险，不加预算/skip自动放行 |

E1/E3合计至少240分钟真实周期写入，另加真实授权/最大一小时冷却及清理等待；
执行前给出确定总时限/磁盘停止点，首次失败原样保留，不能重复运行到全绿后只留最后一次。
E1另设最多4个Monitor的小组，使用原HTTPS接收端/Probe/独立Supervisor，每个允许的正常周期都真实
执行Start/单次GET/Result/签名终止证明，混入受控unknown/保留quarantine的独立分支。
固定fixture目标目录和原origin/IP限速不变；共享目标不能每分钟都启动时，记录实际受限吞吐，不绕过预算。
沿用120分钟窗口和原网络/事务截止；至少一个Monitor覆盖100次实际尝试或达到原保护上限后保守blocked，
每次保留receiver计数、唯一run/fence、终止/结果摘要与写放大。若预算/环境令该覆盖无法达到，应报告未覆盖。
这个小组用有限次数的fixture编排调用既有原语，不交付产品周期daemon；E10仍单独验证压力下安全写入。
仅never_prepared负载不足以代表Result/Reservation/物理证明的WAL和日志成本，两组必须分项计量，不能合并冒称覆盖。
这是有限fixture存储验证，不是7天/24h真实运行、1000W压力或无限期磁盘保证；这些边界继续留给后续任务。
高规模准备记录明确标注管理员fixture输入，不能计作真实周期产生的行/网络结果。

物理R阈值的最坏行/索引膨胀是否能在原cap内达到尚未知；若无法安全构造，保留失败并报告缺口。
精确bytes边界可另用纯准入函数边界输入测试，但**不能替代**E5/E6的真实测量→发布→受限Claim链。
不以管理员手填`disk_used_bytes`或缩小预算声称验证了1GiB。

采样按新短事务读取，趋势至少含：各表total/table/index、数据库总量；`pg_stat_wal`的wal_bytes及
stats_reset、同实例LSN差；实际pg_wal分配量；vacuum/autovacuum次数/时间、估计死元组、长事务；
checkpoint计数/耗时；PG/驱动日志分配和丢失/阻塞；设备块/inode与宿主底层空间。重启/reset分段，
不将累计WAL生成量当瞬时占用，也不将统计估计值当准入依据。

未来交付每场景的实际命令/退出码、版本/镜像/二进制/源码SHA、time series和边界前后同语句账本快照，
原始错误/环境inspect/设备manifest/锁图与正常/故障对照；大型原件留独立Artifact及ZIP SHA256/CRC。
WAL数据不出库作证据，日志禁止URL/Cookie/用户秘密/原始查询参数；只导出固定聚合和必要系统诊断。
设计审查通过也不直接勾选8.3；实施与独立技术验收仍需后续人类授权。

## 10. 文档依据与未关闭风险

- [PG18大小函数](https://www.postgresql.org/docs/18/functions-admin.html#FUNCTIONS-ADMIN-DBSIZE)：total包含索引/TOAST；数据库大小不是设备空间。
- [PG18统计](https://www.postgresql.org/docs/18/monitoring-stats.html)：累计统计存在刷新/快照及reset边界，适合趋势核验。
- [PG18普通vacuum](https://www.postgresql.org/docs/18/routine-vacuuming.html)：回收可复用空间与文件缩小不同；本方案不自动VACUUM FULL。
- [PG18 WAL配置](https://www.postgresql.org/docs/18/wal-configuration.html)：max_wal_size不是硬上限，不作为物理保护证明。
- [PG18日志配置](https://www.postgresql.org/docs/18/runtime-config-logging.html)、[Docker日志](https://docs.docker.com/engine/logging/configure/)：轮转行为须逐项配置并实际测量，默认不能证明有界。

上述是设计依据，不是本仓库已通过测试。原125ms全局锁等待、IPv6/管理确认超时根因仍未知；
持续授权无法清理、未知Commit、物理quarantine与证据保留边界均继承，不因新增磁盘采样而消失。
当前最大缺口为真实专盘、可信来源/时效与停止通道、增长/在途余量和测量成本尚未验证。
本轮到此停止，等待独立设计审查与精确权限/实施授权。

## 11. 本轮实际文档验证

`openspec validate add-monitor-scheduler-worker --strict`与`git diff --check`均exit0；
`openspec list --json`实际返回36/47，唯一任务勾选变化为8.2，8.3仍未勾选。
首次设计的九份新增/修改文件均为docs或本change的Markdown；303个M1–10/internal/tests/workflow受审核心文件
对本轮开始快照逐一SHA256一致，文档本地链接检查通过。原始结果在同一只读评估目录。
未重跑全库strict，历史四份规格七项长度告警未改；未运行E0–E12、PG/Go/网络测试、冷却或CI rerun。
以上为首次设计完成时的验证记录。后续人类已单独授权提交/推送本设计分支；
提交只含Markdown，交付完整SHA由提交后的Git事实给出，不把文档SHA当作8.3实施受测SHA。
该分支不在现有Go CI push分支白名单，本轮不创建PR或修改workflow触发完整长时CI。
提交复核补充了单独的精确权限申请，共十份文档；`authorize`仍为M8定义、status为M9 Up定义，
两项归属与拟议门控位置已按原SQL核对。E0–E12仍全部未执行，8.3保持待审。

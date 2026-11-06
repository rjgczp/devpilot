# Task8.3 — Migration/权限精确差异申请（仅设计，未实施）

基线：`0462dc2e65c8ea9edd3948cf6a72d483e93f7510`；36/47，8.3未勾选。
分支：`codex/task83-storage-design`。人类授权本轮提交并推送文档供ChatGPT独立设计审查，
不授权执行下列DDL/GRANT、数据库迁移、文件ACL、负载测试或任何生产能力。
整体口径、信任边界、故障协议与E0–E12验证见[主方案](monitor-scheduler-task83-storage-plan.md)。
下列均为未来候选Migration11的白名单；没有创建SQL文件。任何超出清单的对象/权限须重审。

## 1. Migration8–10是否足够：只读结论

| 现有版本 | 已有能力 | 对8.3仍缺少什么 |
| --- | --- | --- |
| M8 | deployment的disk_used_bytes/disk_budget_bytes、原数值比较、受限系统身份、物理停止/资源协议 | 没有独立storage来源/样本时间/代次/失效状态，disk默认0不代表真实可用空间；没有窄发布入口 |
| M9 Up | 当前Claim容量账及status投影；新增保留pending保护 | 行cap不是物理字节；未采表/索引/WAL/日志/文件系统；未增加可信刷新或过期拒绝 |
| M10 | 三项已批准Controller清理函数；缺G/W/P/M账23514失败关闭 | 只退役安全记录，不证明vacuum/WAL/日志/挂载余量；不能冒充采集或强制修账入口 |

因此M8–10足够保留现有行数/物理安全边界，**不足以交付真实来源、时效性和80%磁盘拒绝链**。
不能改写这些文件补功能，也不能让管理员/Worker直接UPDATE一个数字充当8.3验证。
以下前向差异是必要能力申请，不是对M1–10实施变更的授权。

## 2. 新增对象与列的候选清单

只新增`scheduler_private.storage_reporters`和`scheduler_private.storage_state`两个表，
两表owner均为既有`devpilot_scheduler_owner`；
各表用`singleton smallint PRIMARY KEY CHECK(singleton=1)`限制最多一行，不建历史时序表。
初始状态为unavailable；部署只支持一个数据库实例、一个当前Reporter可信进程组。

| 表 | 列/类型与约束 | 谁可以改变 |
| --- | --- | --- |
| storage_reporters | singleton；login_name name NOT NULL；incarnation uuid NOT NULL；deployment_generation bigint >0；instance_id uuid NOT NULL；registered_at/expires_at timestamptz；revoked boolean；manifest_digest bytea长度32；volume_manifest jsonb<=8192字节/最多4个固定设备 | 仅离线可信fixture配置；运行函数不得注册、更换login/代次、设备/阈值或续登记期限 |
| storage_state：固定绑定 | singleton；deployment_generation bigint >0；instance_id uuid；postmaster_started_at timestamptz；manifest_digest bytea长度32 | 初始离线登记；失配时不复制旧healthy，需重新审核登记 |
| storage_state：挑战 | sequence bigint>=0；pending_nonce uuid；pending_issued_at/pending_deadline timestamptz；同空/同非空约束，deadline=issued+2s | storage_begin；只保留一个pending；序号溢出拒绝，不回绕 |
| storage_state：接受样本 | accepted_sequence bigint>=0；accepted_nonce uuid；accepted_digest bytea长度32；issued_at/received_at/valid_until timestamptz；valid_until=issued+5s；relation_bytes bigint>=0；metrics jsonb<=4096字节/固定schema；status枚举unavailable/blocked/healthy；cause固定枚举；low_samples smallint BETWEEN0 AND2；last_gate_at timestamptz | storage_report；DB自算R，OS量来自绑定Reporter；低水位两份样本后才eligible |

正常接受样本的绑定/时间/摘要字段须同时存在；初始/失败状态不因NULL转换为healthy。
持久序列、挑战和最近摘要仅服务防乱序/对账，不增加无限历史/新业务谱系或网络许可。
manifest只包含固定设备身份、目录标签、字节预算与实例绑定；不含URL/用户秘密。
W/L/temp/F与inode用主方案口径；metrics不接收SQL、路径、关系名或调用者给出的R。
volume_manifest固定设备数最多4，不足或超限的拓扑先拒绝；不能默默忽略额外挂载。

新增表自身纳入R。现有`public.scheduler_control`不新增列、不扩kind枚举，不把Reporter伪装成Controller。
未来仅允许发布函数写deployment行的`disk_used_bytes=DB服务端实际R`；不写disk_budget_bytes、
verified_at/expires_at/manifest_digest、gate/emergency_stop、业务授权、物理回执或任何预算计数。
deny-only仅令storage不可用，保留最后测得R供审计；绝不能写0表示失败。

## 3. 新函数签名与精确ACL

新增组：`devpilot_scheduler_storage_reporter`，NOLOGIN/NOSUPERUSER/NOCREATEDB/NOCREATEROLE/
NOREPLICATION/NOBYPASSRLS；不加入任何既有运行组或owner组。
离线fixture配置一个独立LOGIN，其唯一新增membership为该Reporter组、无ADMIN OPTION；
LOGIN不具备上述高权属性。Reporter与看门狗是同一可信组的独立进程，不能声称看门狗持有更窄的DB权限。
该LOGIN具体名称/incarnation/有效期与设备清单在后续隔离环境准备审查中绑定，不在本轮创建。

| 对象/签名 | 定义/输入输出契约 | 新增授权 |
| --- | --- | --- |
| schema scheduler_private | 既有schema，不扩大CREATE/TEMP或public schema能力 | 仅给Reporter组USAGE；CONNECT仅使用现有数据库连接资格，不新增跨库权限 |
| storage_begin(p_incarnation uuid) RETURNS jsonb | VOLATILE SECURITY DEFINER；真实session_user必须为登记LOGIN且具Reporter membership，incarnation/代次/有效期匹配；RC/2s/250ms；返回seq/nonce/DB时间与固定manifest绑定 | REVOKE PUBLIC；仅Reporter组EXECUTE |
| storage_report(p_incarnation uuid,p_nonce uuid,p_payload jsonb) RETURNS jsonb | VOLATILE SECURITY DEFINER；鉴权同上；有nonce为<=4096字节固定schema的正常发布/同摘要对账；NULL nonce只允许deny-only unavailable；接受/失效必须实际Commit才算成功 | REVOKE PUBLIC；仅Reporter组EXECUTE |
| storage_admission(p_gate_now timestamptz) RETURNS boolean | STABLE SECURITY DEFINER，只在调用者语句快照读固定state/登记，不产生新快照或写入；有限时间/身份/来源/阈值任一不符即false；不另取时钟代替调用者等待后gate_now | REVOKE PUBLIC；仅既有NOLOGIN devpilot_scheduler_api_owner显式EXECUTE；scheduler owner作为owner已有执行权 |

三函数均由既有NOLOGIN `devpilot_scheduler_owner`拥有，固定`search_path=pg_catalog,scheduler_private`、
全限定表/函数名、无动态SQL；两个写函数固定lock_timeout=250ms并验证2s事务预算。
Reporter不得SET ROLE为owner或运行Worker/Controller/Supervisor/API身份；不调用既有system_identity假扮已批准类型。
身份鉴定直接绑定session_user及新表；不修改M8的system_identity或scheduler_control.kind约束。

两表对PUBLIC及所有运行组无直接SELECT/INSERT/UPDATE/DELETE/TRUNCATE/REFERENCES/TRIGGER授权，
Reporter仅取得schema USAGE与上述两个EXECUTE。新表owner的内置权限足以读写，不给API owner表SELECT。
既有运行角色的EXECUTE/角色membership/表列权限保持原样；仅新增API owner调用私有boolean的权限。
不授pg_monitor、pg_read_all_stats、pg_read_server_files、pg_write_server_files、pg_execute_server_program，
不授CREATE/TEMP、DDL或业务表权限。R由既有owner对固定关系执行大小函数，无需读取行内容或SQL明文。

## 4. 既有函数的拟议门控差异白名单

签名均不变，不新增运行LOGIN EXECUTE；只在原磁盘检查位置收紧来源/新鲜度，不替换原容量算法。
没有全局存储Trigger，不把存储检查塞进system_identity/lock_control造成全部安全写入被拒绝。

| 既有完整签名（scheduler_private下） | 基线有效定义 | 唯一拟议行为差异 |
| --- | --- | --- |
| claim(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,uuid) | M9 **Up**，不是Down中的旧拷贝 | 原父序/容量预留之后、末段control等待后新SPI取gate_now；原disk比较再AND storage_admission(gate_now)，失败整体回滚，不能留下Run/fence/预订/账变更 |
| start_run(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint) | M8 | 原最终门控追加storage predicate；不放宽授权/lease/fence/可信prepare/原Commit先于输入协议 |
| reserve_target(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) | M8 | 原最终许可预算门控追加storage predicate；拒绝不下发目标许可，不重试GET |
| reconcile_claim(uuid,uuid,uuid,uuid,uuid) | M8 | 原只读单语句追加storage predicate，不加锁、不返回新许可；保守NULL不等于原Commit已回滚，不新建替代run |
| authorize(bytea,uuid,uuid,uuid,uuid,bigint,bigint,bigint,integer) | M8 | 在原身份/权限/If-Match优先级之后、全部既有锁等待后新语句、推进g/写授权之前判storage；不合格返回既有retention_capacity，不只设置permitted=false后仍授予；其他gate分支不改 |
| execution_status(bytea,uuid,uuid,uuid) | M9 **Up** | 仍SQL/STABLE同语句快照，原retention_capacity分类条件加入NOT storage_admission(now)；原权限/原因优先级、历史attempt、current_observation、in_flight等不隐藏、不改谱系 |
| recover_started(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,text) | M8 | 只在released之后安排未来next_run_id/next_due的eligible分支追加storage predicate；started→unknown、quarantine、已签名释放事实及其对账必须仍可提交 |

上述函数owner、SECURITY DEFINER/search_path、所有既有角色授权不变。
写函数继续等待后新SPI；status/对账保持只读同语句快照，不能为“最新值”改为多语句不一致读取。
Reporter发布固定锁序deployment UPDATE→storage_state→Reporter登记，绝不取得业务父链/预算；
业务原末段deployment SHARE与发布线性化。新读helper不另加反向锁。
采样SQL在发布事务内计算固定R，文件元数据在挑战Commit后、事务外采集；不能跨目录扫描持业务事务。

**明确无门控变更的安全路径**：Pause/配置PATCH、紧停、commit_result/persist_minimal_result及对账、
accept_termination/reconcile_termination、quarantine_physical、expire_unstarted、M10三项清理函数。
原角色/谱系/物理证明/冷却/行数保护仍必须满足；“继续可用”不代表磁盘已写满后仍能成功写入。
在阈值处停止新工作后，E10必须在真实余量下证明这些安全路径；不能在真的ENOSPC后承诺数据库可写。
正在运行的Probe可按原许可硬截止完成，或由原Supervisor可信停止；磁盘门控本身不生成停止证明、
不返物理槽、不把已started改回leased，也不承诺瞬时取消已经发送的GET。

## 5. OS权限申请与失败/恢复边界

首版只接受新建独立10GiB测试文件系统。一次性设备创建/格式化/mount如需root或CAP_SYS_ADMIN，
仅限单独批准的盘配置步骤；不授权修改宿主已有盘、raw设备扫描或生产设备。
PG/Worker/Reporter/看门狗均不继承mount能力、privileged、Docker socket或宿主管理凭据。

Reporter用独立UID，仅有设备statvfs与固定WAL/日志/temp目录metadata所需的r-x/必要默认目录ACL；
PG文件仍0600归PG所有，Reporter无内容读写权限。已unlink日志由可信轮转器提供受限metadata，
若无该来源，样本判不完整，不能扩大为读取任意/proc/宿主文件。离线审计者沿用testdb管理员诊断资格，
不能把该资格授予Reporter。具体路径/UID/设备ACL实例需在未来受控环境中复核，未在本轮执行。

周期1s、挑战发布截止2s、最大允许样本年龄5s（自DB issued_at计算），失败报告单向失效；
失联不续last-good。数据库墙钟冻结/回拨不能单靠SQL TTL证明安全，独立单调看门狗需能通过
同一storage_report的NULL nonce失败信封实际提交失效，并停止本地新执行；不可达时不冒称已提交。
E8/E9/E12中不能证明时效/停止即判失败保持关闭，不把这一风险隐藏为实现已解决。

恢复storage eligible需要所有量低于70%、两份新的有效低水位样本及可信实例/时钟；
Reporter/watchdog从无部署gate打开权，不续业务授权、不重置冷却、不重放旧run/nonce/回执。
普通vacuum未让R回落时继续blocked；另行审查维护或容量调整，不在本申请内自动重写关系。

## 6. 前向迁移、回退与验收边界

若另获精确实施授权，候选新增一份Migration11；Migration1–10逐字节不改，不删除/禁用保护Trigger。
Up前必须关闭fixture gate、停止新Claim/Start并排空旧执行入口；保留未确认物理资源的原quarantine，
不拿“gate关闭”冒充物理终止。新对象、ACL及七个函数门控替换在同一事务内生效，中途失败全回滚。
初始storage为unavailable，必须完成可信离线注册/首批样本后才可能成为eligible，仍受原部署gate限制。

Down需先关闭gate、停止所有新执行入口并处理在途安全状态；不能退回M10后把默认0作为healthy。
若无法证明旧schema保持关闭且无任何自动恢复执行路径，则拒绝Down，保留M11关闭状态并前向修复。
迁移缺库保护、重复up、中途DDL失败、PUBLIC/真实LOGIN负向与所有M1–10 hash核查均列E11；
本轮没有运行迁移或这些测试。

审批对象包含：新增Reporter及看门狗信任、两个窄EXECUTE、API owner的私有helper调用权、
两张有界表与上述七个门控位置、metadata ACL和一次性新测试盘配置；不能只批准函数名却隐含任意表权限。
旧125ms全局锁等待、IPv6及管理确认超时根因仍未知；新增control竞争须测量，不延长2s/250ms掩盖。
设计推送不勾选8.3，不合并PR12、不Archive/部署，不批准生产迁移或生产互联网执行。

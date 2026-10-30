# Task 8.1 — 容量账迁移与最小权限申请（待批准）

日期：2026-10-30。仅为实施前设计申请，尚未创建 Migration9、函数、权限或测试。
本次用户仅授权将此设计文档提交至独立分支 `codex/monitor-capacity-design`；
不构成 Migration9、数据库权限或8.1实现的批准，不更新PR12实现分支。
设计所依据的本地、origin 实现分支及 PR12 HEAD：
`c31478bd3995216654a5f2206415426ade35dea8`。
实现基线分支：`codex/monitor-execution-status-consistency`；PR12 Open。
Task7.5 已获本轮用户独立批准；34/47，Task8.1 未完成。

设计复核修订：此前版本把预算初始化隐含在 Run INSERT 之后，不能保证首次 Claim
所需预算行存在。本版明确改变拟实现的 claim() SQL 顺序；不保留该旧方案。
这仍是待批准设计，下面的新顺序、Trigger 和迁移均未实施或进行 PG 验收。

## 依据与现状

对应 Design **D9** 的容量条款、Task8.1，以及 Delta `monitor-runs`：
`Retention has enforceable count time and capacity bounds` / `Exception - Cleanup cannot keep up`。
D11 是状态投影；容量阻断原因须与它保持一致，不改已批准的产品语义。

Migration8 已有如下基础，不能把它们重新计作本轮成果：

- Claim 原子预留 global/W/M 的 `terminal_count`，同时取得 32/4/2/1 物理槽。
  虽字段名为 terminal_count，实际含义是所有保留 Run 的未来终态容量预留；
  物理返槽不减它，结果提交也不再加它。
- Schedule 与 budget key 插入触发器各限制10000；已有冷却/引用删除保护。
- 可信终止回执与 HTTP Result 是两个事实，unknown 保守策略保持。

需补齐的差异：

1. 已返物理槽但尚未提交结果的 started Run，仍是未终态记录；现有 active_count
   跟随物理槽减少，不能单独证明全库未终态/未释放隔离记录始终最多32。
2. Run 记录账需要覆盖插入、状态转换、删除保护及事务失败，避免分散更新导致漂移。
3. Workspace 终态容量耗尽目前可能归为 resource_limited，须与并发槽不足区分，
   并向权威 status 提供固定 retention_capacity，不暴露跨租户数量。
4. 7天到期不等于空间已回收；8.1 不得借 TTL、Lease 到期或容器删除减账。

## 申请一：新增前向 Migration9

拟新增 `migrations/00009_enforce_scheduler_capacity.sql`，Migration1–8 字节不变。
只在现有 testdb 守护的可销毁 PG18 验证，不执行生产迁移。

在 `public.scheduler_budget_state` 增加：

```sql
retention_pending_count integer NOT NULL DEFAULT 0
```

CHECK：global 行为0..32，其他 kind 必须0。该账独立于物理 active_count/in_use。
计入 predicate：`attempt_state IN ('leased','started') OR physical_release_confirmed_at IS NULL`。
故物理已释放但仍 started 的 Run 仍计入；unknown 未确认终止仍计入。
只有已终态且可信物理释放已确认（或既有 never_prepared 协议成立）才退出此账。
单纯结果消息、Lease 超时、Controller/Worker 自报均不能释放物理槽。

保留现有未来终态预留策略：global retained/reserved<=100000、W<=10000、M<=100。
这比把100000个终态与32个待终态全部同时用满更保守；不扩大现有可用容量。
单独再限制 pending<=32，避免通过提前物理返槽累积无限待结果 Run。

迁移在关闭 fixture 执行 gate、隔离写者的维护步骤内核对实际 Run、Schedule、key
与资源账；不通过覆盖不一致旧账来制造空间。发现超额或账实不符须整个回滚并报告。
不推进 v/g/epoch/fence，不撤销或恢复历史物理证明，不制造结果，不删除存量配置。
Up/重复Up、DDL/回填故障及事务回滚必须测试。
Down 仅用于已停止、排空且无保留 Run 的可销毁库；不在有资源时撤掉保护。

## 申请二：一个内部触发器函数，无新运行角色 EXECUTE

```sql
scheduler_private.run_capacity_accounting() RETURNS trigger
```

拟为 VOLATILE SECURITY DEFINER，现有非 LOGIN `devpilot_scheduler_owner` 持有，
固定 `search_path=pg_catalog,scheduler_private`，完全限定对象，不用动态 SQL。

```sql
ALTER FUNCTION scheduler_private.run_capacity_accounting()
  OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.run_capacity_accounting() FROM PUBLIC;
```

不向 Worker、Controller、Supervisor、API 或其 LOGIN 身份授予该函数 EXECUTE。
仅由 `monitor_runs` 的容量触发器进入，不是新的结果、回收或管理 API。

- INSERT：Claim **先初始化并按 G→W→P→M 锁定预算行，再 INSERT Run**。
  非延迟的行级容量 Trigger 只对这些已存在、已锁定的行原子预留 retained 与 pending；
  不调用 lock_budgets、不创建预算行、不自行补取锁。缺行、计数不一致或容量不足即失败。
  Claim 中原有 retained 增量移入 Trigger，避免双计；物理槽增量仍由 Claim 执行。
  下节给出必须替换的 SQL 顺序，不以加注释代替重排。
- UPDATE：仅按旧/新 predicate 的变化调整 pending；不重复预留终态空间。
  现有 Start、Result、unknown、终止回执、未准备租约回收路径必须在 Run 之后、
  Reservation 之前已锁预算；Trigger 不初始化预算行、不调用 lock_budgets，不能在
  Reservation 或控制锁之后首次补取预算锁。predicate 不变时不更新容量账。
- DELETE：容量账基础保护只允许已终态、无未确认物理权限、无活动引用的记录；
  拒绝删除仍被 Schedule/Reservation 引用的 Run，禁止 TRUNCATE 绕过账。
  合法删除与减账同事务；调用者必须先按父序预锁，Trigger 不能反向锁父级、Run、
  Reservation 或控制行。本批不提供删除入口、清理循环或投影指针修复。

### Claim 新旧 SQL 顺序：首次登记必须先有预算行

源码基线：`migrations/00008_create_monitor_scheduler.sql` 的 claim()，598–657行；
lock_budgets()，566–579行。以下伪 SQL 展示实际操作顺序，省略参数不代表省略校验。

```sql
-- 旧 Migration8（原文件保持不变）
system_identity('worker', incarnation);
lock_parents(...);               -- User / W / Membership / P / M / Activation / Schedule
lock_existing_outstanding_run(); -- 若存在则失败，不再领取
INSERT INTO public.monitor_runs (...);  -- 此时首次 W/P/M 预算行可能尚不存在
lock_budgets(wid, pid, mid);     -- 之后才创建并锁 G/W/P/M
INSERT INTO public.scheduler_reservations (...);
lock_control(incarnation);
fresh_time_and_authorization_check();
increment_physical_slots_and_retained(); -- 原有混合增量
finalize_claim_and_schedule();
```

拟由 Migration9 替换 claim() 的函数体，完整顺序如下：

1. 身份与原父级锁：先 system_identity；候选 Owner User SHARE → Workspace
   NO KEY UPDATE SKIP LOCKED → Membership → Project → Monitor → Activation → Schedule。
   不在 Workspace 后改选 Owner。现有 outstanding Run 检查/锁仍在预算之前，
   有冲突直接回滚；不得持预算锁后再寻找或锁定另一个既存 Run。
2. 调用现有 lock_budgets(wid,pid,mid)：先锁 global；在其保护下按 W→P→M
   INSERT ON CONFLICT DO NOTHING 并逐行 FOR UPDATE。首次 Claim 因而有完整预算行。
   预算 key 的10000限制仍由既有 Trigger 强制；初始化失败不得留下半套预算。
3. INSERT 新 monitor_runs。容量 Trigger 原子增加 G/W/M retained 及 G pending；
   全部更新命中步骤2已锁的行，不能把“未命中预算行”当成预留成功。
4. Claim 单独条件更新物理并发账：G 的 in_use/active_count、W/P/M 的 in_use，
   保留32/4/2/1约束；**不再增加 retained 或 pending**。这里是事务内暂存的预订，
   不是成功 Claim 或可执行网络能力；物理槽不足也使步骤2–3全部回滚。
5. 创建 Reservation，然后取得原 control 锁；所有等待完成后，以新 SPI 命令和
   clock_timestamp() 复验身份、Owner、完整scope、v/g/epoch、期限、冷却、部署许可与stop。
   最终时间/keep_until、claimed_at/lease、Schedule 和控制账使用复验后的 gate_now。
6. 同一 READ COMMITTED 数据库事务内提交，确认 Commit 后才承认 Claim。
   任意初始化/Trigger/配额/授权/超时/取消失败均全量回滚，不能捕获后保留预订并继续。
   Commit ACK 未知沿用固定 Run 身份对账，不重新 Claim 或重新创建容量预留。

```sql
-- 新设计（未实施）
system_identity('worker', incarnation);
lock_parents(...);
lock_existing_outstanding_run(); -- 既存 Run 的所有加锁在预算之前；有冲突即失败
lock_budgets(wid, pid, mid);     -- 初始化并按 G→W→P→M 锁定，先于新 Run INSERT
INSERT INTO public.monitor_runs (...); -- Trigger 仅在已锁行上预留 retained / pending
increment_physical_slots_only();       -- 不重复增加 retained / pending
INSERT INTO public.scheduler_reservations (...);
lock_control(incarnation);
fresh_time_and_authorization_check();
finalize_claim_and_schedule();
COMMIT;                         -- 唯一持久化边界；此前所有更改一并成败
```

新增 Run INSERT 是创建本候选自己的记录，不是预算之后锁一个共享既存 Run。
其完整scope、next_run_id、fence来自已锁 Schedule；不由调用者任意指定另一个 Run。
既存 outstanding Run 的检查必须保留在步骤1，不能为了重排把它移到预算之后。
重复 ID/唯一约束冲突必须在原预算内失败回滚，不能在同一事务里改选身份或无限等待。

### 其他 Run UPDATE/DELETE 路径的只读锁序核查

下表核查同一 Migration8 源码；行号指本设计基线，不代表8.1实现或运行证明。
代码目录 internal/ 与 cmd/ 未找到直接 monitor_runs INSERT/UPDATE/DELETE；运行写入口在这些
受限 SQL 函数中。容量 Trigger 只能更新已持有的预算行，SQL UPDATE 的隐式行锁也必须
是对既有锁的重入；不得把 Trigger 内 UPDATE 视作“不需要锁序”的操作。

| 入口与源码行 | 现有首次锁获取顺序 | 拟新增 Trigger 行为与兼容条件 |
| --- | --- | --- |
| claim，598–657 | 父级→既存 Run 检查；原 INSERT 后才锁预算 | 按上节实质重排。最后更新新 Run 的 claimed_at 时，预算及新 Run 已由本事务持有，predicate 不变，不再加账 |
| start_run，771–820 | User/父级→Run FOR UPDATE→G/W/P/M→Reservation→Control/Supervisor control→UPDATE Run | leased→started 的 pending predicate 都为true，不改容量；Trigger 不取新锁 |
| expire_unstarted，934–975 | 父级→Run→G/W/P/M→Reservation→Control→UPDATE Reservation/Run | never_prepared 已由原协议确认时，expired退出pending一次；已准备但未释放则仍占pending。仅更新已锁G，不减retained，不伪造物理证明 |
| accept_termination，1209–1293 | 父级→Run→G/W/P/M→排序origin/IP→Reservation→Control→返物理槽→UPDATE Run | started仅增加物理证明仍占pending；unknown/expired等已终态才退出pending一次。Trigger只更新已锁G，不回头锁Run/父级/目标key |
| persist_minimal_result，1324–1382 | 父级→Run→G/W/P/M→排序origin/IP→Reservation→Control→UPDATE stop_reason | 仅stop_reason变化，predicate不变，无容量增减；旧network写路径继续拒绝 |
| commit_result，1395–1463 | 父级→Run→G/W/P/M→排序origin/IP→Reservation→Control→UPDATE Run | 原物理证明门控不变，started→completed退出pending一次；retained早已预留。相同commit_id重提不再执行首次转换，不重复减账 |
| recover_started，1518–1581 | 父级→Run→G/W/P/M→排序origin/IP→Reservation→Control→UPDATE Run | 保守started→unknown；未释放则pending不变，已有可信物理释放才退出pending。安排未来Schedule身份不INSERT新Run、不退款retained |
| DELETE | 当前运行函数没有Run DELETE入口，普通运行角色无表DELETE权限 | 不在8.1引入入口。未来调用者须父级→目标Run FOR UPDATE→G/W/P/M→必要目标key→Reservation，再处理引用后DELETE；Trigger只校验和更新已锁账，不代做预锁/清理 |

prepare_run、consume_input、reserve_target、quarantine_physical 以及各 reconcile 入口
不执行 Run INSERT/UPDATE/DELETE，因此不触发新增容量 Trigger；不为了容量账扩大这些
函数的行为或权限。父级撤销 Trigger 更新 Schedule，不借 Pause/PATCH 删除或重写 Run。

Supervisor 与 Recovery 并发修改同一 Run 时先在共同父级/Run 上串行，再取得同方向预算锁；
Trigger 不在持有 Reservation/Control 后新增 G/W/P/M 锁依赖。不同 Workspace 的 Claim
在全局预算行串行预留，持有预算后不能反向选择既存 Run。初次预算创建也在同方向完成。
因此本设计不引入“预算→既存Run/父级”或“Reservation→新预算锁”的反向边。
DELETE 调用者未预锁、预算缺行、活动引用未解除时不能由 Trigger 自行补救并放行。
该结论是源码与拟定操作顺序的兼容性结论，**不是已实现或真实PG无死锁的验收结论**；
获批实施后必须以首次Claim缺预算、Claim/Supervisor/Recovery交错、最后一槽及故障回滚
的真实PG18锁图、独立连接、normal/race验证，失败则停止，不扩大权限或更改超时。

运行事务保持原有父序、250ms lock_timeout、2s 预算；不新增容量表或反向锁。
容量失败固定 retention_capacity，物理并发不足仍为 resource_limited。
满容量不阻止 Pause/PATCH、已预留 Run 的结果确认、unknown、安全停止或可信物理返槽。

通过 Migration9 CREATE OR REPLACE 更新以下既有函数；签名、owner、角色 EXECUTE 不扩大：

```sql
scheduler_private.claim(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,uuid)
  RETURNS jsonb
scheduler_private.execution_status(bytea,uuid,uuid,uuid)
  RETURNS TABLE(decision text,data jsonb)
```

Claim 接入原子记录账并区分固定容量错误；status 仍单语句、实时鉴权、固定脱敏响应，
只增加对应容量阻断判定。Schedule/key 延用现有10000限制，不新增授权或执行能力。

## 申请三：新增列的有效 ACL 差异

现有非 LOGIN scheduler_owner 拥有 budget 表 SELECT/UPDATE；新增列会继承它，
用途仅维护容量账。现有 API owner 的表级 SELECT 将允许读取新计数用于 status；
不能将这项有效列权限变化称为“零权限差异”。

API owner 当前也有表级 UPDATE。为使它不能写新计数，迁移改为仅保留全部旧列 UPDATE：

```sql
REVOKE UPDATE ON public.scheduler_budget_state FROM devpilot_scheduler_api_owner;
GRANT UPDATE (
  kind,budget_key,in_use,tokens,last_refill_at,not_before,keep_until,
  terminal_count,active_count,schedule_count,key_count
) ON public.scheduler_budget_state TO devpilot_scheduler_api_owner;
```

这不扩大旧列能力；新列 UPDATE 仅 scheduler_owner，不授予 API owner。
不新增角色、成员关系、LOGIN 属性、secret 读取、DDL/CREATE/TEMP 或运行角色表权限。
新触发器不能成为任意 SQL/跨租户写入入口；既有完整 scope、Owner、身份与 Fence 门控不变。

## 7天与后续任务边界

7天依据已持久化 finished_at 与数据库时间，不能用查询时钟重置起点。
到期记录继续占用真实容量；有活动引用、冷却或未确认物理权限时不视为可回收。
8.1 验证到期/无安全空间时拒绝需要新增容量的操作，不以“过期”虚构减账。
实际删除、每批<=100行/2s、父序 keyset 与投影指针原子维护严格留给8.2。
所以本批不得宣称自动7天物理清除或完整长期保留机制已完成；实际时间回收验收仍依赖8.2。
1GiB/WAL/磁盘测量留8.3，行数不作为磁盘安全证明。

## 获批后的真实验收计划（尚未执行）

| 项目 | 必要证据 |
| --- | --- |
| M/W/global/count 边界 | 真实持久 Run 填充，独立连接竞争最后一槽；不能只把计数 UPDATE 到边界冒充真实数据 |
| 首次Claim与锁序 | W/P/M预算行起初不存在，证明预锁初始化先于Run INSERT；保存与Supervisor/Recovery交错的pg_locks/pg_blocking_pids及事务时间线，失败无Run/Reservation/计数残留 |
| pending 与物理账分离 | 已释放但未结果、unknown 未释放、结果/回执两个顺序；真实 Supervisor 证明，零提前返槽 |
| Schedule/key10000 | 不同 Workspace 并发最后一槽、插入失败全回滚、冷却/引用数据不被当作空位 |
| Result/状态转换 | 首次提交、同 commit_id 重提、旧 fence、unknown、转换中故障；账无负值/双计 |
| Commit未知/重连 | 真实 PG18 Commit 成功丢 ACK、实际回滚、连接恢复后账实核对；不再发 GET |
| 容量耗尽安全操作 | 真实 HTTP Pause/PATCH、授权登记失败原子回滚、停止及回执返物理槽仍可执行 |
| 时间边界 | 7天到期仍占空间，不能删在途/隔离/有引用 Run；不把8.2标为完成 |
| 迁移/ACL | 0→9、8→9、重复Up、失败回滚、受保护 Down；旧列权限保持，新列写入和内部函数调用拒绝 |
| 回归与交付 | PG18 normal/race、受影响 Go unit/race/vet、原7.1–7.5及6.3–6.5/5.4、API零目标审计、Change strict、diff check；冻结 SHA 的真实 Push/PR CI |

只保存实际命令、退出码、源码 SHA/清单与必要原始日志。历史失败证据不删除；
历史 IPv620s 未确定根因与精确 canceled 证明缺口继续保留，不重复宣称修复。
完成8.1后停止；不进入8.2、不合并PR12、不Archive、不部署，生产执行硬关闭。

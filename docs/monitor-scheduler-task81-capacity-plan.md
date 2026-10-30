# Task 8.1 — 容量账迁移与最小权限申请（待批准）

日期：2026-10-30。仅为实施前设计申请，尚未创建 Migration9、函数、权限或测试。
本次用户仅授权将此设计文档提交至独立分支 `codex/monitor-capacity-design`；
不构成 Migration9、数据库权限或8.1实现的批准，不更新PR12实现分支。
设计所依据的本地、origin 实现分支及 PR12 HEAD：
`c31478bd3995216654a5f2206415426ade35dea8`。
实现基线分支：`codex/monitor-execution-status-consistency`；PR12 Open。
Task7.5 已获本轮用户独立批准；34/47，Task8.1 未完成。

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

- INSERT：在 Run 阶段之后按 G→W→P→M 锁现有预算行，原子预留 retained 与 pending。
  Claim 中原有 retained 增量随之移入此处，避免双计；物理槽增量仍由 Claim 执行。
  任何后续鉴权、预算或 Commit 失败都与 Run 一并回滚。
- UPDATE：仅按旧/新 predicate 的变化调整 pending；不重复预留终态空间。
  现有 Start、Result、unknown、终止回执、未准备租约回收路径必须在 Run 之后、
  Reservation 之前已锁预算；触发器不能在 Reservation 后首次补取预算锁。
- DELETE：容量账基础保护只允许已终态、无未确认物理权限、无活动引用的记录；
  拒绝删除仍被 Schedule/Reservation 引用的 Run，禁止 TRUNCATE 绕过账。
  合法删除与减账同事务；本批不提供删除入口、清理循环或投影指针修复。

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

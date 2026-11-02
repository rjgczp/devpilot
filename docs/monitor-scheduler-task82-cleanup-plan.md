# Task 8.2 — 安全清理设计准备（待独立审查，未授权实施）

日期：2026-11-02。基线为已验收的 `0d2f5677dd6a2439ab010b6a8ec807941b1403cd`。
Task8.1已获用户独立技术验收批准，35/47；8.2仍未完成。
本文件是设计申请，不是Migration10、函数或GRANT的实施许可。本轮只提交文档。
Migration1–9、生产实现、测试断言不变；生产执行硬关闭。

## 1. 规格映射与范围

| 已批准依据 | 本设计的处理 | 后续真实验收要求，当前未执行 |
| --- | --- | --- |
| Task8.2；D6 Result/回收/清理父序 | 每事务一个完整Monitor scope，不补锁User/Session/Membership | 与Start、Result、Recovery、Supervisor交错的pg_locks、阻塞PID与提交时间线 |
| D9；monitor-runs / Retention has enforceable count time and capacity bounds | 只删可安全回收的终态及其已释放引用；到期但不能安全删除时继续占账 | 7天边界、容量满、无空间、并发最后槽、回滚/ACK未知 |
| Scenario: Concurrency - Bounded cleanup preserves references | keyset、有界批次、原子维护两个Schedule指针 | 清理与HTTP读取/Result并发，无悬空引用；unknown不被旧success替代 |
| Scenario: Exception - Cleanup cannot keep up | 无可删空间时保持retention_capacity，Pause/PATCH和紧停继续可用 | 满容量下真实HTTP与独立物理停止回归 |
| D9 Schedule/预算key回收 | Run清完、授权失效、冷却与引用消失后才退役；Fence不能随活动Run丢失 | Schedule重登记、旧Run/Receipt/Fence重放、HMAC轮换与旧key一小时保护 |
| Scenario: Security - Disk growth is measured separately from row limits | 不把DELETE提交当成磁盘/WAL已回收 | 实际物理磁盘验证留8.3，本批不实施vacuum编排 |

不新增历史查询API，不改变Result/unknown/物理返槽协议，不执行Probe或目标网络请求。
清理只返还记录容量，不返还Start令牌、物理槽或目标并发，也不重建执行许可。

## 2. Schema9的实际依赖，不能直接写DELETE循环

只读核对 `migrations/00008_create_monitor_scheduler.sql` 与
`migrations/00009_enforce_scheduler_capacity.sql` 得到：

- Run以完整scope/run为主键；Reservation FK指向Run，Schedule两个最近指针FK也指向Run。
- M9 `run_capacity_accounting()` 的DELETE分支拒绝leased/started、物理证明为空、任何Reservation引用、
  最近指针引用、当前Schedule授权/近期Start/冷却或Monitor预算冷却未结束的记录。
- `schedule_capacity()` 禁止删除仍有Run、未过期授权或冷却未满的Schedule；`budget_capacity()`
  禁止删除global、在用/冷却key、仍被Run或Reservation引用的key。
- 非LOGIN `devpilot_scheduler_owner` 已有这四张表的DELETE权限；运行角色没有清理EXECUTE，
  也不能获得表DELETE。现有候选扫描是Worker-only到期Claim扫描，不能冒充Controller清理枚举能力。

因此建议未来用**单独申请的前向Migration10新增窄入口**，不编辑M1–9，不删除或禁用既有Trigger。
先解除确实可退役的引用，再让现有DELETE Trigger减账。绝不用管理员UPDATE计数、删容器或Lease到期充当物理证明。
本准备版本保留M9对scope授权和冷却的全部拒绝条件，没有申请放宽它们。
这意味着持续续授权/冷却中的scope可能无法清掉过期历史并持续blocked；这是明确的可用性限制。
如果产品要求在持续授权期间也能清理，须先独立审查更精细的Run级保护及前向迁移差异，不能在实施时悄悄放宽。

## 3. 候选与删除资格

候选只是无锁hint。按完整scope轮转；同一scope按 `(finished_at, run_id)` 升序keyset，禁止OFFSET。
每页最多64个候选scope或目标key；单次Run候选最多100条，只从中形成预算内的小批次。
全局/W容量压力从同一轮转中选择scope，不持G锁扫描/锁定其他Monitor；不能因压力删更新或在途记录。
优先清理 `finished_at <= gate_now - interval '7 days'`；数量压力下再选最旧安全终态，
压力是否存在由数据库锁后计数决定，调用者不能用一个布尔参数授权任意删历史。

每个Run必须在锁后重新满足全部条件：

1. 完整scope、固定身份/谱系、终态且finished_at存在；不是leased/started，不把unknown改成completed/canceled。
2. `physical_release_confirmed_at`已确认。曾prepared者还须对应released Reservation，
   保留原Supervisor签名适配器已验证/持久化的完整终止事实与incarnation绑定；清理函数不自行签发/推测证明。
3. 未prepared者只认既有never_prepared协议；有矛盾、缺少已准备记录或仅有Worker报告则拒绝。
4. Reservation非quarantined/reserved，目标许可已不可恢复；引用及所属scope/key冷却全部结束。
   全部冷却/最大一小时安全保留/HMAC轮换旧key保护取更晚截止时间，不因删除而缩短。
5. 不参与未完成Result提交、物理释放或活动引用。不能确认时保留原记录和容量。

游标只控制扫描位置，不证明资格；非法/超长游标拒绝。页尾无锁hint失效时重新开启有界事务，不能持锁改选scope。
游标不永久跳过尚不合格的历史：一轮结束重置，之后重新复验；不形成无界排队或同tick忙重试。

## 4. 父序、时间与事务界限

Run清理事务固定为：

```text
READ COMMITTED / system_identity(controller, incarnation)
Workspace NO KEY UPDATE -> Project UPDATE -> Monitor UPDATE
-> Activation UPDATE -> Schedule UPDATE
-> 全部候选既存Run UPDATE（同scope，完整key排序；任何待删Run都在预算锁前取得）
-> 既存预算 G -> W -> P -> M -> origin/IP（类型与完整key排序）
-> 对应Reservation（完整key排序）-> 末段control
-> 新SPI语句复验身份/全部删除条件，取得一次gate_now
-> 原子指针处理、删除released引用、删除Run、必要的登记/key退役
-> Commit
```

不要求仍有active Owner，也不在后面补取User/Session/Membership。
不复用会初始化预算行的lock_budgets来做清理；只锁既存账，缺账则停止并报告，不能生成冷却或修平差账。
锁等待后用新的SQL语句、`clock_timestamp()`和已有单调/时钟异常保护，不使用事务开始时间判断TTL。
Controller的可信登录/incarnation必须在最终门控仍有效；暂停/部署关gate不构成新增网络许可。
清理不能占用Worker专用control助手，不伪造Worker身份。

全事务包括Commit最多2秒，`lock_timeout<=250ms`，不得延长。
每事务最多100行变更，保守计入指针UPDATE、Reservation/Run/Schedule/key DELETE和Trigger账本写入。
首版每scope最多8条Run；先计算完整写入上界，含附带操作可能超100则缩小批次/分后续事务，禁止先超额再回滚充数。
锁完Run再发现另一个待删/待替换Run时，回滚后另起事务，不能在G/Reservation/control后补锁Run。
取消、超时、deadlock、CHECK/计数异常全部回滚；不吞异常部分提交。
任意事务与连接在清理提交后释放，不跨HTTP或独立Supervisor操作。

### 仅预算key的独立事务

origin/IP key回收只取G -> 固定kind/key预算行 -> 末段必要control，绝不回头获取业务父链。
在G保护下新语句检查所有Reservation引用、in_use、not_before、keep_until及轮换保护；
任何历史Reservation仍引用该key也不删除。每调用只删一个key，原Trigger递减key_count。
不同scope共享同origin/IP时不从当前scope反向锁另一个scope。
W/P/M预算回收在scope父序事务中另验全部相关Schedule/Run/Reservation均不再需要该key，
保守保留仍有登记的父级预算，不能只因当前Monitor的Run清空就删共享账。

## 5. 投影、回执与容量的原子处理

推荐清空被删Run对应的 `last_attempt_run_id` / `latest_result_run_id`，与DELETE同事务。
不需要选择并补锁另一个Run，HTTP单语句快照只能见到完整旧态或完整新态，读不到悬空指针。
为避免旧success掩盖unknown，若删除最近attempt则同时清空latest_result指针；不自动回退到更旧成功结果。
不改v/g/epoch/Fence、授权期限、next_due、last_started、next_start_not_before或Activation ETag。
若未来要替换指针而非清空，应先单独审查latest-attempt/latest-completed及当前谱系门控，不能借清理重写观察。

released Reservation只能和对应已可删Run同事务删除；不得先独立删回执以绕过M9保护。
其单次消费nonce/digest账随保留期结束退役后，不再提供该历史Run对账；这是有界历史边界，须在运维文档声明。
旧回执/旧Result/旧Run重放必须因完整scope/Run已不存在或当前代次/Fence不符而拒绝，不能释放新Run的槽。
Run ID不得在清理后人为复用；保留Schedule时last_fencing_token绝不重置。
有任何Run时不删Schedule；Schedule退役仅在所有Run/引用为空、授权无效且完整冷却期结束后。
后续重新登记必须走原Owner重新授权并推进g，不能恢复旧tag、旧授权或旧执行能力。
删除回执账后的防重放结论是待真实PG验证的设计要求，不是本轮已通过事实。

Run DELETE仅由原Trigger递减G/W/M retained；允许删除的Run已不占pending，pending不再减一次。
不手工更新retained/pending，不触碰in_use/active_count或补充tokens。
Schedule/key删除由原Trigger各减一次；不存在时重复清理返回无变化，不生成新的数据。
ACK未知只重读/重新评估当前保留事实：未找到不当作HTTP失败，也不重发GET、重开Run、返物理槽。
没有证明Commit结果时，不声称精确删行数量；下一次安全清理可对尚存合格数据工作。

## 6. 最小权限申请草案（未批准，未执行）

建议三项窄Controller-only能力；先设计审查和单独授权，再申请实施：

| 拟签名，均RETURNS jsonb | 参数及能力边界 |
| --- | --- |
| `scheduler_private.retention_candidates(uuid,text,integer)` | incarnation、最大512字节结构化keyset游标、limit1..64；仅返回完整scope或origin/IP key清理hint及下一游标，不锁业务行、不返回URL/网络原件、用户秘密或全部Run历史 |
| `scheduler_private.cleanup_monitor(uuid,uuid,uuid,uuid,timestamptz,uuid,integer)` | incarnation、wid/pid/mid、可空且配对的finished_at/run_id游标、Run limit1..8；仅做上述scope清理和符合全部保护的登记/父预算退役，总写入<=100，不改授权/结果/物理释放 |
| `scheduler_private.cleanup_target_key(uuid,text,text)` | incarnation、固定origin或ip、规范64位HMAC key；仅退役一个已无引用且保护到期的key，不能读HMAC密钥、换key或清空冷却 |

候选分页分为scope/key两个固定段，由游标标记段，不接受表名/SQL片段/任意过滤器。
同一Monitor一批事务；候选函数不授予删除资格，最终函数逐语句重新校验。

未来最小ACL差异为：三函数由既有非LOGIN `devpilot_scheduler_owner`持有，
VOLATILE SECURITY DEFINER、固定可信search_path、完全限定对象、无动态SQL；
REVOKE PUBLIC EXECUTE，仅GRANT EXECUTE给既有 `devpilot_scheduler_controller`。
不向Worker、Supervisor、API、PUBLIC授予新能力；不增加表/列/秘密权限或角色属性。
owner已有表DELETE，**这不表示Controller已有清理授权**。新增EXECUTE本身仍需单独批准。
不增加可信事件来源、回执密钥或管理员override；没有“force”参数，不允许清理未证明停止的数据。
本轮不写这些SQL，不创建Migration10，不运行GRANT，也不声称上述签名已获批准。

## 7. 实施前未关闭决策与后续验收计划

需要独立确认：保留M9授权/冷却保护导致持续活跃scope无法清理时接受blocked；
回执与Result对账限于保留期；最近unknown退役时同时清空旧success指针；三项最小能力及前向迁移范围。
如不能接受这些限制，应先补充设计审查，不能直接换成更宽松的Trigger或全表清理。

获实施授权后才执行真实可销毁PG18 normal/race，不能用本轮文档校验替代：

- Run/Reservation/预算/指针同事务回滚、未知Commit和重复调用；全部计数与实际行逐项一致，无双减/负账。
- TTL前后、M/W/G满额、100行/2秒界限、keyset相同时间戳/空页/重入、无可删空间仍允许Pause/PATCH。
- Start/Result/Recovery/Supervisor与清理双提交顺序；独立连接屏障与pg_locks/pg_blocking_pids，不用sleep-only证明。
- 最新unknown和旧success、当前v/g/epoch隔离、清空最近指针时HTTP一致快照、旧Result与回执重放拒绝。
- 真僵尸/quarantine未证明停止绝不删；已可信停止只清记录账，物理槽与HTTP结果不变；6.3–6.5/5.4相关回归。
- Schedule登记注销/重新Owner授权、共享origin/IP跨租户、HMAC轮换一小时与key重建不提前补令牌。
- 三入口真实LOGIN最小权限、跨scope伪造、恶意游标和limit拒绝，API零目标DNS/TCP/HTTP。
- 新前向迁移与旧版Trigger配合、失败全回滚；源码哈希/冻结SHA/实际命令退出码/首次失败及CI Artifacts保留。

历史全局预算行约125ms等待根因仍未知。清理也会竞争G锁，不能用通过一次测试宣称该风险已修复；
任何新增反向锁或无法满足预算的证明均须停止并独立审查，不能延长超时。
8.3磁盘/WAL、后续任务、合并/Archive/部署/公网执行均不在本次范围。

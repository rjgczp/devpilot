# Task8.1 容量账实施与审查索引

范围：仅8.1。设计依据为产品负责人明确批准的
[`a0273be20881490b59cfce7ef5a398f5fa8cb2ac`](https://github.com/rjgczp/devpilot/commit/a0273be20881490b59cfce7ef5a398f5fa8cb2ac)。
授权消息为本会话“DevPilot AI — Task 8.1 实施授权”，明确批准 Migration9、内部容量
Trigger、Claim重排、status及最小ACL。不是引用此前阶段授权，也不自称独立代码验收通过。

## 实现与权限

- `migrations/00009_enforce_scheduler_capacity.sql`：前向新增；Migration1–8字节不变。
- `run_capacity_accounting()`：非LOGIN scheduler_owner 的内部 VOLATILE SECURITY DEFINER；
  固定 search_path、完全限定对象、无动态SQL、PUBLIC EXECUTE撤销，无运行角色新EXECUTE。
- Claim：原父序/既存Run检查→初始化并锁G/W/P/M→新Run INSERT/Trigger预留→仅物理槽
  增量→Reservation→Control→新SPI/数据库时间授权复验→同事务Commit。失败全回滚。
- UPDATE只处理pending predicate变化，terminal_count不重复增加；DELETE拒绝未终态、
  未证明物理释放、引用、授权/冷却未结束的数据，合法单条删除与减账同事务。禁止TRUNCATE。
- API owner 保留全部11个旧列UPDATE，新增列仅可SELECT、不可UPDATE；现有运行角色的
  表/列、秘密、角色属性及既有函数EXECUTE不扩大。新函数不能供运行角色直接调用。
- status 保持实时Cookie/成员关系/完整scope与单语句快照；容量阻断只返回固定
  retention_capacity，不返回预算计数、其他租户信息或目标摘要。

`terminal_count`继续表示“已保留Run及未来终态审计容量预留”：G<=100000、W<=10000、
M<=100，pending另<=32；物理active_count/in_use仍由可信物理协议独立维护。
该预留策略比同时用满100000终态+32待终态更保守，不扩大旧可用容量。
7天到期记录仍占账并阻断需新增容量的Claim，不因TTL/Lease/重启虚构空间。
真实批量清理、keyset、投影指针与磁盘/WAL分别留8.2/8.3，均未实施。

## Requirement / Scenario / Test

对应 Delta monitor-runs 的 `Retention has enforceable count time and capacity bounds`，
`Exception - Cleanup cannot keep up`；并保持物理证明、结果幂等及当前投影隔离要求。

| 验收点 | 实际测试/证据入口 |
| --- | --- |
| 首次预算缺行、并发初始化、Claim回滚 | TestBudgetRetentionFirstClaimAndRollback |
| M最后一记录槽、满容量Owner HTTP Pause/PATCH与固定status | TestBudgetRetentionMonitorCapacityAndSafeWrites |
| W10000、并发最后一槽 | TestBudgetRetentionWorkspaceCapacity |
| G100000、独立Worker/Workspace最后一槽 | TestBudgetRetentionGlobalLastSlot：真实99999条历史、两个Worker竞争最后一条；锁图与阻塞PID在日志 |
| 已提交Claim丢弃响应、重连对账、unknown/quarantine不提前减账 | TestBudgetRetentionPendingQuarantineAndLostACK；这里是主动丢弃响应，真正Commit ACK故障另由原7.2物理套件验证 |
| CHECK、最小ACL、内部函数不可直接调用、TRUNCATE拒绝、空库Down/Up | TestBudgetRetentionACLAndMigration |
| Schema8已领取Run升级9、pending回填一次、非空Down拒绝 | TestBudgetRetentionUpgradeInFlight |
| 旧7天历史不算空位、单条DELETE回滚/提交 | TestBudgetRetentionSevenDayHistoryIsNotFree |
| 升级gate未关/旧账不一致/DDL故障全回滚、重复Up、空库Down恢复权限 | TestSchedulerCapacityMigrationLifecycle：真实PG18三种故障；保留具体Goose诊断 |
| 可信物理返槽与pending/保留账独立 | 原7.5十二项normal/race物理矩阵；观察点新增retentionLedger一致性断言，原物理/HTTP/权限断言不删 |
| 结果首次提交/回滚/ACK未知/幂等/旧谱系及unknown | 保留原7.1–7.5、6.3–6.5、5.4套件；冻结CI结果与实际原始Artifacts为准 |

容量边界的数据填充是可销毁PG18中的行政静态、never-prepared历史夹具：实际INSERT
十万条记录并运行真实Trigger，不是Mock或仅把计数改到上限；它不证明Linux物理终止。
其中的历史时间/终态不冒充实际Probe、HTTP结果或Supervisor签名回执。真实物理证明
仅来自独立Linux接收端、网络停止/PIDFD/回收/签名与PG返槽的原联合套件。

## Schema隔离及不可更改的历史合同

普通API/身份/配置与新迁移测试使用最新Schema9。21个旧Scheduler测试文件通过显式
`schedulerHistoricalDB`固定Schema8：它们含行政投影Run、历史M8退役/Down及旧权限断言。
不能在Schema9中伪造物理证明、删账或撤保护以使这些历史夹具清理成功。
新容量、Budget及所有当前真实物理联合套件使用独立Schema9，不能把Schema8结果算作
Schema9容量证明；原7.1冻结组件及Snapshot6/7也保持原独立版本与测试断言。

原7.1 `scheduler_minimal_result_test.go`源码哈希检查保留；新容量观察断言放在7.5观察点。
原生产依赖测试只将明确批准的Migration9文件纳入白名单，仍要求精确9份迁移、API无
执行内核依赖、启动worker与路由哈希不变，并增加Migration8固定哈希保护。
历史“下一失败迁移”测试随实际新增9向前移到10；M8重复Up明确使用up-to8，原断言不变。

## 实际开发验收与证据

本地根：`/private/tmp/devpilot-task81/`。开发回执的head为a0273be且dirty=true，逐次
源码清单、二进制SHA256、命令、退出码和时间均在record.py回执中；不冒充未来干净提交。
冻结实现提交及实际Push/PR结果须按PR最新HEAD、CI_EXPECTED_HEAD和Artifacts中的head核对。

| 已执行项目 | 结果与原始证据 |
| --- | --- |
| 31项Budget normal | 31/31、退出0；budget-normal-final-candidate/normal-coverage.json及逐项日志 |
| 同31项Budget race | 31/31、退出0；budget-race-candidate/race-coverage.json及逐项日志 |
| 后补单条DELETE事务断言normal/race | 各退出0；delete-check/{normal,race}.json与日志；未重复全矩阵充数 |
| 完整PG18 integration normal/race | 各156项、退出0；integration-fixed/{normal,race}/result.json、实际每组命令与postgres.log |
| Go unit / race / vet | 均退出0；checks/unit-approved-migration.json、checks/unit-race.json、checks/vet-final.json |
| Schema9真实物理联合链路 | 12/12、全部退出0；physical/result.json及每项网络、签名、PIDFD、PG账与retentionLedger |
| API零目标审计 | 退出0；api-audit/real.audit.txt：80允许控制连接、0未批准目标；control校准捕获1次违规尝试，原白名单与CGO守护不变 |
| CI测试清单/历史协议合同 | 退出0；checks/ci-contract-fixed.json，原7.1源码哈希保护通过 |
| Migration1–8哈希 / diff check / Change strict | 已通过；migrations1-8.sha256及本次命令；最终冻结交付仍核对 |

冻结HEAD的完整GitHub CI仍需实际完成后才能登记通过；此处不预宣成功。
8.1未勾选，当前验收基线仍34/47，独立代码审核尚未进行。

## 首次失败与风险

1. 新夹具同一测试重复创建固定账号，users_email_key冲突：仅修正子测试名称；
   budget-normal-first/normal-8.log及PG日志保留。
2. 新增UpgradeInFlight清单未同步：inventory严格拒绝执行，无跳过；
   budget-normal-candidate/保留原清单、命令和错误。
3. 新TRUNCATE测试先被既有外键以0A000拒绝：保留该断言，再用CASCADE负向请求证明
   新Trigger仍以23514拒绝；budget-normal-ready/normal-13.log及PG日志保留。
4. 向冻结7.1测试加观察断言触发原源码哈希合同失败：恢复原文件，不改期待哈希；
   新断言移到7.5，合同复验退出0。首次输出在本会话工具记录，未单独捕获内层退出码；
   其外层命令串状态被末尾diff check遮蔽，后续统一用record.py独立记录。
5. 原生产合同仅允许8个迁移，发现新9后unit失败：仅纳入已授权9及M8哈希保护，
   API依赖/启动断言不改；checks/unit.log及复验日志保留。
6. 历史M8重复up误升新9，随后Down未产生M8的g退役：只将该历史用例目标固定8，
   不改迁移或断言；integration/normal/core-05.log与修复后integration-fixed/均保留。

历史IPv620s根因未知及7.3精确canceled缺少可信拒绝来源仍保留，未声称修复。
本批不证明磁盘硬预算、连续长期自动清理、生产出口或生产部署能力。
PR12保持Open；不合并、Archive、部署或进入8.2。

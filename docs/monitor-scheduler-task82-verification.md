# Task8.2 — 实施候选与验收索引

状态：实现候选，冻结提交与完整CI尚待完成；Task8.2未勾选，35/47。
设计基线：`d8667ff7f42876f858cc1dc3b2d25fb95db962d3`。该基线的Push41418393805、
PR41418399395均success，不把它们作为本轮实现的CI结果。

## 授权与实现边界

产品负责人在本会话请求 `call_32c1ce81fff84e4f825180556dfaa746` 中明确回复：
“批准仅上述8.2范围、三项窄EXECUTE及信任边界”。该消息授权本轮三项精确能力，
不是引用早期Checkpoint或代理自审。详见[已批准设计](monitor-scheduler-task82-cleanup-plan.md)。

- 新增 `migrations/00010_add_bounded_scheduler_cleanup.sql`，只创建三项Controller-only
  VOLATILE SECURITY DEFINER函数：retention_candidates、cleanup_monitor、cleanup_target_key。
  非LOGIN owner、固定search_path、PUBLIC撤销；不增加角色、成员关系、表列或秘密权限。
- Migration1–9与d8667ff的Git blob逐字节核对一致。没有修改M9授权、冷却或引用保护Trigger。
- `internal/scheduler/retention_linux.go`仅schedulerfixture/Linux构建可用；一次函数调用一个
  READ COMMITTED事务，2秒Context，Commit未知不重做网络或宣称回滚。普通发布/API无新入口。
- 每事务一个scope、Run批次最多8；父序→既存Run→G/W/P/M→origin/IP→Reservation→control，
  全部等待后新SPI复验身份与数据库时间。末段登记防止同一事务通过更换incarnation多次写入。
- GET DIAGNOSTICS逐项计入DML及原Trigger副作用，最多50条同LOGIN Controller登记，
  最坏99次行变更；超限预先拒绝。真实PG测试另观测整个事务前后差值，不只数Run。
- 清理只减retained；pending、物理槽、速率、冷却均不手工减账。最近attempt退役时同时清空
  latest_result指针，旧success不自动成为current_observation；仍有Run不删Schedule/重置Fence。

## Requirement / Scenario / Test / Evidence

依据：D6、D9与monitor-runs的Retention requirement，尤其
`Concurrency - Bounded cleanup preserves references`、`Exception - Cleanup cannot keep up`。
下列是开发期真实PG18候选回执；dirty=true原样保留，不能冒充未来冻结SHA。
`/private/tmp/devpilot-task82/`为本地原始目录，交付时另存稳定证据副本并保留哈希。

| 场景 | 具体测试与证据 | 当前开发期结果 |
| --- | --- | --- |
| 父序keyset、Run/Reservation/登记/key原子退役 | TestBudgetCleanupKeysetLedgerAndRetirement | normal/race通过；17条分8/8/1，写入41/41/14 |
| 实际Rollback、Commit完成后应用ACK丢失、重连 | TestBudgetCleanupRollbackACKAndLastSlot | normal/race通过；不冒称TCP层ACK抓包故障 |
| 完整事务写入计数、第二次调用拒绝 | TestBudgetCleanupFullTransactionWriteBound、WholeTransactionTriggerWrites | normal/race通过；99次实际行变更含Trigger；51登记拒绝且全回滚 |
| 双连接清理竞争与真实锁图 | TestBudgetCleanupConcurrentReservationAndRollback、ParentWaitAndFreshIdentity | normal/race通过；pg_locks/阻塞PID；等待后失权42501且无删除 |
| 计数满额后一个清理槽供两个Worker竞争 | TestBudgetCleanupGlobalLastSlot、WorkspaceLastSlot、MonitorLastSlot | normal/race通过；真实100000/10000/100行；Workspace SKIP与最终retention_capacity分别验证 |
| 两指针原子维护，不回退未删除旧指针目标 | TestBudgetCleanupAtomicProjectionClearing | normal/race通过；另一连接见完整旧态，Commit后同时NULL，Fence保留 |
| Scope、身份、游标、limit及最小权限 | TestBudgetCleanupScopeAndIdentityReject、TargetCoolingReferencesAndACL | normal/race通过；Controller无表DELETE，Worker/Supervisor/API无新EXECUTE |
| 保守授权/冷却保护 | TestBudgetCleanupConservativeProtection | normal/race通过；保持protected及全部预算 |
| Migration10/Down/重复Up/中途DDL失败 | TestBudgetCleanupMigrationLifecycle | normal/race通过；失败无部分函数/GRANT，Schema9断言未被替换为Schema10结果 |
| 真实HTTPS、独立停止/可信返槽、完整一小时冷却后清理unknown | 原TestRecoveryPausePG + TestCleanupPhysicalPreparePG/FinishPG | 开发候选normal/race通过；6个Go进程，实际等待约3600秒，原始签名与物理证据独立保存 |
| Supervisor失联、unknown保留quarantine | 原TestRecoveryUnconfirmedPG + TestCleanupPhysicalRejectPG | 开发候选normal/race通过；4个Go进程，无回执、不删、不返槽 |
| 冷却后仍被released引用的key不可删；引用退役后才删除；旧permit/Result/Receipt重放 | 最新PhysicalFinishPG及run-cleanup82.py | 新增联合断言，等待冻结CI执行，不用前一候选证明它已通过 |
| 原31项Budget与7.1–7.5、6.3–6.5/5.4、API零目标审计 | 原流水线完整保留 | 开发期42项Budget normal/race曾通过；最终45项完整清单与所有历史回归以新冻结CI为准 |

14项清理测试的当前候选原始回执：`current-normal/normal-coverage.json`与
`current-race/race-coverage.json`，均complete=true、14/14、exit0。
`physical-candidate/result.json`为较早dirty候选的真实冷却证明；之后补充了早拒绝、
never_prepared完整字段核对及联合key/replay断言，故最终必须以冻结CI的新一次完整运行为准。
`unconfirmed-current/`保存真实失联拒绝回执。没有用过去通过记录替代本轮新SHA。

## 首次异常与修正

1. 初次本地编译发现测试变量e作用域错误，随后补充用例误写Store.Get（应为Detail）；
   属测试编译错误，原日志在build/initial.log、build/capacity-current.log保留。
2. 本地build-budget.sh在完成编译后因cp源/目标同为/tools/goose退出1；
   原build/budget-final-candidate.log保留。未改原脚本或断言；后续构建用分离工具目录，CI仍用原路径协议。
3. 首轮统计绝对值出现41→82→96，发现不能将pg_stat_xact_user_tables的绝对值当作单事务计数。
   对照[PostgreSQL原始问题报告](https://www.postgresql.org/message-id/CABPTF7Wb6VOac1EFopGzhAtiHOU8_XRkFvhC6BnSj%3DjACBB%2BGw%40mail.gmail.com)，
   改为固定DML/已冻结Trigger副作用精确累计；独立99行差值测试及连续事务41/41/14回归均通过。
   targeted-normal-first/原件保留，最初7项通过不当作完整计数验收。

历史5.4、7.3首次失败、IPv6/管理确认超时及约125ms全局预算锁等待证据仍在
[8.1最终索引](monitor-scheduler-task81-final-review.md)，未改写或删除；125ms根因仍未确定。

## 运行与人工恢复边界

- 只使用已登记的隔离Controller身份。每调用单独事务；先无锁候选页，再对一个scope或一个key调用。
  不能持G锁寻找其他父资源，不能把多个状态操作拼进同一事务；scope页/Run页分别按返回游标轮转。
- protected、无合格记录或retention_capacity时保留数据和资源账；下一有界轮次重读，不在原事务忙循环。
  持续授权或共享目标仍处冷却时可能长期blocked，这是已批准的保守边界，不用force绕过。
- 未取得独立物理证明时只能停止新Start、维持quarantine并恢复可信Supervisor控制面；
  不得用Lease、Worker报告、容器删除、管理员UPDATE容量账或直接删Reservation恢复容量。
- 只有scope已无Run、无有效授权、全部冷却/引用保护通过才退役Schedule及父预算。
  有Run时Fence不重置；后续重新登记仍须Owner新g授权，旧Run/Receipt不能复活。
- 记录及回执超过保留边界后不再提供旧对账事实；未找到不等于HTTP失败或已回滚，绝不补发GET。
- DELETE提交不等于磁盘已回收，WAL/vacuum/1GiB实测留8.3；本轮未实现8.3。

## 冻结交付门禁

待完成：当前源码冻结提交、该SHA完整Push/PR CI、实际Artifact与源码清单核对。
这些通过前8.2保持未勾选。独立代码审查尚未签署；PR12 Open，不合并、Archive、部署或开放生产执行。

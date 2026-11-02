# Task8.1 最终审查包（独立技术验收已批准）

冻结受测实现：[`0d2f5677dd6a2439ab010b6a8ec807941b1403cd`](https://github.com/rjgczp/devpilot/commit/0d2f5677dd6a2439ab010b6a8ec807941b1403cd)。
设计依据：已批准的[`a0273be20881490b59cfce7ef5a398f5fa8cb2ac`](https://github.com/rjgczp/devpilot/commit/a0273be20881490b59cfce7ef5a398f5fa8cb2ac)。
[PR #12](https://github.com/rjgczp/devpilot/pull/12)保持Open。

**2026-11-02独立技术验收已批准，Task8.1完成，35/47。**
批准出处：本会话用户原始消息“DevPilot AI — Task 8.1 正式收尾与 8.2 设计准备”，
明确“接受冻结 SHA 0d2f5677dd6a2439ab010b6a8ec807941b1403cd 的 Task 8.1 技术验收结果”，
并确认历史5.4、7.3兼容性修改已完成独立代码审查。此处记录用户的批准，不以代理自审代替独立签署。
本次只补文档，不改生产实现、权限、Migration1–9或测试断言，不重跑既有测试矩阵或手工重跑冻结工作流。
文档提交与冻结受测实现分开标识；本次批准只允许准备8.2设计，不允许清理实现或新增权限。

## 1. 最终CI及源码绑定

|事件|工作流|verify|integration|execution-security|最终状态|
|---|---|---|---|---|---|
|Push|[41402215734](https://github.com/rjgczp/devpilot/actions/runs/41402215734)|success|success|success|completed/success，attempt1|
|PR|[41402221192](https://github.com/rjgczp/devpilot/actions/runs/41402221192)|success|success|success|completed/success，attempt1|

冻结实现的workflow三个作业×两个事件全部成功，验收时6个PR可见check均pass，没有剩余CI失败项。
文档推送若触发新CI，其结果另以文档提交SHA记录，不沿用本表冒称新SHA已通过。
Schema9 Budget31项normal/race各完整执行；7.3为44个编排条目、7.4为6个、7.5为12个，
两次CI各自complete=true、全部exit0，不用其他SHA或dirty开发结果填补本SHA。

验收时本地、远端分支、PR受测实现均核对为0d2f567；本轮文档提交不冒充该受测SHA。源码清单SHA256：
`3b618c9b4f16e7fefa48f2e2bb87f338cc10b558758c0c5eed034d14e8ff69be`。
本次从冻结Git树读取blob字节，对1439个源码条目逐项计算SHA256，与CI回执完整清单相同。
两次CI合计488份当前SHA命令回执均clean、exit0；另外48份legacy7.1回执属于独立冻结
`c1c9fe4b24efb2a4bc18de0ef4d75c9fe0f1014c`，只作历史Schema8组件回归，不当作新实现成果。
这些数量是命令回执数量，不是独立安全场景数量。

本次仅运行文档校验：Change strict、git diff --check及实现路径不变检查均exit0；
全库strict为exit1，仍是4份历史规格的7项Requirement描述过长WARNING，未修改无关规格。
原始命令/退出码见本地交付目录`task81-final-review-20261102/validation-exits.txt`及对应日志。
本轮没有手动重跑PG18/Linux矩阵或冻结CI；后续文档提交不是新的实现验收SHA。

### 最终Artifact

全部6份实际ZIP已下载核对GitHub digest并验证CRC，原件未反复提交Git。
以下路径引用均指ZIP内部路径；normal/race是实际运行结果，不是Mock。

|事件|Artifact|ID|ZIP SHA256|
|---|---|---|---|
|Push|[execution-security](https://github.com/rjgczp/devpilot/actions/runs/41402215734/artifacts/13124861885)|13124861885|`3330c9c97b1565eaa9fc38509525010f8e8f20ccaac2b3a4118c45981852bb32`|
|Push|[integration](https://github.com/rjgczp/devpilot/actions/runs/41402215734/artifacts/13122382833)|13122382833|`23579c6029a147a1bf595b5589fca4714d9021afc43177c4eb840eefc98b5bbb`|
|Push|[verify](https://github.com/rjgczp/devpilot/actions/runs/41402215734/artifacts/13121473312)|13121473312|`29e5b78aa51dc4cb1bf6a1b32d91fed5e587b3e159baaf897e41f66be40155f3`|
|PR|[execution-security](https://github.com/rjgczp/devpilot/actions/runs/41402221192/artifacts/13124039991)|13124039991|`6bf22538158e655ff9997160087e2fc3ada6b53281577fef05208bf7278d102d`|
|PR|[integration](https://github.com/rjgczp/devpilot/actions/runs/41402221192/artifacts/13121513926)|13121513926|`f8bd4e8622a5def11ad87867365595ae41fa73e9bd1853615e3e284eb5739dcc`|
|PR|[verify](https://github.com/rjgczp/devpilot/actions/runs/41402221192/artifacts/13121897148)|13121897148|`80dfae8f281aeac06327ff0c61f61cdd4fd7fd65c6559cfa2a9a1c9bd1ce4fb9`|

## 2. 已实现范围与口径

实现入口是Migration9的内部`run_capacity_accounting()`、先初始化/锁G→W→P→M再INSERT Run的Claim，
以及固定retention_capacity状态投影。非LOGIN owner、固定search_path/PUBLIC撤销不变；
没有新运行角色EXECUTE。API owner保留旧列能力，不获得新增pending列UPDATE。

- retained账仍使用原terminal_count列，预留每条Run将来所需审计容量；G≤100000、W≤10000、M≤100。
  它包含尚未终态的Run，因此比“100000终态再加32活跃”更保守，**不宣称可同时存100032条Run**。
- pending独立≤32，谓词为leased/started或尚无物理释放证明；不等同物理in_use/active_count。
- 7天旧记录仍占账并可阻断新Claim，不因TTL、Lease或重启虚构空间；**没有实现7天自动删除**。
- schedule及budget key各≤10000；资源不足固定retention_capacity，不通过跨Workspace/重试绕过。
- Pause、PATCH及安全停止保留原路径；物理返槽不等于HTTP结果提交，不返还已消耗速率或缩短冷却。

## 3. Requirement / Scenario / Test / Evidence

规格依据为本Change已批准Delta，不修改主规格或提前同步/Archive。
缩写R1：monitor-runs / **Retention has enforceable count time and capacity bounds**；
S1：**Exception - Cleanup cannot keep up**。
下面B编号对应上述两个security Artifact的`budget-evidence/{normal,race}/`：
B8即`normal-8.log`和`race-8.log`，同名前缀`.json`保存命令、exit、源码/二进制hash，
`-postgres.log`/`-ledger.log`保存数据库原件。两次CI都可按相同路径复验。

|Requirement / Scenario|验收事实与实际Test|Evidence（两个CI各normal/race）|
|---|---|---|
|R1 / S1；persistence **Parent first locks remain compatible**|首次预算缺行、并发初始化、Claim失败全回滚：TestBudgetRetentionFirstClaimAndRollback|B8；真实独立连接与事务屏障|
|R1 / S1|M最后一槽、100条上限、满额时真实HTTP Pause/PATCH继续成功与固定status：TestBudgetRetentionMonitorCapacityAndSafeWrites|B9；HTTP200、P0001/retention_capacity及账本|
|R1 / S1|W10000条真实历史与最后槽竞争：TestBudgetRetentionWorkspaceCapacity|B10；不是仅将计数改成上限|
|R1 / S1；worker-security **Many workers reserve the last capacity**|G99999条真实历史、两个Worker/Workspace争最后槽，最终100000：TestBudgetRetentionGlobalLastSlot|B11；锁图/SQLSTATE/最终数据库状态|
|R1 / S1；monitor-runs **Start acknowledgement is lost**|Claim响应丢弃后重连对账，不增加Run；unknown无物理证明保留pending/物理槽：TestBudgetRetentionPendingQuarantineAndLostACK|B12；此项是提交后丢应用响应，不冒称独立捕获TCP ACK丢包|
|worker-security **Least privileged roles cannot bypass state transitions**|pending CHECK、内部函数不可直接EXECUTE、API owner新列不可UPDATE、TRUNCATE保护：TestBudgetRetentionACLAndMigration|B13；真实LOGIN/ACL/SQLSTATE；空库9→8→9|
|R1 / S1；persistence **Shape and scope violations fail in PostgreSQL**|已有Schema8在途Run升级9，pending回填一次，非空Down拒绝：TestBudgetRetentionUpgradeInFlight|B14；不伪造物理释放来清理|
|R1 / S1|8天历史仍占账；合法单条DELETE回滚/提交：TestBudgetRetentionSevenDayHistoryIsNotFree|B15；行政never-prepared历史夹具，只证明Trigger账，不当作Linux终止证据|
|R1 / budget key上限|真实10000个key边界、Claim失败不留Run：TestBudgetArithmeticAndKeyCapacity|B3；沿用原冷却/key保护|
|R1 / schedule上限（继承边界）|TestCheckpointOneRegistrationCapacityLastSlot：9999真实Schedule+两授权争最后一槽|integration的normal/race `core-03.log`；**此测试固定Schema8**。M9未重定义schedule_capacity/authorize；Schema9另由B13和API回归覆盖新列ACL。不是Schema9下再做一万行调度压测|
|persistence **Full migration rollback and exhaustion**的9升级细化|TestSchedulerCapacityMigrationLifecycle：open-gate、ledger-mismatch、DDL故障原子回滚，重复Up/空库Down恢复旧ACL|integration normal/race `core-05.log`；原失败Goose输出保留|
|worker-security **Physical resource release requires independent termination proof** / **Expired lease leaves a live zombie**|TestBudgetPhysicalReleasePG：真实45s Lease/僵尸、签名/物理终止、返槽后pending仍满额拒Claim，终态后才新领取|`release54-evidence/{normal,race}/pg-driver.log`及result.json；不能用DELETE容器代替回执|
|monitor-runs **Unknown delivery is terminal for retry and distinct from failure** / **Process dies after sending GET**|NoPermit/RecordedDenial/Unconfirmed/CommitACK等；首次与重复Recovery、物理前后、回滚/确认丢失精确pending变化|`recovery73-evidence/result.json` 44/44；原NoPermit normal/race驱动日志与receipts；所有其他预算列仍比较|
|monitor-runs **Result commits are fenced and can be reconciled without networking** / **Successful commit loses acknowledgement**|TestResultCommitACKPG、Rollback/Validation/Pause/Patch/Renew等，结果幂等、旧谱系只留审计且不退款|`result72-evidence/result.json` 18/18；真实物理证明仍独立|
|monitor-runs **Execution status is a separate live authorized projection** / **Current identity and privacy remain mandatory**|7.4权威API与7.5真实PG/HTTP矩阵，容量状态脱敏、unknown与物理in_flight分离|`status74-evidence/result.json` 6/6、`status75-evidence/result.json` 12/12；原API零目标审计在integration Artifact|
|R1 / **Bounded cleanup preserves references**|仅DELETE基础拒绝/单条减账保护，不是清理器并发/keyset或指针回收实现|**留8.2，未验收**|
|R1 / **Disk growth is measured separately from row limits**|本批不把行数cap当1GiB磁盘/WAL安全保证|**留8.3，未验收**|

原31项Budget清单、逐次命令及Goose版本分别在coverage/receipt/build原件中；
不把旧历史组件的成功或行政数据填充记作当前真实网络/物理终止成果。

## 4. 首次失败、修复与不可删除的负面证据

|事件|原件/来源|修复与限制|
|---|---|---|
|322c948下历史5.4失败：physical31、pending32，两新Claim均retention_capacity，旧断言要求一成功|[Push 41028965601](https://github.com/rjgczp/devpilot/actions/runs/41028965601)，security Artifact**12966100765**，SHA256 `f2b003a0a0d641710e6beb5b60cd8ce4c50ee37415ccf6a3304866a0112fa9a8`；原`release54-evidence/normal/pg-driver.log`|[`fee4e0d`](https://github.com/rjgczp/devpilot/commit/fee4e0df9937f23614c8423ca6f29db89a991749)仅适配fixture。保留物理合同，分阶段证明pending拒绝与合法终态后领取；未直接减账|
|fee4e0d下历史7.3 NoPermit失败：全预算JSON比较报告refunded tokens/slots|[Push 41378086638](https://github.com/rjgczp/devpilot/actions/runs/41378086638)/[PR 41378091273](https://github.com/rjgczp/devpilot/actions/runs/41378091273)；Artifact13113308736/13112869131，SHA256分别`af20d1f74ee964eef8869b5daf5809a50f7ef98c5ce3b50a790b7c2f757dc4e8`/`be35b78e516d5cbfd573219293cf51d70dc272b82a9ef65f916863c5b3b4368d`|原Artifact缺before完整值，没有补造。保持旧断言的日志增强复现exit1，实际唯一差异global pending1→0；0d2f567仅修测试，条件化精确delta与完整其他字段比较|
|7.3修复前后绑定|本地`recovery73-repair-20261102/`中的diagnostic-budget-before/after、diagnostic-column-diff、diagnostic-only.diff、targeted/receipts|先诊断负面复现，再5用例normal/race共10/10。旧回执fee+dirty=true原样保留；新SHA完整CI另作上述证据|

原始5.4/7.3 ZIP及日志仍保存在本地既有审查目录，未覆盖、删除或改写；先前failed状态不因新绿色结果抹除。

## 5. B项及未关闭风险

1. **历史约125ms全局锁持有/等待根因未确定，未宣称修复。** 原[PR 41028971817](https://github.com/rjgczp/devpilot/actions/runs/41028971817)的PID124/transaction900阻塞证据、124.736ms等待、173.391ms Start跨度及原始Artifact12965530648（SHA256 `7d2989dd44744eada4728354241ed5d0f5ca0cbf78a5863a2ae68b72a56bd0d0`）保留。
   后续本地arm64一次有效观测/当前CI通过不能反推原x86_64 Runner无问题；无效observer exit2也保留。未区分原延迟中的SQL/Trigger、WAL和Runner调度责任。
2. Schedule一万行的真实最后槽证明继承Schema8不变路径；最终审查应明确接受该证据继承，或另行授权Schema9定向边界验证。本轮不擅自改断言/补跑。
3. 保守retained预留不是100032条同时可用容量；7天清理、长期不停顿回收不在本批完成。达到cap无可安全回收空间时blocked是预期安全行为。
4. 当前DELETE保护会因任何Reservation、最近指针、有效授权、近期Start/冷却等拒绝删除。8.2不能简单加DELETE循环，详见下一节设计输入。
5. 1GiB运行数据/10GiB测试盘、WAL/vacuum/日志实际增长尚待8.3，行数不等于磁盘保障。
6. 历史部署角色真实GRANT清单缺失、历史IPv6/管理确认超时、精确canceled缺乏可区分可信拒绝来源等既有风险保留，不凭本轮通过作新归因或扩大生产权限。

## 6. 最终签署与后续8.2的设计输入

原先34/47待签署状态由上述2026-11-02用户明确批准关闭；8.1现更新为35/47，8.2仍未勾选。
本次仅**准备**[8.2安全清理设计](monitor-scheduler-task82-cleanup-plan.md)，不把签署当实现或新增权限许可。

下面仅列已有约束/待设计问题，不是已批准的8.2方案或清理实现：

- 原D6父序与单Monitor事务；keyset、有界候选、每事务最多100行且2s，不在持G锁时回头取父资源。
- 不删leased/started、quarantine或未证明物理终止记录；合法unknown也须独立物理事实和所有引用/冷却满足要求。
- 现M9只要存在Reservation就拒Run DELETE；released Reservation的生命周期/回执防重放证据需先设计，不能靠管理员DELETE绕过。
- Schedule最近attempt/result指针需同事务清空或替换；Schedule注销不得重置仍关联Run的fence，也不能缩短预算key冷却。
- M9对有效授权、近期Start和monitor预算keep_until的保护可能让持续授权scope无法回收旧Run；必须审查安全、可用性与既有合同，不能私改M1–9或声称已能持续清理。
- 现运行角色没有Run清理入口/直接DELETE能力；如需前向Migration、SECURITY DEFINER或EXECUTE，8.2设计须逐项列签名、角色和最小差异，另行批准，不能沿用8.1授权。
- 设计验收应覆盖并发Result/Start/Recovery、最后容量、回滚/ACK未知、指针和保留边界、旧fence/receipt重放；磁盘增长另留8.3。

本轮不创建8.2实现、Migration10、数据库函数或GRANT，不改当前批准的Spec/Design语义，不合并PR12、Archive或部署。生产网络执行硬关闭。

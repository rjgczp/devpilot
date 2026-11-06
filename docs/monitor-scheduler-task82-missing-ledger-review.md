# Task8.2 — G/W/P/M缺账定向核查

审查基线：`45a2a95dd99eb1e3118476d8b81da6699140e27d`。仅处理人类本轮要求的
`cleanup_monitor()`缺账失败关闭问题；Task8.2仍未独立验收，35/47。
原PR12及该SHA的完整Push/PR CI、六份Artifact和历史失败均保留，见前一交付索引。
本轮不重跑完整矩阵，不实施8.3、不合并或部署，生产执行硬关闭。

## 真实PG18发现

原SQL的`PERFORM ... FOR UPDATE`在找不到行时只设置FOUND=false，不自动抛错。
G有显式存在性检查；W/P/M没有。M9的Run DELETE Trigger仅对G/W/M减账并检查
FOUND，因此不能补足P；没有选中Run时，这个Trigger根本不运行。

使用三个独立、无网络上游、可销毁PG18集群，每个测试检查G/W/P/M四个组合：

| 场景 | 原G | 原W | 原P | 原M | 修复后normal/race |
| --- | --- | --- | --- | --- | --- |
| 存在合格历史Run，实际进入删除路径 | 23514 | 23514 | **00000，返回deleted_runs=2** | 23514 | 全部23514 |
| Run仍存在，但keyset不选中任何候选 | 23514 | **00000** | **00000** | **00000** | 全部23514 |
| 零Run，仅有冷登记及预算 | 23514 | **00000，返回rows_written=7** | **00000，返回rows_written=7** | **00000，返回rows_written=7** | 全部23514 |

修复前3个定向测试各退出1，合计7/12未失败关闭；不是超时或工具错误。
意外成功调用有意ROLLBACK，未把已回滚诊断声称为实际提交删除。
修复后3个normal和3个race测试均退出0，每模式12个组合均拒绝。
这些首轮修复回执仍记录45a2a95+dirty=true；后续冻结验证使用单独证据目录，不能改写旧回执。

## 已有保护能覆盖什么

- 原`budget_capacity()`在有Run引用时阻止普通DELETE G/W/P/M（23514），global本身始终受保护。
- 无Run且冷却到期时，W/P/M普通DELETE可成功；Schedule不构成该Trigger的引用拒绝条件。
- 没有从这些资源到预算text key的跨表FK；行内CHECK不能证明另一行存在。
- 本轮未证明正常Controller接口能制造缺账；运行角色仍没有表DELETE权限。
  但数据库现有约束不足以保证所有受控缺账输入都被清理入口拒绝。

有Run或global的缺账由既有可销毁fixture管理员在**准备事务**中SET LOCAL
session_replication_role=replica删除单个账行；随后提交并结束此设置。
测试不改计数值、不伪造Supervisor、Run终态或HTTP结果。零Run的W/P/M直接使用原正常DELETE。
真正调用通过独立Controller登录，断言session_user/current_user正确、replication_role=origin，
三张相关表的所有用户Trigger均为O。故障注入不跨越被测调用，不新增GRANT或信任来源。
每轮只把故障行原样恢复到可销毁fixture，确认完整基线恢复后才验证另一个组合。

## 最小修复与不变量

Migration10仅增加三行：W、P、M各自FOR UPDATE后立即`IF NOT FOUND`抛出
现有`capacity ledger inconsistent`/SQLSTATE23514。G的既有检查不变。
不初始化预算、不自动修账、不新增锁或DML、不改父序、清理资格、物理证明、权限、2秒/250ms预算。
Migration1–9、生产Go及全部物理/网络实现均不改。

负向测试文件：`tests/integration/scheduler_cleanup_missing_ledger_test.go`：

- TestBudgetCleanupMissingLedgerRuns
- TestBudgetCleanupMissingLedgerNoCandidate
- TestBudgetCleanupMissingLedgerRegistration

每个组合都比较失败前后的完整Run、Reservation、Schedule（含两个指针）、全部预算及
control gate/transaction标记的同语句JSON快照；失败后逐值不变。历史never-prepared到期记录
来自原cleanupHistory helper，不制造HTTP结果；latest_result指针保持NULL，非空attempt指针也受到比较。
三个测试加入原Budget inventory，未来正常CI仍会完整包含它们；本轮仅做指定三项normal/race。

## 命令与证据

原生Linux构建沿用`tests/scheduler/build-budget.sh`：Go1.24.3、Goose3.26.0、
integration,schedulerfixture,budgetfixture；normal/race独立二进制，tagged vet同时执行。
定向runner复用原run-budget.py的保护配置，仅把选择限定为上述精确测试名，
PG18镜像digest、network=none、512MiB/CPU1/PID128、驱动cap-drop ALL及no-new-privileges均不变。
每个Go进程120秒；Controller事务2秒、锁250ms，未延长或增加skip。

原始命令、二进制/源码SHA256、退出码、全量故障前快照、实际SQLSTATE、数据库日志及环境inspect：
`/Users/sansea/.codex/artifacts/devpilot/task82/missing-ledger-review/`。
`before-*`为三个预期失败原件；`after-normal-*`/`after-race-*`为首轮修复验证；
`frozen-*`用于新冻结提交的独立定向验证，尚未执行的项目不作为通过。

本修复先交付独立审查分支，PR12保持原冻结SHA和CI；未触发或宣称新提交完整CI通过。
最终审查后才决定是否更新PR12，不通过绕过测试或反复CI重跑获得全绿。
原约125ms全局锁等待、IPv6等历史未定根因不因本轮被宣称修复。

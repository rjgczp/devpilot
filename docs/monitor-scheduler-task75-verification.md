# Task 7.5 — Activation / execution-status 一致性验收

基线：main/origin/main `5905f518632025c32b2a02c6f21ab675bf45bb2c`，PR11阶段性合并；
main CI40669015064实际成功。开发分支：`codex/monitor-execution-status-consistency`。
本轮仅7.5，已在实现提交的Push/PR CI全部成功后勾选，当前34/47；独立代码审查待签署。

## 变更边界

仅测试、测试驱动可选观察屏障和CI编排。生产Go、Migration1–8、SQL签名、角色、
EXECUTE及表列ACL均无变更。未发现需要修改生产实现的缺陷。
复用原两个HTTP路由、`checkpointClientObservation`消费者合同、原DNS/IP/TLS/GET
内核、Supervisor及签名回执/PG返槽函数。没有新客户端或生产执行入口。

`recoveryBeforeSettlement`是测试驱动的可选nil回调，位于独立停止证明验证后、
现有Settlement事务之前；不生成证明、不修改旧断言、不延长事务/网络permit。
用于真实60s到期时验证尚未在PG确认物理释放的Run仍占槽。原7.4六项已复验nil路径。

## D11.3 / Requirement / Scenario → Test

| 场景 | 实际测试与证明 |
| --- | --- |
| Cross endpoint disabled waiting and blocked states | `TestExecutionStatusCrossEndpointMatrix`：disabled、普通enabled缺授权、缺部署许可、authorized_waiting、due、PATCH撤销；真实Owner授权/意图/PATCH HTTP，Owner/Member无并发窗口读取固定迁移对及权威状态 |
| Independent snapshots are not merged into a false state | `TestExecutionStatusCrossEndpointSnapshots`：PATCH、Pause、授权×两读顺序；真实Workspace锁阻塞实际HTTP写者，独立连接采集pg_locks/pg_blocking_pids，Commit前后完整v/g，消费者拒绝拼接并刷新 |
| Runtime changes keep the intent validator stable / authorization expiry | `TestExecutionStatusCrossEndpointExpiry`：真实60s数据库期限，查询不改Activation body/tag/g；到期后同version重新授权及真实锁屏障，g变更必须刷新 |
| Current identity and privacy remain mandatory | `TestExecutionStatusCrossEndpointAuthority`：两个接口的Owner/Member/401/404、同时属于两个Workspace的Member混合真实ID、移除后404，固定脱敏错误；复用严格14字段Activation及最小status白名单 |
| Running survives pause and configuration invalidation visibly | `TestExecutionStatusConsistencyPausePG`、`PatchPG`：真实Start/HTTPS后Owner通过HTTP暂停/PATCH；旧started及原v/g/epoch仍可见，in_flight=true；独立停止后旧Result只留审计，不覆盖新观察 |
| Minimal status / matching completed | `TestExecutionStatusConsistencyCompletedPG`：真实Claim仅leased，Start后started，可信返槽不伪造completed；独立Result Commit后才有匹配current_observation |
| Unknown is not masked by an earlier success | `TestExecutionStatusConsistencyUnknownPG`：第一Run真实HTTPS/可信终止/Result成功；测试驱动以全新Run ID、完整60s未来间隔准备下一输入，第二Run实际GET后保守unknown；旧成功仅留最近completed审计，当前观察null，回收前后in_flight true→false |
| Unknown and authorization expiry remain separate observations | `TestExecutionStatusConsistencyExpiryPG`：真实60s期限，独立停止证明已获取但尚未提交PG；过期、in_flight=true、槽仍1；Controller保守unknown，可信回执确认后槽0/in_flight=false，HTTP结果仍null |
| Physical resource release requires independent termination proof | `TestExecutionStatusConsistencyUnconfirmedPG`：真实Supervisor失联，无终止回执，两个接口保留unknown/in_flight=true，quarantine及global槽1；不以fixture销毁宣称返槽 |
| Configuration management never performs target networking | `TestMonitorNoTargetNetwork`追加静态矩阵、双读屏障、鉴权；原精确目的白名单、CGO来源检查和违规连接反例均不变 |

未知Run测试的第二次输入只在测试驱动准备独立未来scope/Run ID，不是实现新的Scheduler
周期功能；不创建Run/Lease/Permit/结果/终止证明，不缩短60s间隔。第8–10组未实施。

## 实际本地结果

所有PG均为现有testdb守护通过的可销毁PG18，Linux原生Go1.24.3及Goose3.26.0。
开发回执保留实际dirty基线及源码/二进制哈希，不冒充未来干净提交的CI结果。

| 命令/清单 | 退出码及证据（本地根 `/private/tmp/devpilot-task75/`） |
| --- | --- |
| `go test ./internal/monitor` / `go test -race ./internal/monitor` | 0；unit-final.log / unit-race-final.log |
| `go vet ./...`；Linux原生完整fixture标签vet | 0；vet-final.log、checks/linux-vet.json |
| 原生integration.test / -race.test，7项选定测试、120s预算 | 各0，63.058s / 64.193s；pg-ready/{normal,race}.json及原始日志；4项新增+3项受影响历史HTTP回归 |
| `python3 tests/scheduler/run-status75.py --tools <tools> --evidence <physical>` | 12/12、全部0；physical/result.json及逐项receipts、接收端、规则、PIDFD、签名、PG账本 |
| `python3 tests/scheduler/run-status74.py ...` | 6/6、全部0；status74-regression/result.json及原始证据 |
| 原 `tests/network/run-monitor-audit.sh` | 0；api-audit/real.audit.txt：80允许控制连接、0未批准目标连接；control child0/audit1，real child0/audit0 |
| CI integration清单合同 / gofmt / git diff --check / Change strict | 0；ci-plan-final.log、openspec-change.log；原151项不删，新增4项，总155每模式，真实60s独立分组 |
| 全库OpenSpec strict | 1；openspec-all.json：原4份规格7项长度WARNING，14通过/4失败；未改无关规格 |

原始本地证据归档（稳定保存）：
`/Users/sansea/.codex/artifacts/devpilot/task75/task75-evidence.tar.gz`，SHA256：
`c48d2c6d7aa6398277745819bde37ae13ea6943aa7477b5e102adbe2289bc8cc`。
大型trace留在原始归档及后续GitHub Actions Artifacts，不反复提交Git。

## 首次失败记录与剩余风险

原始失败均保留，修复后从本轮首项重验：
1. 离线Goose构建因GOPROXY=off无法查询模块元信息；改读本地模块缓存文件代理，仍无网络，版本不变。
2. 首轮真实到期断言通过，清理因新fixture gate未关触发原Down保护；仅关闭无Run离线fixture gate，不伪造物理证明。
3. 第二轮到期/屏障均通过，新跨租户fixture重复建立固定账号失败；改用已有第二Workspace建立Monitor，安全断言不变。
4. 新测试屏障编译缺pgxpool import；补import。日志、原始新fixture源码和命令均在归档。

7.3保守unknown、精确canceled尚不支持、历史IPv620s及既有未确定风险继续保留，未宣称修复。
本地所有本轮容器及本轮启动的Docker运行时已停止；未预称独立批准。
PR保持Open，完成7.5交付后停止；不实施8.1，不合并、Archive、部署或开放生产网络。

## 冻结实现提交的实际 CI

实现/测试 SHA：`4434303831467630a969b12609f32d5b929143e1`，冻结时工作区干净。
以下两个workflow的verify、integration、execution-security均实际success后，才更新任务34/47。
记录跟进提交只改文档/任务索引，其CI结论以PR最新检查为准，不冒充已验证。

| 事件 | Workflow | 原始Artifact入口 |
| --- | --- | --- |
| Push | [40816288586](https://github.com/rjgczp/devpilot/actions/runs/40816288586) | [verify](https://github.com/rjgczp/devpilot/actions/runs/40816288586/artifacts/12872447032)、[integration](https://github.com/rjgczp/devpilot/actions/runs/40816288586/artifacts/12873440539)、[execution-security](https://github.com/rjgczp/devpilot/actions/runs/40816288586/artifacts/12875315625) |
| PR | [40816345802](https://github.com/rjgczp/devpilot/actions/runs/40816345802) | [verify](https://github.com/rjgczp/devpilot/actions/runs/40816345802/artifacts/12872860996)、[integration](https://github.com/rjgczp/devpilot/actions/runs/40816345802/artifacts/12873271319)、[execution-security](https://github.com/rjgczp/devpilot/actions/runs/40816345802/artifacts/12875177949) |

每workflow完整155 normal+155 race；原76+5内核安全、Budget23、历史Snapshot、
6.4/6.5/5.4、6.3、7.1/7.2/7.3/7.4回归及本轮12项联合用例保留并通过。
原始命令、退出码、实际HEAD/源码/二进制哈希及接收端/网络/签名/PG数据在对应Artifact。
PR integration Artifact已本地核对：head4434303、dirty=false、155/155、API80/0。
Push Artifact两次本地读取EOF，未重复测试；远端Artifact及其workflow实际成功记录仍可审阅。
本轮无新增未解决实现问题；历史风险与新测试开发首次失败均保留。
下一开发任务是8.1，本轮未实施；PR12保持Open等待独立审查，不合并。

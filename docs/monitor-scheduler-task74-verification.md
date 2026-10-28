# Task7.4 — 权威 execution-status 审查索引

范围：隔离 PG18/Linux fixture。基线 a1fc65a0dbc9c53259d9b06a411250575f35fa6d。
本轮产品明确接受7.3保守 unknown，核对原冻结证据后关闭7.3（32/47）。
精确 canceled 分类仍是后续待办；历史IPv6路由20s超时未定位，不宣称已修复。

## 实现与权限边界

- 复用现有 `internal/monitor/execution_status_handler.go` 的唯一GET路由、
  Cookie-only实时鉴权、5s HTTP/2s DB、拒绝query/条件头/body和固定错误。
  复用 `execution_status_store.go` 的一次QueryRow，不创建重复接口。
- Migration8仅修改原 `scheduler_private.execution_status(bytea,uuid,uuid,uuid)`
  的只读SQL函数体：明确Owner/Member，最近attempt以claimed_at/fence稳定排序；
  当前观察读7.2门控的latest_result_run_id，完整scope及当前v/g/epoch再次校验。
  最近completed独立保留历史谱系，最新unknown不被旧success遮盖。
- 意图、有限授权、调度、attempt分别保留原字段与枚举，各观察最多一条；
  in_flight仍依赖既有真实物理释放字段，不以Lease/HTTP结果推断返槽。
- 原SQL签名/owner/search_path/属性、PUBLIC REVOKE、API EXECUTE、全部其他ACL、
  角色与表列权限逐行不变。Migration1–7字节不变；Migration8函数体以外字节不变。
- `docs/api/openapi.yaml`仅补充现有字段说明，不改字段形状/枚举或Activation契约。
  不新增生产执行入口、不改物理终止/返槽/Result Commit/恢复协议。

## Requirement / Scenario / Test / Evidence

Change Delta `monitor-runs/spec.md` 的
`Execution status is a separate live authorized projection`、完整谱系及最小私密结果
要求映射如下；Design D11为合同依据，7.5完整跨接口矩阵不在本轮。

| Scenario | 实际测试 | 证据位置 |
| --- | --- | --- |
| Current identity and privacy remain mandatory | TestExecutionStatusHTTPAuthority | pg-final/{normal,race}.log；真实受限API LOGIN、Cookie、撤销/到期/禁用/移除/重入、双Workspace成员混合scope |
| Minimal status distinguishes intent and actual execution | 原TestCheckpointOneHTTPIntentStatusContract；TestExecutionStatusRealCompletedPG | 前者仅静态HTTP投影；后者实际GET/原内核/独立物理停止/固定Result Commit/真实HTTP读取 |
| Unknown is not masked by an earlier success | TestExecutionStatusProjectionIsolation；TestExecutionStatusRealUnknownPG | 旧completed与新unknown；后者真实Controller→unknown、可信释放前后in_flight true→false，unknown仍无HTTP结果 |
| Reauthorization cannot relabel an old observation | TestExecutionStatusProjectionIsolation的version/generation/epoch；TestExecutionStatusRealOldResultPG | 错误指针默认null；后者真实Owner PATCH后旧Run只留历史审计，不更新当前观察 |
| Same statement identity/scope/projection | TestExecutionStatusSingleStatementSnapshot | 独立事务未提交/已提交配置和成员变更；HTTP/API LOGIN前后完整快照，版本2/g3→版本3/g4，失权404 |
| Fixed errors, bounded timing, private output | TestExecutionStatusHTTPFailuresAndBudget；status74Decode | 真实表锁触发2s后503 database_timeout，恢复200，关闭依赖500；固定字段白名单、敏感sentinel和日志脱敏 |
| Minimal bounded OpenAPI DTO / no inferred state | TestExecutionStatusPopulatedOpenAPIContract；TestExecutionStatusOrthogonalSchema | 完整非空attempt六状态、三个可空单对象；拒绝私密字段/数组/虚构healthy枚举；原客户端不可用回退合同继续保留 |
| Least privileged API / no target networking | 原TestSchedulerRuntimeRolesHaveOnlyNarrowCapabilities；TestMonitorNoTargetNetwork新增两个子用例 | 真实LOGIN只能原API函数；原精确出站白名单、CGO/NSCD反例及正校准不改 |

## 执行记录与源码绑定

本地根目录 `/private/tmp/devpilot-status74/`；所有record.py回执含实际argv、UTC时间、
退出码、日志/二进制/源码清单哈希。dirty a1fc65a回执是开发证据，不能改写为最终SHA成功。
最终提交的GitHub Actions须重新执行并以同一干净实现SHA绑定。

- 新4项基础PG/HTTP测试与必要原2项回归，normal/race各6项，均exit0；
  `pg-final/normal.json`、`race.json`及原始日志。
- 新3项Linux真实物理链normal/race，6/6 exit0；`physical/result.json`、
  各目录的接收端/网络规则/strace/签名/PIDFD/PG账本与receipts。
- Go unit/race/vet已执行，实际回执见checks/。具体源码版次依回执，不以较早
  unit成功替代冻结CI；原76+5与6.4/6.5/5.4仍由既定CI完整复验。
- API原精确目的白名单保持不变；本轮审计结果及冻结CI结果仅在实际产生后登记。
- CI原147项全部保留，新增status74独立4项组，总151项每模式；保留120s进程预算、
  60s/45s独立组、不skip/retry。execution-security追加3×2真实status物理链，
  使用现有recovery合法测试二进制与监督拓扑，原有矩阵不删改。

## 原始失败保留与最小修复

1. 初次编译的新fixture误用Project指针输入/Monitor嵌入字段；只修正新测试类型。
   首次工具诊断有转录副本（不是伪称原始stdout），原源码及后续实际record回执保留。
2. 新行构造SQL参数$7同时被推断float8/bigint，PG42P08；显式bigint修复。
3. 新fixture将旧Run的claimed_at生成得更晚；一次只加诊断的定向复现证明
   fence1领取时间比fence2晚9.314ms，API当前指针本已正确，失败在最新attempt顺序预期。
   改用相同不可变schedule.created_at作为领取基准，完成时间独立；断言不放宽。
4. 合成unknown行阻止Goose Down，所有投影断言已经通过。只删除从未有Reservation/
   注册Worker/物理能力的合成投影行；不伪造停止字段/回执、不返任何实际容量。
   真实物理释放只用已批准链，并在单独6项测试中验收。

上述失败每次停止当次流程，未运行其后测试；修复后从本批首项重验。
原始PG日志、SQLSTATE、源码及诊断在negative/，不删除旧7.3/6.3负面证据。
先前SIGCONT/管理确认未确定原因及历史生产角色清单限制仍保留。

## 停止点

7.4仅在必要真实验收通过后勾选，冻结提交后继续核对实际Push/PR CI。
不以本索引预先声明CI成功或独立批准，不实施7.5。
PR11保持Open，生产执行硬关闭，不合并、不Archive、不部署。

提交材料检查补记：原始PG日志包含尾随空白，首次暂存diff检查报错后命令仍产生
本地提交adc1d70，尚未推送。以普通后续提交用哈希索引引用原件，保留原始归档
字节，不改写日志、不修改实现/断言、不强推；最终净diff再次严格检查。

## 冻结99497c3首次CI失败及单独授权的历史fixture修复

Push40656977308的integration在历史
`TestActivationExpiryAfterEveryProtectiveWait/updatefalse-activation`达到120s，
栈停在无界PID接收；该时刻没有仍在运行的写者goroutine。原始触发原因未确定，
不能据路径推定为Runner偶发问题。同SHA的PR40656985730 integration151 normal/
151race通过仅为对照，不抵销Push失败。原日志、栈、Artifact ZIP哈希、状态快照
及原始命令保存在`evidence/task74-activation-fixture/`指向的不可改写归档。

产品独立批准`call_ee313bbc2a494a2e9bef6eb5b5ee4e38`后，仅修改该测试：
强ETag预读移到原150ms窗口前；写者仍执行原ActivationUpdate及等待后实时复验；
PID屏障同时观察PID、写者提前返回和原6s context。全部权限断言、真实等待、
120s进程预算不变，无生产代码、权限或安全网络边界修改。

实际重新编译normal/race二进制，在新的network-none、受testdb守护的可销毁
PG18中各运行此一测试一次（每模式16子用例），exit0。具体argv、时间、
原始日志/二进制/源码哈希在normal.json/race.json；这些是dirty99497c3开发回执，
不冒充未来冻结SHA。新提交的完整Push/PR CI结果须另行核对。
首次缺失PID的内部触发原因仍未确定；定向通过不证明不存在调度或并发风险。

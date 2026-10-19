# add-http-monitor Verification

## Approval and scope

2026-10-19（Asia/Shanghai）用户确认独立设计评审通过、全部MVP选择，授权Apply及创建指向main的PR。实现只保存/管理HTTP Monitor配置，无目标DNS/HTTP探测、调度/激活/Worker/结果、DELETE/归档或独立角色；不自动合并、归档、公开部署。独立PR代码评审仍由后续评审者完成，不把本文的实现自检称为独立PR评审。

基线HEAD/main/origin/main为`66dcf69e60c457966ee3390ee2838a6076fae059`，分支`codex/add-http-monitor`。Propose文件是本聊天此前生成的未跟踪文件，已保留。未覆盖其他未提交工作。

## Evidence interpretation

- 规划：proposal/design/delta specs为审核后的预期契约，不能单独证明实现或测试通过。
- 实际本地执行：macOS arm64、Go1.24.3、Goosev3.26.0、独立Docker PostgreSQL18。工具/环境见`evidence/environment.json`。破坏性schema测试仅守护测试库，ordinary integration和integration-race顺序运行。Linux系统调用审计在独立测试容器网络命名空间执行。
- 托管CI：本文件初稿时尚未创建PR，不能宣称托管CI已成功。创建PR后按实际提交SHA和GitHub checks单独记录；本地结果不能替代托管CI。
- 未来探测引擎：DNS/IP分类、rebinding/绑定dial/TLS SNI/代理/跳转/出口等执行期验收未实现，未计为本轮通过。无监控被激活。

## Commands and actual local results

| 检查 | 命令/证据 | 状态 |
| --- | --- | --- |
| 格式与补丁 | `gofmt -l cmd internal tests`、`git diff --check` | PASS，无输出 |
| 静态检查 | `go vet ./...`、`go vet -tags=integration ./...` | 见`evidence/vet*.txt` |
| 全量单元 | `go test ./... -count=1` | 见`evidence/unit.txt` |
| 全量Go race | `go test -race ./... -count=1` | 见`evidence/race.txt` |
| PG18全量 | `go test -tags=integration ./... -count=1 -timeout=120s` | 见`evidence/integration.txt` |
| PG18 integration-race | `go test -race -tags=integration ./tests/integration -count=1 -timeout=120s` | 见`evidence/integration-race.txt` |
| Monitor逐场景 | `go test -tags=integration ./tests/integration -run '^TestMonitor' -count=1 -v` | 见`evidence/monitor-pg.txt` |
| 有界URL fuzz | `go test ./internal/monitor -run '^$' -fuzz '^FuzzCanonicalURL$' -fuzztime=10s -parallel=2` | PASS，351715执行；`evidence/fuzz.txt` |
| 依赖失败守护 | 编译后的integration.test仅运行TestMonitorMigration，6种不满足依赖环境 | 全部按预期exit1、无DDL；`evidence/dependency-guards.json`。PG版本17/18/19边界另由实际unit `TestServerIdentityAndVersionGuard`覆盖 |
| Migration1–5 | SHA256固定守护 `TestHistoricalMigrationsAreImmutable`，与基线逐字节对比 | 不变；`evidence/migration-hashes.json` |
| OpenSpec本Change | `openspec validate add-http-monitor --strict --json` | PASS无issue；`evidence/openspec-change-strict.json` |
| 全库strict | `openspec validate --all --strict --json` | 既有4份主规格7个长度WARNING使strict失败；`evidence/openspec-all-strict.json`。没有修改主规格消除警告 |

本地首次广泛回归发现历史auth契约精确API白名单尚未扩展，已只增加两个Monitor路径的四个方法；旧API operation/schema与基线JSON深比较相同。首次late-commit夹具直接INSERT持有Project FK KEY SHARE，正确阻塞受支持Store的Project UPDATE；已将该MVCC游标夹具明确改为两条guarded SQL插入，生产父先于子保护不变。成员移除测试沿用既有“Owner不能移除另一Owner”的协议：Owner退出/角色降级和普通Member移除分别测试，没有扩大旧权限。

## Calibrated zero-target-network acceptance

可复验命令（本地实际运行，完整输出`evidence/network-local.txt`）：

```sh
MONITOR_AUDIT_OUTPUT=/absolute/disposable/evidence tests/network/run-monitor-audit-docker.sh
```

脚本使用当前Go1.24.3以`integration,networkaudit`标签编译Linux二进制和固定Goose，创建独立无宿主映射端口PG18容器；工具镜像与CI固定同一PG18 digest，只有strace/iptables/Python。审计进程只加NET_ADMIN/SYS_PTRACE并共享此独立测试容器的网络命名空间，不使用宿主网络/宿主防火墙/应用DB。IPv4/IPv6 firewall在被测代码前拒绝未批准出站；strace -yy独立记录connect/sendto/sendmsg/sendmmsg意图并校验TCP协议，不把同端口UDP当作批准通信。

| 工作负载 | child exit | audit exit | 精确允许尝试 | 未批准尝试 |
| --- | --- | --- | --- | --- |
| 正向对照：人工TCP192.0.2.1:80 | 0 | **1** | 0 | **1，被准确检测** |
| 真实Monitor HTTP流程 | 0 | **0** | **4** | **0** |

正向对照的child刻意在连接被firewall拒绝后exit0，外部探针必须根据实际尝试单独exit1，harness还核对准确目的192.0.2.1:80，避免把任意脚本失败当校准成功。真实流程从注册/登录、Project创建、邀请接受开始，执行Monitor POST/list/detail/PATCH、旧条件/混合租户/Member写拒绝、退出和logout。批准的目的仅TCP127.0.0.1:5432（DB）和TCP127.0.0.1:18081（真实本地HTTP测试server）；其余包括DNS/IPv6/Unix/无法分类目的失败关闭，不以连接失败、不可达、超时或无响应排除尝试。

命令、子测试输出、classifier输出与退出码见`evidence/control.*`、`evidence/real.*`。完整syscall traces已保存在`evidence/control.trace`与`evidence/real.trace`，可由上述脚本复现，托管CI另上传完整trace artifact。静态来源审核为`TestMonitorHasNoTargetNetworkCapability`和`evidence/monitor-imports.txt`，检查生产模块import/call边界，不把AST检查或库依赖图单独当运行证据。

## Requirement to executed test mapping

| Capability / Requirement | 对应实际case |
| --- | --- |
| management: inherited configuration permissions | `TestMonitorHTTPJourneyAndIsolation`、`TestMonitorStorePermissionsPaginationAndVersions` |
| strict mutation JSON | `TestStrictInput`、`TestMonitorInputProtocol`、`TestMonitorMethodCSRFSelectorsAndConditions` |
| deterministic fields | `TestStrictInput`、`TestMonitorMergedConfigurationRevalidated` |
| offline URL policy | `TestCanonicalURL`、`TestRejectedURLs`、`TestURLPathRawPathSerialization`、`FuzzCanonicalURL` |
| no target networking / no activation | `TestMonitorHasNoTargetNetworkCapability`、dedicated Linux `TestMonitorNoTargetNetwork`及positive control；精确DB列集合/四操作契约 |
| live reads and complete tenant scope | `TestMonitorCreatorReferenceAndEmptyScope`、`TestMonitorReadSnapshotAndLateCommit`、HTTP journey和Store权限矩阵 |
| list bounds and minimal projection | Store分页、`TestMonitorOpenAPIResponsesAndProjection`；列表schema无URL |
| single representation and version | HTTP journey、OpenAPI contract、PG defaults；Project/Monitor independent version测试 |
| strong conditional updates | `TestSelectorsAndTag`、HTTP条件语法/固定错误、`TestMonitorConcurrentVersions`、Store ABA/overflow |
| transport/CSRF/budgets/privacy | `TestMonitorSlowBodiesAndEarlyDrainAreBounded`、`TestMonitorClientDisconnectCancelsMutation`、`TestMonitorServerRejectsPathAliasesWithoutRedirect`、`TestMonitorPostWritePanicAborts`、failure/panic tests及PG HTTP fault suite |
| persistence: schema/provenance | `TestMonitorMigration`：defaults/NOT NULL/CHECK/13列/三索引/不可变与等值赋值 |
| composite references | Migration混合父ID与临时三元组child拒绝、`TestMonitorSameTenantSerializationAndParentReferences`真实父删除/FK插入双顺序、isolated creator RESTRICT |
| protected SQL and lock compatibility | `TestMonitorIdentityAndMembershipOrderings`、`TestMonitorProjectWriteOrderings`、`TestMonitorDelayedOwnerRequestCannotUseRejoin`、`TestMonitorCleanupCandidateOrdering` |
| post-wait expiry | `TestMonitorExpiryAfterEveryProtectiveWait`：User/Session/Workspace/Membership/Project/Monitor各等待及post-gate，DB等待观测+数据库时间推进 |
| atomic complete config / version | `TestMonitorConcurrentVersions`、`TestMonitorMergedConfigurationRevalidated`、ABA/overflow和父版本断言 |
| confirmed Commit / uncertain outcome | `TestMonitorFaultAtomicityAndTenantIndependence`真实Rollback后fault/Commit丢ACK且提交计数≤1、`TestMonitorHTTPFaultsCancellationAndRecovery` |
| Migration6 and historical boundaries | `TestMonitorMigration`0→6/存量5→6/up/up/真实6失败/6→5→6；全历史迁移回归及next failure7 |
| PG18/race/full regression | 全量integration与integration-race日志、`TestDatabaseGuard`/版本守护、六种依赖失败exit1 |
| future SSRF execution boundary | 文档契约审查，本轮**没有实现或执行引擎安全测试**，后续Change必须单独验收 |
| workspace-access minimal extension | 原有Workspace/member/invitation/Project全部回归；新增Monitor同继承权限，旧API DTO/method/schema深比较不变 |

所有数据库顺序测试使用既有真实pgx事务包装器，仅控制屏障、pg_backend_pid/pg_blocking_pids观测与故障回执，不替换生产授权SQL。Go Race Detector与PG并发证据分开。维护SQL/多Owner fixture不代表新增公开接口。

## Known limits and review handoff

配置可接受不代表域名存在、可达或执行期SSRF安全。URL path仍可含秘密，只对授权单项公开，列表/错误/日志省略。当前同Workspace写串行，POST不幂等，未知提交可能保留配置，不能凭名称/URL唯一恢复请求。未来激活、执行期安全及结果version一致性必须另行Change。

独立PR代码评审待进行，包含URL/IDNA、完整SQL归属、FK/锁顺序、post-wait时间、未知Commit、探针校准和迁移边界；不自动合并、归档、部署或激活。


## Final local acceptance

最后一轮本地串行验证脚本以set -e运行，整体exit0：Monitor逐场景集成、普通vet、integration vet、全量unit、全量unit-race、全量PG18 integration及integration-race全部PASS。Monitor集成日志含97个PASS场景（父测试与子测试分别计数）；全量PG18日志保留包级结果，不能把97解释为所有历史场景总数。普通PG18 integration约58秒，integration-race实际耗时见对应日志。格式检查和git diff --check无输出。

除独立PR代码评审/其发现的后续修复外，本地实现验收已完成；托管CI结果将在PR创建后独立采集，不用本地日志代替。

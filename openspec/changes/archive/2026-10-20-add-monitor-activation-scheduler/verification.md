# Activation Intent Foundation — Apply Verification

## 授权、基线与范围

2026-10-19（Asia/Shanghai）用户确认独立设计评审通过并正式批准方案A，授权代码、隔离测试、提交和指向main的待审PR；未授权合并、Archive、部署、应用库迁移或生产探测。此文是实现者的验收记录，不冒称独立安全审查。

初始HEAD/main/origin/main及GitHub ref为`2cfdcd6c0c094e938fc0570b67697346c3da7aee`；PR#9已合并（merge `f14a63118f12a09f5fd3b8c17f4c83cd09fe02fa`）、九个Change已归档。唯一初始未跟踪内容为已批准规划目录，已保留；实施分支`codex/add-monitor-activation-scheduler`。`evidence/baseline.json`保留全部批准工件和Migration1–6 SHA256；proposal/design/五份delta保持字节不变，tasks仅按实测勾选。

实现只有独立activation意图资源、Migration7及文档/验收。没有Scheduler、Monitor Worker、cron、执行开关、任务、租约、run或探测结果。原API仅继续Session清理Worker，不导入monitoring内核。所有activation响应固定blocked/scheduler_unavailable、next_due_at=null；新旧Monitor默认disabled。原配置DTO、monitor tag和Project版本规则保持。

本地命令元数据在`evidence/<label>.json`，原始输出同名`.txt`。实现尚未提交时元数据诚实记录基线HEAD及dirty=true，不将其当作实施提交；`source-manifest.json`绑定最终代码/测试/迁移/文档字节，发布后在下文追加实际实施SHA与托管CI，不以旧main结果替代。最终PR head的CI还须另行确认。

## 工具与隔离环境

本地macOS arm64、Go1.24.3、pgx/v5 v5.8.0、固定原生Goose3.26.0；独立Docker PG18.6，固定镜像digest与既有CI一致，只绑定127.0.0.1:55437，数据库`devpilot_registration_test`。未访问或迁移应用DB；Down只由既有testdb保护的可销毁库执行。`initial-schema6.*`记录先显式建立schema6；新7于隔离库验收。

Linux审计通过两个独立runner运行。配置API审计仅允许原有精确PG/localHTTP tuple；新激活流量没有增加resolver/target白名单。执行内核网络fixture没有上游路由、probe无DB/Cookie凭据；其DNS/HTTP/TLS均为隔离fixture。Docker构建与镜像来源、纯Go/CGO0、控制子进程、trace/receiver/防火墙证据分别保存。未执行生产部署级出口验证，实际监控从未启用。

## Migration7 与锁图

`monitor_activation_states`仅13列，一对一三元组PK/FK、enabled计数/actor索引、合法state/version/g/actor组合、next_due恒NULL、身份及created_at不可变、时间/冷却不回退。普通SQL删除/TRUNCATE和代次重置被拒绝。Migration7持Monitor SHARE ROW EXCLUSIVE锁直至回填/trigger安装及提交；历史配置version/URL/来源/时间不变，原有邀请五种状态及撤销Session也保持。

`TestActivationMigration`实测0→7、存量6→7、repeat up、真实7创建对象后除零故障全回滚、初始化失败导致Monitor插入回滚、7→6→0。旧Migration6 suite显式up-to6；下一故障版本夹具为8且包含真实7。历史六份SQL字节哈希未变。溢出/缺失状态/失败trigger的测试使用可销毁库管理员夹具构造不可达状态，原子恢复trigger；生产代码没有DISABLE TRIGGER路径。

实际写保护链：User SHARE → Session SHARE → Workspace NO KEY UPDATE → actor Membership UPDATE → Project UPDATE → Monitor UPDATE → Activation UPDATE。最终**新语句**按statement_timestamp重新检查身份、Session时效、当前Owner和完整scope，并直接门控UPDATE/版本代次/配额/冷却；不信Handler或锁前快照。配置PATCH的旧Store无需新增第二次更新：数据库trigger只在Monitor→Activation方向撤销授权，actor=NULL，保留冷却。新Owner命令actor的User已在最早持SHARE，新增FK不向其他User反向加锁。

真实独立连接/事务barrier配合pg_blocking_pids、pg_locks未授予锁及pg_stat_activity Lock等待验证。每级等待过期、授权撤销/禁用/退出/成员角色/重新加入、邀请接受、清理和Project写均覆盖两个顺序；无子→父锁序、弱锁升级、全局APIquota锁或网络期间业务锁。READ COMMITTED最终门控之后自然过期允许提交，符合原协议；取消/死锁整次失败，无业务重放。

## 实测矩阵

| 类别 | 真实结果与证据 |
| --- | --- |
| 状态/权限/CAS | `activation-final.*`、`activation-final-http.*`：默认g1；Owner enable/pause/恢复/同值；Member仅读；scope替换；v/g和body确认；两Owner最多一成功；PATCH前后竞争；旧tag/ABA失效 |
| 实时授权与锁 | 上述PG输出：User/Session/Workspace/Membership/Project/Monitor/Activation每级等待、post-gate时效边界、退出/重入Member、撤销/禁用/清理和Project两个顺序；真实锁观测 |
| 配额/冷却 | `TestActivationQuotaAndCAS`：99个真实Owner启用后两Store争第100/101项，精确100、409；60秒持久化冷却429；暂停/PATCH不受满额和冷却阻挡，同值enable不重复占名额 |
| Commit故障 | `TestActivationCommitFaults`及`TestActivationHTTPUnknownCommitAndFaults`：实际Rollback、真实Commit成功后丢确认、HTTP安全503且无data/ETag、GET对账、原tag412、没有自动重试；缺表/死锁/超时分类 |
| Trigger与审计 | `TestActivationTriggerAtomicFailureAndBoundedAudit`：任意字段及同值PATCH失效；缺行/g溢出/故障全部回滚；64次命令只有一状态行；actor不外露；Workspace意图不随原Owner退出/Session撤销消失 |
| HTTP/文档 | `TestActivationStrictProtocol/Transport/TransportEdgesAndDeadline/ErrorsAndCancellation/OpenAPI`：1024字节双字段、128字节强tag、媒体/重复键/Unicode/条件/query/RawPath、CSRF早拒绝零DB、取消/deadline/panic、精确14字段及422独立字段schema |
| 全量Go | `unit-complete.*`、`race-final.*`、`vet.*`及最终交付回归；gofmt、diff检查输出见收尾记录 |
| PG18历史回归 | `integration-current.*`、`integration-race.*`及最终delivery运行；完整前九阶段normal/race，schema变更suite串行，真实7生命周期未skip |
| 配置/意图零目标网络 | `config-network-delivery.*`及目录：control child0/audit1，准确1个违规192.0.2.1:80；real child0/audit0、16批准基础设施尝试、0未批准目标。包含真实HTTP enable/pause/PATCH/重启、100 enabled与HTTP未知Commit恢复 |
| 第九阶段真实网络 | `execution-network.*`及目录：76场景，72正常audit0与4故意绕过audit1，child均0；DNS变化/混合RR/IP绑定/SNI/TLS/redirect/proxy/header-only/超时/取消/资源、IPv4/6独立出口阻断与正向fixture |
| 第九阶段PG联动 | `snapshot-pg.*`、`snapshot-pg-race.*`及目录：schema7只读LOGIN角色、scope/原子快照、禁止身份读/DDL/写、释放pool/locks；v7在途时合法v8 PATCH/撤销完成，旧结果仍属v7；子进程无DB/Cookie |
| 审计器/fuzz | `python-network.*`7项与`python-execution.*`2项通过；`dns-fuzz.*`128800次、`address-fuzz.*`161133次；只作有界回归，不外推穷尽证明 |
| OpenSpec | `openspec-change.*` strict exit0/issues=[]；`openspec-all.*` exit1仅四份历史规格七项长度WARNING；主规格未改动 |

每个原始网络目录都有构建/命令/exit/trace及控制证据。网络错误不可达不是零attempt证明，本轮依赖实际系统调用分类器。部署环境未提供，以上namespace默认deny通过不能声称任意生产网络SSRF安全。

## Requirement → Test → Evidence

完整逐Requirement/Scenario索引见下方自动生成附表，涵盖五份Delta的23条要求/66个场景。后续Worker重授权/生产门禁要求在本轮以无入口、固定blocked、文档/依赖contract验收；没有虚构未批准B的运行测试。

## 保留的失败与修正

- `first-unit.*`：旧临时Go缓存引用已失效源路径；改用本轮独立GOCACHE与现有GOMODCACHE，未改依赖。
- 初始文档/路由守护失败：为批准的activation资源新增精确API允许项/错误schema，仍检查原配置响应和完整白名单；不删除旧断言。
- 初始Migration7测试误把合法g+1当成非法跳变，改测g+2；测试重复使用同名账户fixture改为在原scope创建Monitor，协议不变。
- `integration-initial.*`与`config-network.*`：旧HTTP测试helper未给PUT附媒体/CSRF，服务器正确403；修正helper。旧维护SQL夹具需消耗配置version，断言相应改为新version；不放宽新guard。
- `unit-last.*`：既有注册1ms deadline/5ms sleep测试一次时序失败。未改认证代码；`registration-deadline-regression.*`单独20次通过、`unit-complete.*`全量复验通过，保留该风险记录及全部失败输出。

## 剩余风险与交付门禁

配置任意成功PATCH都会暂停意图，包括名称/同值，已按批准行为实现。Workspace串行写与100意图上限适用于A，不是分布式探测速率/公平性保证。只保留最末审计，不能完整追责历史。DBA/超级用户可停trigger的权限不属于普通SQL不变量保护。对外不公开actor/URL，错误与普通日志没有原始网络/SQL/凭据。

生产出口未验收，未创建生产Worker、Scheduler或真实探测入口；未来必须另行批准、独立网络隔离、清除旧enabled并推进g且由当前Owner重确认。HTTP exactly-once、租约/fencing、结果和外部网络栅栏均未交付。PR保持Open，等待独立技术及安全代码审查；不得自动合并、归档或部署。

## 实施提交与托管CI

2026-10-20（Asia/Shanghai）已通过已连接GitHub App发布实施提交 **`3473d528c4f5e37c365e6a9206ef66e8a478a651`**，源树`3c8fc5c1841007544057065e6d185d17e1204a8f`与本地暂存区逐字节一致；本地分支已核对并前移到同一SHA，没有reset/覆盖文件。[PR #10](https://github.com/rjgczp/devpilot/pull/10)指向main、保持Open且未合并。

- [Push run39352867538](https://github.com/rjgczp/devpilot/actions/runs/39352867538)和[PR run39353091460](https://github.com/rjgczp/devpilot/actions/runs/39353091460)均completed/success，head_sha精确为上述实施SHA；两次的verify、integration、execution-security及所有要求步骤均success。完整逐步骤回执在`evidence/hosted-implementation-ci.json`。Activation专项、真实PG18 normal/race、配置零目标审计、76执行器网络场景和schema7 PG快照normal/race均实际运行，没有跳过安全步骤。
- 实际下载Push的两个Artifact并按GitHub摘要核验：`monitor-network-evidence` ID12234551279（SHA256 `5d077b95e34a81c091c214e571e300ee25f026d2e73fc4a091d4b59fb8c023b3`）；`execution-security-evidence` ID12234341089（SHA256 `1475aef669cb5c8836cf0b3e91a0c0b76f975be329f87d06d584878aae939a83`）。原始ZIP分别保留于`evidence/hosted-implementation-config.zip`和`evidence/hosted-implementation-execution.zip`，无需依赖90天托管保留期；逐文件SHA见`hosted-artifact-manifest.json`。
- 托管配置控制child0/audit1、准确1违规；real child0/audit0、16基础设施批准attempt/0目标违规。托管Linux/amd64/Go1.24.3/CGO0的76场景中72正常audit0、4故意绕过audit1且child均0；实际检查正常/race PG联动v7/v8输出及各自零违规audit。核验摘要在`hosted-artifact-check.json`。
- 23/23 tasks依真实结果完成，Change strict通过；全库四份历史规格七项描述长度WARNING保持。Migrations1–6、批准proposal/design/delta及主规格保持不变。没有实际监控入口或生产出口结论。

以上CI和Artifact属于**明确实施SHA**，不是旧main结果，也不冒充追加本回执后的文档提交。最终交付必须再检查最新PR head对应的Push/PR CI；其实际最新SHA、链接和结果在交付回复中报告。PR等待独立技术及安全代码审查，不合并/归档/部署。

## 完整场景映射附表

证据路径相对本Change的`evidence/`，策略说明相对仓库。场景在对应requirement的测试组内验证，部署与未来Worker事项明确仅为门禁/文档。

| Requirement | Scenario | Actual tests / validation | Evidence |
| --- | --- | --- | --- |
| `http-monitor-execution` / Single execution is internal and never activates configuration | Success - Deployment preserves inactive history | TestDormantProductionDependencyAndSchemaBoundary; TestMonitorNoTargetNetwork | integration-delivery.txt; config-network-delivery/ |
| `http-monitor-execution` / Single execution is internal and never activates configuration | Security - No public or hidden trigger | TestDormantProductionDependencyAndSchemaBoundary; TestMonitorNoTargetNetwork | integration-delivery.txt; config-network-delivery/ |
| `http-monitor-execution` / Single execution is internal and never activates configuration | Success - Controlled invocation without production targets | TestDormantProductionDependencyAndSchemaBoundary; TestMonitorNoTargetNetwork | integration-delivery.txt; config-network-delivery/ |
| `http-monitor-execution` / Acceptance reports unit network database and egress evidence separately | Security - Required real network matrix | Independent Linux execution/configuration runners; TestSystemSnapshotPG*; PG normal/race; planning is not a pass | execution-network/; config-network-delivery/; snapshot-pg-race/; integration-delivery.txt; integration-race.txt |
| `http-monitor-execution` / Acceptance reports unit network database and egress evidence separately | Concurrency - Required PG18 and regression evidence | Independent Linux execution/configuration runners; TestSystemSnapshotPG*; PG normal/race; planning is not a pass | execution-network/; config-network-delivery/; snapshot-pg-race/; integration-delivery.txt; integration-race.txt |
| `http-monitor-execution` / Acceptance reports unit network database and egress evidence separately | Security - Separate detector calibration and historical audit | Independent Linux execution/configuration runners; TestSystemSnapshotPG*; PG normal/race; planning is not a pass | execution-network/; config-network-delivery/; snapshot-pg-race/; integration-delivery.txt; integration-race.txt |
| `http-monitor-execution` / Acceptance reports unit network database and egress evidence separately | Exception - Planning and missing infrastructure are not passes | Independent Linux execution/configuration runners; TestSystemSnapshotPG*; PG normal/race; planning is not a pass | execution-network/; config-network-delivery/; snapshot-pg-race/; integration-delivery.txt; integration-race.txt |
| `http-monitor-management` / Monitors are configuration resources with inherited permissions | Success - Owner creates and Member reads configuration | TestMonitorStorePermissionsPaginationAndVersions; TestMonitorHTTPJourneyAndIsolation; TestActivationStatePermissionsAndInvalidation | integration-delivery.txt; config-network-delivery/real.test.txt |
| `http-monitor-management` / Monitors are configuration resources with inherited permissions | Security - Creator leaves and readable Member cannot write | TestMonitorStorePermissionsPaginationAndVersions; TestMonitorHTTPJourneyAndIsolation; TestActivationStatePermissionsAndInvalidation | integration-delivery.txt; config-network-delivery/real.test.txt |
| `http-monitor-management` / Monitors are configuration resources with inherited permissions | Exception - Unsupported operations | TestMonitorStorePermissionsPaginationAndVersions; TestMonitorHTTPJourneyAndIsolation; TestActivationStatePermissionsAndInvalidation | integration-delivery.txt; config-network-delivery/real.test.txt |
| `http-monitor-management` / Configuration management never performs target networking | Security - Syntactically valid hostile DNS name | TestMonitorNoTargetNetwork; TestMonitorHTTPJourneyAndIsolation | config-network-delivery/real.audit.txt; config-network-delivery/real.trace |
| `http-monitor-management` / Configuration management never performs target networking | Success - No activation implied by persistence | TestMonitorNoTargetNetwork; TestMonitorHTTPJourneyAndIsolation | config-network-delivery/real.audit.txt; config-network-delivery/real.trace |
| `http-monitor-management` / Configuration management never performs target networking | Security - Intent endpoints are also offline | TestMonitorNoTargetNetwork; TestMonitorHTTPJourneyAndIsolation | config-network-delivery/real.audit.txt; config-network-delivery/real.trace |
| `http-monitor-persistence` / Migration seven creates constrained dormant intent without rewriting history | Success - Fresh and populated upgrades | TestActivationMigration; TestActivationTriggerAtomicFailureAndBoundedAudit; TestHistoricalMigrationsAreImmutable; TestDatabaseGuard | integration-delivery.txt; activation-final-http.txt; baseline.json |
| `http-monitor-persistence` / Migration seven creates constrained dormant intent without rewriting history | Exception - Atomic migration and configuration rollback | TestActivationMigration; TestActivationTriggerAtomicFailureAndBoundedAudit; TestHistoricalMigrationsAreImmutable; TestDatabaseGuard | integration-delivery.txt; activation-final-http.txt; baseline.json |
| `http-monitor-persistence` / Migration seven creates constrained dormant intent without rewriting history | Security - Composite identity and ordinary SQL guards | TestActivationMigration; TestActivationTriggerAtomicFailureAndBoundedAudit; TestHistoricalMigrationsAreImmutable; TestDatabaseGuard | integration-delivery.txt; activation-final-http.txt; baseline.json |
| `http-monitor-persistence` / Migration seven creates constrained dormant intent without rewriting history | Security - Generation and cooling cannot be reset | TestActivationMigration; TestActivationTriggerAtomicFailureAndBoundedAudit; TestHistoricalMigrationsAreImmutable; TestDatabaseGuard | integration-delivery.txt; activation-final-http.txt; baseline.json |
| `http-monitor-persistence` / Migration seven creates constrained dormant intent without rewriting history | Security - Guarded native Down and old schema | TestActivationMigration; TestActivationTriggerAtomicFailureAndBoundedAudit; TestHistoricalMigrationsAreImmutable; TestDatabaseGuard | integration-delivery.txt; activation-final-http.txt; baseline.json |
| `http-monitor-persistence` / Monitors store minimal constrained configuration and provenance | Success - Database generated configuration | TestMonitorMigration; TestActivationMigration; TestActivationStatePermissionsAndInvalidation | integration-delivery.txt |
| `http-monitor-persistence` / Monitors store minimal constrained configuration and provenance | Exception - Documented database validation subset | TestMonitorMigration; TestActivationMigration; TestActivationStatePermissionsAndInvalidation | integration-delivery.txt |
| `http-monitor-persistence` / Monitors store minimal constrained configuration and provenance | Security - Identity and tenant immutability | TestMonitorMigration; TestActivationMigration; TestActivationStatePermissionsAndInvalidation | integration-delivery.txt |
| `http-monitor-persistence` / Composite references preserve project and monitor tenant ownership | Security - Mixed tenant parent cannot persist | TestMonitorMigration; TestActivationMigration; TestMonitorSameTenantSerializationAndParentReferences | integration-delivery.txt |
| `http-monitor-persistence` / Composite references preserve project and monitor tenant ownership | Success - Safe future child identity and list index | TestMonitorMigration; TestActivationMigration; TestMonitorSameTenantSerializationAndParentReferences | integration-delivery.txt |
| `http-monitor-persistence` / Composite references preserve project and monitor tenant ownership | Exception - Independent creator lifetime and restricted deletion | TestMonitorMigration; TestActivationMigration; TestMonitorSameTenantSerializationAndParentReferences | integration-delivery.txt |
| `http-monitor-persistence` / Composite references preserve project and monitor tenant ownership | Concurrency - Foreign key protection | TestMonitorMigration; TestActivationMigration; TestMonitorSameTenantSerializationAndParentReferences | integration-delivery.txt |
| `http-monitor-persistence` / Monitor writes protect live authority before actual DML | Success - Compatible protected write | TestActivationExpiryAfterEveryProtectiveWait; TestActivationIdentityAndMembershipOrderings; TestMonitorIdentityAndMembershipOrderings; TestActivationDelayedOwnerRequestCannotUseRejoin | activation-final-http.txt; integration-delivery.txt |
| `http-monitor-persistence` / Monitor writes protect live authority before actual DML | Security - Direct Store invocation is still tenant authorized | TestActivationExpiryAfterEveryProtectiveWait; TestActivationIdentityAndMembershipOrderings; TestMonitorIdentityAndMembershipOrderings; TestActivationDelayedOwnerRequestCannotUseRejoin | activation-final-http.txt; integration-delivery.txt |
| `http-monitor-persistence` / Monitor writes protect live authority before actual DML | Concurrency - Authority loss in both orders | TestActivationExpiryAfterEveryProtectiveWait; TestActivationIdentityAndMembershipOrderings; TestMonitorIdentityAndMembershipOrderings; TestActivationDelayedOwnerRequestCannotUseRejoin | activation-final-http.txt; integration-delivery.txt |
| `http-monitor-persistence` / Monitor writes protect live authority before actual DML | Concurrency - Time is rechecked after all waits | TestActivationExpiryAfterEveryProtectiveWait; TestActivationIdentityAndMembershipOrderings; TestMonitorIdentityAndMembershipOrderings; TestActivationDelayedOwnerRequestCannotUseRejoin | activation-final-http.txt; integration-delivery.txt |
| `http-monitor-persistence` / Lock compatibility is verified with existing project and authority writers | Concurrency - Project update and Monitor write | TestActivationProjectWriteOrderings; TestActivationIdentityAndMembershipOrderings; TestActivationCleanupCandidateOrdering; waitActivationBlock | activation-final-http.txt; integration-race.txt |
| `http-monitor-persistence` / Lock compatibility is verified with existing project and authority writers | Concurrency - FK locks and independent scopes | TestActivationProjectWriteOrderings; TestActivationIdentityAndMembershipOrderings; TestActivationCleanupCandidateOrdering; waitActivationBlock | activation-final-http.txt; integration-race.txt |
| `http-monitor-persistence` / Monitor versions atomically guard complete configuration | Concurrency - Two distinct Owners and ABA | TestMonitorConcurrentVersions; TestActivationTwoOwnerCASAndConfigOrderings; TestActivationTriggerAtomicFailureAndBoundedAudit | integration-delivery.txt; activation-final-http.txt |
| `http-monitor-persistence` / Monitor versions atomically guard complete configuration | Success - Complete future configuration handoff | TestMonitorConcurrentVersions; TestActivationTwoOwnerCASAndConfigOrderings; TestActivationTriggerAtomicFailureAndBoundedAudit | integration-delivery.txt; activation-final-http.txt |
| `http-monitor-persistence` / Future probing requires a separately verified SSRF execution boundary | Security - Rebinding TOCTOU and redirect escape | TestMonitorNoTargetNetwork; TestSystemSnapshotPGLiveIsolatedExecution; independent76-case execution runner; future production remains closed | config-network-delivery/; snapshot-pg-race.txt; execution-network/summary.json |
| `http-monitor-persistence` / Future probing requires a separately verified SSRF execution boundary | Success - Configuration remains dormant without new schema | TestMonitorNoTargetNetwork; TestSystemSnapshotPGLiveIsolatedExecution; independent76-case execution runner; future production remains closed | config-network-delivery/; snapshot-pg-race.txt; execution-network/summary.json |
| `http-monitor-persistence` / Future probing requires a separately verified SSRF execution boundary | Concurrency - Internal execution does not extend write transactions | TestMonitorNoTargetNetwork; TestSystemSnapshotPGLiveIsolatedExecution; independent76-case execution runner; future production remains closed | config-network-delivery/; snapshot-pg-race.txt; execution-network/summary.json |
| `http-monitor-persistence` / Future probing requires a separately verified SSRF execution boundary | Security - Internal library is not production egress approval | TestMonitorNoTargetNetwork; TestSystemSnapshotPGLiveIsolatedExecution; independent76-case execution runner; future production remains closed | config-network-delivery/; snapshot-pg-race.txt; execution-network/summary.json |
| `monitor-activation` / Activation intent is disabled by default and distinct from execution | Success - Existing and newly created monitors remain dormant | TestActivationMigration; TestMonitorHTTPJourneyAndIsolation | integration-delivery.txt; config-network-delivery/real.test.txt |
| `monitor-activation` / Activation intent is disabled by default and distinct from execution | Success - Enabling records intent without claiming execution | TestActivationMigration; TestMonitorHTTPJourneyAndIsolation | integration-delivery.txt; config-network-delivery/real.test.txt |
| `monitor-activation` / Activation reads and writes enforce current scoped authority | Security - Owner and Member permissions | TestActivationStatePermissionsAndInvalidation; TestActivationIdentityAndMembershipOrderings; TestActivationExpiryAfterEveryProtectiveWait; TestActivationDelayedOwnerRequestCannotUseRejoin | activation-final-http.txt; integration-delivery.txt |
| `monitor-activation` / Activation reads and writes enforce current scoped authority | Security - Lost identity and substituted scope | TestActivationStatePermissionsAndInvalidation; TestActivationIdentityAndMembershipOrderings; TestActivationExpiryAfterEveryProtectiveWait; TestActivationDelayedOwnerRequestCannotUseRejoin | activation-final-http.txt; integration-delivery.txt |
| `monitor-activation` / Activation reads and writes enforce current scoped authority | Concurrency - Authority and expiry are checked after waits | TestActivationStatePermissionsAndInvalidation; TestActivationIdentityAndMembershipOrderings; TestActivationExpiryAfterEveryProtectiveWait; TestActivationDelayedOwnerRequestCannotUseRejoin | activation-final-http.txt; integration-delivery.txt |
| `monitor-activation` / Commands confirm configuration and consume a separate generation | Success - Explicit enable pause and resume | TestActivationStatePermissionsAndInvalidation; TestActivationTriggerAtomicFailureAndBoundedAudit | activation-final-http.txt |
| `monitor-activation` / Commands confirm configuration and consume a separate generation | Concurrency - Same value and ABA | TestActivationStatePermissionsAndInvalidation; TestActivationTriggerAtomicFailureAndBoundedAudit | activation-final-http.txt |
| `monitor-activation` / Commands confirm configuration and consume a separate generation | Exception - Generation exhaustion | TestActivationStatePermissionsAndInvalidation; TestActivationTriggerAtomicFailureAndBoundedAudit | activation-final-http.txt |
| `monitor-activation` / Every configuration revision atomically revokes activation | Success - Conservative revocation for all fields | TestActivationStatePermissionsAndInvalidation; TestActivationTwoOwnerCASAndConfigOrderings; TestActivationTriggerAtomicFailureAndBoundedAudit | activation-final-http.txt; integration-delivery.txt |
| `monitor-activation` / Every configuration revision atomically revokes activation | Concurrency - Configuration and activation serialize | TestActivationStatePermissionsAndInvalidation; TestActivationTwoOwnerCASAndConfigOrderings; TestActivationTriggerAtomicFailureAndBoundedAudit | activation-final-http.txt; integration-delivery.txt |
| `monitor-activation` / Activation uses one strong precondition over configuration and generation | Exception - Strict conditional grammar and error precedence | TestActivationStrictProtocol; TestActivationTwoOwnerCASAndConfigOrderings; TestActivationCommitFaults; TestActivationHTTPUnknownCommitAndFaults | activation-doc-contract.txt; activation-final-http.txt |
| `monitor-activation` / Activation uses one strong precondition over configuration and generation | Concurrency - Two Owners consume one condition | TestActivationStrictProtocol; TestActivationTwoOwnerCASAndConfigOrderings; TestActivationCommitFaults; TestActivationHTTPUnknownCommitAndFaults | activation-doc-contract.txt; activation-final-http.txt |
| `monitor-activation` / Activation uses one strong precondition over configuration and generation | Exception - Unknown commit and response loss | TestActivationStrictProtocol; TestActivationTwoOwnerCASAndConfigOrderings; TestActivationCommitFaults; TestActivationHTTPUnknownCommitAndFaults | activation-doc-contract.txt; activation-final-http.txt |
| `monitor-activation` / Activation transport and response projections are bounded and private | Exception - Body path and method rejection | TestActivationTransport; TestActivationTransportEdgesAndDeadline; TestActivationErrorsAndCancellation; TestActivationOpenAPI; TestMonitorHTTPJourneyAndIsolation | activation-doc-contract.txt; race-final.txt; integration-delivery.txt |
| `monitor-activation` / Activation transport and response projections are bounded and private | Security - CSRF and fixed failure behavior | TestActivationTransport; TestActivationTransportEdgesAndDeadline; TestActivationErrorsAndCancellation; TestActivationOpenAPI; TestMonitorHTTPJourneyAndIsolation | activation-doc-contract.txt; race-final.txt; integration-delivery.txt |
| `monitor-activation` / Activation transport and response projections are bounded and private | Success - Exact status representation | TestActivationTransport; TestActivationTransportEdgesAndDeadline; TestActivationErrorsAndCancellation; TestActivationOpenAPI; TestMonitorHTTPJourneyAndIsolation | activation-doc-contract.txt; race-final.txt; integration-delivery.txt |
| `monitor-activation` / Intent quotas and enable cooling are shared and durable | Concurrency - Last available intent and shared cooldown | TestActivationQuotaAndCAS; TestActivationStatePermissionsAndInvalidation | activation-final-http.txt; config-network-delivery/real.test.txt |
| `monitor-activation` / Intent quotas and enable cooling are shared and durable | Security - Pause and configuration changes remain available | TestActivationQuotaAndCAS; TestActivationStatePermissionsAndInvalidation | activation-final-http.txt; config-network-delivery/real.test.txt |
| `monitor-activation` / Intent ownership survives its author while audit remains bounded | Success - Another Owner retains control | TestActivationWorkspaceIntentLifetime; TestActivationStatePermissionsAndInvalidation; TestActivationTriggerAtomicFailureAndBoundedAudit | activation-final-http.txt; integration-delivery.txt |
| `monitor-activation` / Intent ownership survives its author while audit remains bounded | Security - No active Owner never causes execution | TestActivationWorkspaceIntentLifetime; TestActivationStatePermissionsAndInvalidation; TestActivationTriggerAtomicFailureAndBoundedAudit | activation-final-http.txt; integration-delivery.txt |
| `monitor-activation` / Intent ownership survives its author while audit remains bounded | Success - Last-state audit has a fixed row bound | TestActivationWorkspaceIntentLifetime; TestActivationStatePermissionsAndInvalidation; TestActivationTriggerAtomicFailureAndBoundedAudit | activation-final-http.txt; integration-delivery.txt |
| `monitor-scheduling` / Dormant scheduling state never creates runnable work | Security - Large intent populations and restarts | TestDormantProductionDependencyAndSchemaBoundary; TestActivationMigration; TestMonitorNoTargetNetwork | integration-delivery.txt; config-network-delivery/real.audit.txt |
| `monitor-scheduling` / Dormant scheduling state never creates runnable work | Exception - Unknown intent commit does not imply a probe | TestDormantProductionDependencyAndSchemaBoundary; TestActivationMigration; TestMonitorNoTargetNetwork | integration-delivery.txt; config-network-delivery/real.audit.txt |
| `monitor-scheduling` / Production execution requires a separately reviewed deployment boundary | Security - Missing or unverifiable production isolation | TestDormantProductionDependencyAndSchemaBoundary; TestMonitorNoTargetNetwork; deployment remains unverified | integration-delivery.txt; config-network-delivery/real.audit.txt; README.md policy |
| `monitor-scheduling` / Production execution requires a separately reviewed deployment boundary | Security - Fixture success has limited scope | TestDormantProductionDependencyAndSchemaBoundary; TestMonitorNoTargetNetwork; deployment remains unverified | integration-delivery.txt; config-network-delivery/real.audit.txt; README.md policy |
| `monitor-scheduling` / Future worker introduction cannot activate stored intent automatically | Security - Later deployment with old enabled intent | TestDormantProductionDependencyAndSchemaBoundary; TestActivationOpenAPI; future-worker policy documented, no Worker delivered | activation-doc-contract.txt; README.md and design §6/11 |
| `monitor-scheduling` / Acceptance separates planning database network and deployment evidence | Concurrency - Real database proof for dormant activation | Go normal/race PG18 suite; TestMonitorNoTargetNetwork; TestSystemSnapshotPGLiveIsolatedExecution | integration-delivery.txt; integration-race.txt; config-network-delivery/; snapshot-pg-race.txt |
| `monitor-scheduling` / Acceptance separates planning database network and deployment evidence | Security - Independent zero-network and kernel regression | Go normal/race PG18 suite; TestMonitorNoTargetNetwork; TestSystemSnapshotPGLiveIsolatedExecution | integration-delivery.txt; integration-race.txt; config-network-delivery/; snapshot-pg-race.txt |
| `monitor-scheduling` / Acceptance separates planning database network and deployment evidence | Exception - Missing infrastructure and planning-only status | Go normal/race PG18 suite; TestMonitorNoTargetNetwork; TestSystemSnapshotPGLiveIsolatedExecution | integration-delivery.txt; integration-race.txt; config-network-delivery/; snapshot-pg-race.txt |


## Review approval, merge and archive — 2026-10-20

The user completed review and authorized merge/archive (message: “审核完成，批注合并与归档”). This later authorization supersedes the earlier Open-PR/no-merge/no-archive delivery status above. Deployment and production execution remain outside the authorization.

- PR #10 is merged: https://github.com/rjgczp/devpilot/pull/10 . GitHub reports merged at 2026-10-20T01:50:55Z, merge commit `85402ab7877e67062e2d48b24e9ad9c31b721461`, reviewed head `deb2b7d07cc60c03d0c7a64a3323f32b3e5e2844`. The signed merge object was imported locally with its exact SHA; main was fast-forwarded without reset.
- The reviewed head's push run 39353677000 and PR run 39353682768 both completed successfully. Merge-main run [39355689230](https://github.com/rjgczp/devpilot/actions/runs/39355689230) also passed verify, integration and execution-security at the exact merge SHA. Raw job metadata is retained in `evidence/merge-ci.json`.
- All 23/23 tasks and four planning artifacts are complete. All five delta specs were synchronized: 13 added and 10 modified requirements, with all 66 delta scenarios compared against main specs. Existing titles/Purpose, unmentioned requirements and 12 unrelated specs were preserved. New monitor-activation and monitor-scheduling Purpose sections were copied verbatim.
- Change strict validation passed without issues before the move; normal main-spec validation passed 17/17. The five affected main specs pass strict. Global strict retains only seven known description-length warnings in four unrelated historical specs; no warning was suppressed or unrelated contract rewritten.
- Archived as `openspec/changes/archive/2026-10-20-add-monitor-activation-scheduler/`. All 404 original tracked Change files were SHA256-checked byte-for-byte before and after the move, including hidden metadata and binary CI artifacts. This verification update is append-only; original receipts and failures retain their historical meaning. `evidence/archive-receipt.json` records original hashes, synced spec hashes and preservation checks. The implementation source manifest still matches every source file.
- The archive commit only moves retained artifacts, synchronizes main specs and adds this receipt. Its exact published SHA and hosted CI outcome will be checked after publication and reported in the final delivery; this paragraph does not claim the merge CI tested that later commit.
- No deployment, production database migration, actual scheduler/Worker, public target probe or production monitor activation was performed. Execution remains blocked/scheduler_unavailable, next_due_at=NULL.

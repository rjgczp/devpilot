# Requirement → Test → Evidence

下列15项新执行要求及1项persistence修改均对应可复验断言；不同证据种类不互相替代。测试源位于`internal/monitoring/`，真实网络case由`tests/execution/run.py`逐项运行。完整场景仍以delta specs为准。

| Requirement | Test / assertion | Evidence |
| --- | --- | --- |
| Single execution is internal and never activates configuration | TestDormantProductionDependencyAndSchemaBoundary；原配置包network守护与启动/路由白名单 | foundation/startup/unit/race；config-network；Migration/API原哈希 |
| System snapshots bind complete scope and configuration version | TestSystemSnapshotPGScopePrivilegesAndRelease、StatementConsistency；TestImmutableSnapshotResultContract | snapshot-pg-final/race；pg-final/race；core-final-race |
| Every permitted target connection validates bounded complete DNS observations | TestDNSFamilyProtocolMatrix；mixed/mixed6/additional/reserved-alias/cname/inconsistent/ipv6/rebinding/repeat | core-unit；network-final-code各case及events |
| DNS failure and message resource limits fail closed | TestDNSBudgetLimits、FuzzDNSWire；dns-*、aliases/addresses/records/oversize/total/dname及边界case | dns-fuzz；network-final-code，目标attempt0 |
| Target policy rejects special use and translation addresses | TestPinnedRegistryPolicy、FuzzAddressPolicy；blocked-*、nat-app及绕过控制 | ip-fuzz；registry/manifest.json；network-final-code |
| Actual dialing is pinned to one approved literal destination | TestPlaintextBudgetsBeforeParsing的dialPermit/pinConnection；rebinding/repeat/refuse | core-final-race；真实DNS/trace/fixture计数；wrapper只列unit |
| HTTPS verifies the original hostname on the pinned connection | tls/cname-tls/expired/untrusted/wrong-name/tls-big/tls-stall/tls-stage | network-final-code；SNI events、固定分类、连接trace |
| HTTP execution has no ambient credentials proxies redirects or reuse | redirect-301/302/307/308、repeat、upgrade；每case代理投毒/凭据断言 | network-final-code；proxy0、Location DNS0、原Host/GET |
| Status checking never reads response bodies and bounds protocol input | TestPlaintextBudgetsBeforeParsing；header-big/hints/flood/flood-lf/hints-big/upgrade/malformed/infinite/chunked/gzip/status | core-final-race；network-final-code |
| Probe deadlines share the saved configuration timeout | dns-timeout/slow/tls-stall/tcp-timeout/cancel/T30/dns-stage/dns-chain-slow/tcp-stage/tls-stage | per-case durations/results；network-final-code |
| Admission enforces bounded tenant consumption without queues | TestAdmissionAtomicLimitsRetentionAndCapacity、RaceReleaseAndRates、MultipleKernelInstancesShareProcessAdmission；capacity/concurrency | race/core-final-race；network-final-code；resources.json |
| Results are immutable private observations of one configuration version | TestImmutableSnapshotResultContract；TestSystemSnapshotPGLiveIsolatedExecution；AuthorityIsNotCookieAuthority | pg-final/race的v7/v8输出/审计；snapshot-pg-final/race |
| Failure classification and diagnostic output are safe and deterministic | TestSafePreflightAndClassification、ImmutableSnapshotResultContract；真实各错误/secret-sentinel正常输出排除 | core-final-race；全部网络case；无日志/metrics生产入口 |
| Egress isolation is a separate mandatory security boundary | bypass-v4/v6/nat/public、允许HTTP/IPv6正控制、Go UID/caps/FD检查、run.py路由/防火墙/cgroup断言 | network-final-code topology/listeners/firewall/resources；child0/audit1四控制 |
| Acceptance reports unit network database and egress evidence separately | verification.md分栏；每条record.py命令/exit；原配置零网络脚本不改 | unit/race/vet/fuzz、network、PG、firewall各自文件；CI后续绑定SHA |
| Future probing requires a separately verified SSRF execution boundary（MODIFIED） | 以上执行器安全矩阵＋原配置API无目标网络、Migration1–6/API不变 | config-network；baseline/source-tree；最终CI的三个job分别验收 |

所有本地命令的实际退出码在同名JSON；较早network/network2/network-final等完整目录无损保存在`evidence/prior-network-runs.tar.gz`并有逐文件哈希清单。最终套件的非关键65份trace在`network-final-code/additional-traces.tar.gz`，其余case JSON/输出与关键trace可直接读取；解包后恢复原路径。托管CI的run/head SHA与最新PR状态由verification交付部分追加，不能以本文索引宣称尚未运行的CI通过。

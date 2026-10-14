# Verification

2026-10-14（Asia/Shanghai），在最终实现源码上实际执行。基线 main 为14922d2；本Change不自动迁移、不修改健康检查语义、不扩展登录/Workspace/Redis/AI能力。规划最后两项修订先通过严格校验，再按用户授权实施。

## 环境与依赖

- Go 1.24.3，Darwin arm64，Apple M4，24 GiB物理内存。
- 专用Docker PostgreSQL **18.6**，原生Goose **v3.26.0**；x/crypto **v0.48.0**，未升级Go工具链。相邻版本的Go基线核对见迁移文档。
- PostgreSQL多架构index：`postgres@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336`。已实际检查包含linux/arm64及linux/amd64，CI固定该digest。
- 使用独立容器devpilot-registration-test、127.0.0.1:55432、专用devpilot_registration_test库，不读取开发.env，不修改开发数据卷。

## 实际执行记录

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| `go test ./... -count=1 -timeout=90s` | 全部通过 | [unit.txt](evidence/unit.txt) |
| `go vet ./...` | exit 0，无诊断 | 与最终unit/race检查同一执行记录，所有状态均为0 |
| `go test -race ./... -count=1 -timeout=120s` | 全部通过，无竞态报告 | [race.txt](evidence/race.txt) |
| `go test -tags=integration ./... -v -count=1 -timeout=120s` | 真实PG18全部通过 | [integration.txt](evidence/integration.txt) |
| `go test -race -tags=integration ./tests/integration -count=1 -timeout=120s` | 真实PG18并发/取消检查通过 | [integration-race.txt](evidence/integration-race.txt) |
| 缺TEST_DATABASE_URL运行集成suite | 预期非零退出，无skip | [missing-db.txt](evidence/missing-db.txt) |
| `go test ./internal/registration -run='^$' -fuzz=FuzzParseInput -fuzztime=5s -parallel=2` | 80,462次执行，无崩溃 | [fuzz.txt](evidence/fuzz.txt) |
| `go mod verify` | all modules verified | 依赖下载/完整性检查实际执行通过 |
| `gofmt -l cmd internal tests` / `git diff --check` | 空输出 | 最终提交前检查通过 |
| `openspec validate add-user-registration --strict` | 通过 | 最后修订前置及完成检查均实际执行 |
| 独立API进程：无schema启动、DB暂停/恢复、SIGTERM | 全部通过 | [process-smoke.txt](evidence/process-smoke.txt)、[process-log.txt](evidence/process-log.txt) |

原生Goose在真实库验证up/up、up/down/up、坏SQL的事务回滚及版本未误记；所有schema变更/Down仅在隔离测试库执行。statement_timeout使用真实PG18的10ms期限与pg_sleep夹具，另以表锁触发注册的数据库子期限，区分连接错误/执行超时/客户端取消。未知57014不误判超时。

早期并发测试夹具曾在两个hash都启动之前取消其中一个请求，导致等待条件超时；修正为先等待两个hash进入，再同时取消。之后最终普通、race和集成测试均通过。此修复没有放宽槽位或取消契约。

## 哈希资源基线

参数保持Argon2id v19、m=65536 KiB/t=3/p=1，16-byte随机盐和32-byte输出。其他测试结束后，使用独立非Race进程测量，未为性能降低安全参数：

```sh
go test -c -o /tmp/devpilot-password.test ./internal/password
/usr/bin/time -l /tmp/devpilot-password.test -test.run='^$' -test.bench='^BenchmarkHashResources/1$' -test.benchtime=20x -test.benchmem
/usr/bin/time -l /tmp/devpilot-password.test -test.run='^$' -test.bench='^BenchmarkHashResources/2$' -test.benchtime=20x -test.benchmem
```

| 并发 | 最终批次数/样本数 | 单次hash p50 / p95 | 每批分配 | 进程峰值RSS |
| --- | --- | --- | --- | --- |
| 1 | 20 / 20 | 96.58 / 99.66 ms | 67,114,420 B；51 allocs | 141,590,528 B，约135 MiB |
| 2 | 20 / 40 | 107.6 / 112.9 ms | 134,228,936 B；101 allocs | 343,523,328 B，约328 MiB |

原始输出：[hash-1.txt](evidence/hash-1.txt)、[hash-2.txt](evidence/hash-2.txt)、[resources-1.txt](evidence/resources-1.txt)、[resources-2.txt](evidence/resources-2.txt)。Go基准含少量预热执行，表中样本指最终20批次。RSS包括运行时/GC及历史未回收缓冲区，不能据128 MiB同时工作内存宣称进程只有128 MiB。

实测支持当前5s处理/2s数据库控制预算。依据本机基线，后续同机性能回归参考为hash p95<250ms、基准进程峰值RSS<512MiB；它们是保留余量的本机观测目标，不是生产SLO或内存硬上限，部署环境必须用相同安全参数重测。超预算先评估容量/预算；任何降参必须另行审核。Argon计算不可被context强制中断，客户端取消不提前释放槽位，GC回收不保证立即发生。

## 场景与证据映射

表中每个Spec Requirement下的成功/异常/安全场景映射到对应测试或明确的文档/操作评审；未来邮箱信任消费者和公开限流不以不存在的实现冒充已测试。

| Requirement / 重点场景 | 验证位置 |
| --- | --- |
| Strict registration request：结构/键/媒体/大小限制、无效原始Unicode转义、合法代理对/U+FFFD | TestStrictRequestsAndCheapRejection、TestRepresentationAndMethods、TestUnicodeRawEscapes、FuzzParseInput |
| Predictable email and password validation：邮箱子集/归一化、15/128码点及512bytes边界、密码原字节 | TestValidationBoundaries、TestUnicodeRawEscapes、TestRegistrationWithPostgreSQL；README兼容性说明 |
| Password storage resists offline guessing：独立随机盐、PHC完整性/参数、随机源失败、脱敏 | TestPHCAndIndependentSalts、TestRandomFailure、TestErrorResponsesAndSlotRelease、真实PG注册hash重算 |
| Successful registration creates an unverified account：201投影、active/null、字段注入、未知结果、未来信任语义 | TestOpenAPIContract、TestStrictRequestsAndCheapRejection、TestServerDeadlineAndUnknownCommit、TestRegistrationWithPostgreSQL；README/OpenAPI明确active不证明邮箱归属，未来消费者约束由后续Change验收 |
| Duplicate registration preserves existing accounts：大小写/空白、并发201/409、不改旧密码/disabled/验证/时间 | TestRegistrationWithPostgreSQL |
| Bounded registration processing：非法请求不占槽、2槽耗尽、中途取消仍占槽、真实返回后复用、server deadline | TestStrictRequestsAndCheapRejection、TestCancellationKeepsSlotsUntilHashReturns、TestServerDeadlineAndUnknownCommit、TestErrorResponsesAndSlotRelease、BenchmarkHashResources |
| Hash resource limits do not establish abuse protection：资源实测、禁止静默降参、公开部署前置条件 | 本页原始benchmark证据、README/Design发布门槛；仅验收前置要求已记录，不宣称限流已实现 |
| Stable safe registration responses：固定码/头、连接503、缺表500、DB超时503、客户端取消不写503、原始错误不透出 | TestErrorClassification、TestErrorResponsesAndSlotRelease、TestClientCancellationInStoreAndRealHTTP、TestServerDeadlineAndUnknownCommit、真实PG相应子用例 |
| Registration contract has executable verification：API文档一致、缺测试依赖失败、开发库保护 | TestOpenAPIContract、TestDatabaseGuard、TestUnavailableDatabaseIsFailure、missing-db.txt、CI独立PG18 job |
| Complete user records：UUIDv7/时间/未验证状态、非空/长度/状态约束、PHC不绑参数 | TestMigrationAndConstraints、TestRegistrationWithPostgreSQL；PHC格式测试在password包 |
| Canonical email uniqueness is enforced by PostgreSQL：直写拒绝、UNIQUE竞争、参数化SQL | TestMigrationAndConstraints的全部非法直写/SQL样式邮箱用例、TestRegistrationWithPostgreSQL并发用例 |
| Registration writes are atomic and non-destructive：具名唯一分类、非邮箱错误、取消/重试不覆盖 | TestErrorClassification、TestServerDeadlineAndUnknownCommit、TestRegistrationWithPostgreSQL/client-cancel-during-insert |
| Versioned explicit user schema migration：原生Up重复/失败回滚、无自动DDL、健康不改、诊断受限 | TestMigrationAndConstraints、process-smoke.txt；迁移文档中的串行发布/受限诊断为操作规约，不宣称CLI自带锁 |
| Destructive migration rollback is operationally restricted：disposable回退、共享/生产限制、例外恢复 | 隔离库真实up/down/up；docs/database-migrations.md明确日常仅status/up、例外授权/备份/恢复评审；不是CLI自动环境识别 |

## 审查与部署边界

本地验证完成不代表独立代码审查已通过。交付PR等待独立审查，不自动合并或归档；主Specs保留归档/同步流程原有边界。公开部署前仍必须另行实现并评审注册限流与防滥用控制，两个hash槽不能替代这一要求。409保留已评审的邮箱占用枚举取舍。此轮未执行共享/生产迁移或部署。

## 归档记录

2026-10-14，用户确认独立审查结束并授权合并与归档。PR #2已在GitHub合并，verify/integration的push与pull_request检查均成功，本地main已同步至合并提交1ac1eab。21/21实施任务完成；两份delta的Purpose、14项Requirement与64个Scenario已逐字同步为主Specs，原有runtime/health Specs保持不变。归档前所有4份主Specs严格校验通过，所有原始验证证据随Change完整保留。此次归档仅调整规格/归档路径及README证据链接，不修改业务代码，也不执行数据库迁移。

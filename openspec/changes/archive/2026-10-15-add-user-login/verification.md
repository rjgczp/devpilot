# Verification

2026-10-14（Asia/Shanghai）。在用户明确批准Apply及本轮清理Worker后实施；基线main为 `a38472dabd8e06e6f9a030e759fa5d5dfb652edc`，开发分支 `codex/add-user-login`。本记录区分实际检查与未来发布前置条件；不合并、不归档、不部署共享/生产环境。

## 环境与数据边界

- Go **1.24.3**，Darwin arm64，**Apple M4 / Mac16,12 / 24 GiB**；详细值见 [environment.json](evidence/environment.json)。
- 独立容器 `devpilot-login-test`，仅 `127.0.0.1:55432`，不挂载开发数据卷；专用 `devpilot_registration_test` 库沿用现有隔离保护。
- 实际 PostgreSQL **18.6**（Debian 18.6-1.pgdg13+2，aarch64），与CI相同的固定PG18 digest；原生Goose **v3.26.0**，x/crypto **v0.48.0**，未升级工具链或添加认证依赖。
- 不读取开发 `.env`，不回退应用DSN。测试核对当前库名和PG18版本；所有migration down/down-to、schema重命名、行删除、锁与终止连接夹具均只用于该可丢弃库。日常共享/生产仅串行status/up。
- 版本1迁移未修改，SHA-256保持 `e09f6476b25ce0a60cbd6607486a9b4208f2c3413957c83e54110e0e661b5490`；只追加版本2。API启动没有Goose、DDL或自动迁移调用。

## 实际检查结果

模块交付时先运行密码/注册、Token/配置、真实Session仓储、认证/Worker、真实HTTP/TLS与清理测试，再执行以下最终验收。以下证据来自实际命令输出，不以规划checkbox代替执行。

| 检查 | 实际结果 | 证据 |
| --- | --- | --- |
| `gofmt -l cmd internal tests` / `git diff --check` | 无输出 | [format-and-modules.txt](evidence/format-and-modules.txt) |
| `go vet ./...` | exit 0，无诊断 | [vet.txt](evidence/vet.txt) |
| `go mod verify` | all modules verified | [format-and-modules.txt](evidence/format-and-modules.txt) |
| `go test ./... -count=1 -timeout=120s` | 全部通过 | [unit.txt](evidence/unit.txt) |
| `go test -race ./... -count=1 -timeout=120s` | 全部通过，无data race | [race.txt](evidence/race.txt) |
| `go test -tags=integration ./... -v -count=1 -timeout=120s` | PG18全套通过，integration包13.141s | [integration.txt](evidence/integration.txt) |
| `go test -race -tags=integration ./tests/integration -v -count=1 -timeout=120s` | PG18并发/取消检查通过，40.561s | [integration-race.txt](evidence/integration-race.txt) |
| PHC解析fuzz，5s / parallel=2 | 727,481次执行，通过 | [fuzz-phc.txt](evidence/fuzz-phc.txt) |
| 严格JSON解析fuzz，5s / parallel=2 | 105,903次执行，通过 | [fuzz-json.txt](evidence/fuzz-json.txt) |
| 缺TEST_DATABASE_URL显式集成 | 预期非零失败，无skip，无DDL | [missing-db.txt](evidence/missing-db.txt) |
| 缺固定Goose显式集成 | 预期非零失败，迁移前停止 | [missing-goose.txt](evidence/missing-goose.txt) |
| PG版本/库名边界 | `TestServerIdentityAndVersionGuard`覆盖17/18/19边界及错误库名；真实库确认18.6 | 不冒充实际启动了PG17/19服务器 |
| `openspec validate add-user-login --strict` | 通过 | [openspec.txt](evidence/openspec.txt) |

开发阶段Dummy PHC自检发现预计算盐有17 bytes，已重算为规范16 bytes并通过真实Dummy/Hash/Verify与最终检查；未降低参数。沙箱阻止回环端口和time的sysctl统计时，采用获授权的执行环境完成检查，没有跳过测试或伪造RSS。

## 真实Argon2资源测量

参数始终为Argon2id v19、m=65536 KiB、t=3、p=1，16-byte盐、32-byte输出。编译独立非Race密码测试二进制，以混合Hash/Verify固定并发1/2运行，不以Race开销代表生产预算：

```sh
go test -c -o /tmp/devpilot-mixed-password.test ./internal/password
/usr/bin/time -l /tmp/devpilot-mixed-password.test -test.run='^$' -test.bench='^BenchmarkMixedResources/1$' -test.benchtime=20x -test.benchmem
/usr/bin/time -l /tmp/devpilot-mixed-password.test -test.run='^$' -test.bench='^BenchmarkMixedResources/2$' -test.benchtime=20x -test.benchmem
```

| 并发 | 最终批次/样本 | 单次计算p50 / p95 | 每批分配 / allocs | 进程最大RSS |
| --- | --- | --- | --- | --- |
| 1 | 20 / 20 | 103.1 / 143.2 ms | 67,114,770 B / 53 | 142,163,968 B，135.6 MiB |
| 2 | 20 / 40 | 121.1 / 143.6 ms | 134,228,803 B / 102 | 277,217,280 B，264.4 MiB |

原始输出：[mixed-1.txt](evidence/mixed-1.txt)、[mixed-2.txt](evidence/mixed-2.txt)。Go基准含校准/预热，样本列为最终20批；RSS包含运行时、GC及历史缓冲，不是128MiB硬上限。混合负载不同于历史纯Hash基线，不能据这些样本宣称网络恒时或普遍性能提升。结果在历史本机p95<250ms/RSS<512MiB参考范围内；5s处理/2s数据库预算在本机测试成立，部署机器须重测，未宣称生产SLO或限流完成。

## Requirement 与可执行证据映射

各Requirement的成功/异常/安全/并发场景通过以下测试及明确的文档评审覆盖；未来Workspace/公开防滥用/共享生产迁移规约只作为文档与后续设计边界验收，不声称本轮实现这些产品能力。

| Capability / Requirement | 证据 |
| --- | --- |
| user-authentication / Strict login request | TestLoginStrictInputAndCheapRejection、TestCSRFProofAndMethods、FuzzParseInput；原始Unicode/重复键/4096边界及非法请求不lookup/计算 |
| Login field policy is independent of account existence | 同上、TestValidationBoundaries、TestUniformCredentialFailuresAndDummyIsolation；共享ASCII政策，登录1码点/注册15码点及精确字节 |
| Credential failures share one contract and bounded cost | TestUniformCredentialFailuresAndDummyIsolation、TestAuthenticationWithPostgreSQL；真实不同PHC与dummy选择，dummy匹配仍401，disabled仍计算，统一body/Cookie不变 |
| Password verification rejects unsafe PHC before computation | TestVerifyAndApprovedPolicy、TestDummyPHC、FuzzPHC、TestPHCAndIndependentSalts；零/溢出/过参/编码/长度在IDKey之前拒绝，固定长度constant-time派生key比较 |
| Confirmed login creates an independent session | TestSessionStoreAndUpgrade、TestSessionCreationRechecksConcurrentUpdate、TestCreationLockSerializesDisableAndConcurrentRevocation、真实HTTP/TLS；短事务FOR SHARE两种锁顺序、凭据变化、独立token、RNG/commit fault不发Cookie/重试 |
| Login resource work is bounded across endpoints | TestMixedBudgetCancellationAndFaultRecovery、TestWorkflowBoundAndTimeouts、TestCancellationKeepsSlotsUntilHashReturns、混合资源基准；实际两个handler同时进入受控计算、取消仍占槽、无第三调用、创建等待不占Argon槽 |
| Current identity depends on live database state | TestCookieIdentityAndLogoutContracts、TestAuthenticationWithPostgreSQL、TestSessionStoreAndUpgrade；每次DB读取/24h不续期/retained expiry/revoke/disabled统一401、无Set-Cookie |
| Logout is protected and idempotent | TestCookieIdentityAndLogoutContracts、TestDependencyFailurePrivacyAndUnknownCommit、TestAuthenticationWithPostgreSQL、并发Revoke PG测试；204、删除scope、DB失败保留Cookie、旧token重放401、其他session保留 |
| Authentication responses distinguish dependency failures safely | TestDependencyFailurePrivacyAndUnknownCommit、TestClientCancellationPropagatesWithoutResponse、TestPanicAfterResponseStartsAbortsConnection、TestAuthenticationDatabaseFailuresAndCancellation、TestErrorClassification；500/503不伪装401，HEAD/Allow、未知57014、schema、锁超时、真实socket取消与恢复、日志脱敏 |
| Public authentication requires separate abuse controls | README发布门槛与Design第7/11节；仅文档/审核要求完成，来源/账户/跨实例限流和防撞库实现仍未交付 |
| Authentication has executable verification | TestOpenAPIAuthenticationContract、隔离库guard、缺DB/Goose预期失败、全量unit/race/PG/integration-race、CI配置；schema套件顺序执行 |
| Authentication does not grant transaction-level Workspace authority | README/OpenAPI/Design明确查询时点身份与未来资源/动作事务协议，普通SELECT与middleware不提供全程原子授权；源码仅users/sessions，未建立Workspace/RBAC或伪授权测试 |
| session-management / Session secrets have high entropy and digest-only storage | TestTokenCanonicalAndDigest、TestSessionStoreAndUpgrade、真实HTTP/TLS；32-byte CSPRNG/43字符、SHA-256(decoded bytes)、重复/坏Cookie及替代传输不认证、随机失败、完整摘要唯一与参数化SQL探针 |
| Sessions have a constrained additive database schema | TestMigrationAndConstraints、TestSessionStoreAndUpgrade；版本1旧用户保留升级、up/up、session单步down保留users、完整disposable往返、坏版本3事务回滚、NOT NULL/长度/FK/UNIQUE/时间约束/级联删除 |
| Database time determines session validity | PG保留expiry/revoke行无Worker仍拒绝、默认差值24h、active JOIN和不续期；直接SQL解禁恢复未撤销会话限制文档化 |
| Revocation has explicit concurrency semantics | TestCreationLockSerializesDisableAndConcurrentRevocation、TestAuthenticationWithPostgreSQL；并发幂等UPDATE、commit后新查询拒绝，旧身份快照不是长期授权租约 |
| Browser session Cookie has fixed security attributes | TestCookieIdentityAndLogoutContracts、TestAuthenticationWithPostgreSQL；production TLS/loopback HTTP，name/Path/Domain/Secure/HttpOnly/Lax/删除/JSON脱敏，me不覆盖新Cookie |
| Authentication configuration fails closed before listening | TestAuthDefaultsLocalAndInvalidPolicies、TestOriginCompleteTuple、TestProcessRejectsAuthenticationPolicyBeforeListening；默认HTTPS、显式loopback例外、缺/错配置、非法origin/TTL、变量安全诊断；完整原进程/SIGINT/SIGTERM回归 |
| Login and logout require explicit same-origin CSRF proof | TestCSRFProofAndMethods、PG forged login/logout；缺/null/重复/恶意suffix/同站不同源/Referer-only/Fetch Metadata/自定义头/OPTIONS，无lookup/Argon/DB/Cookie副作用 |
| Cleanup is bounded and independent of validity | TestCleanupBatchMultiInstanceAndFaults、TestWorkerBudgetCancellationNoOverlapOrImmediateRetry、TestWorkerDatabaseDeadline、TestCleanupWorkerDrainsBeforeResourcesClose；两个执行各1000、跳过锁候选、保留valid、失败/缺表/取消/期限、无立即重试、Runner取消后先退出再关资源 |
| user-registration / Bounded registration processing | 原注册取消/容量/期限/真实HTTP断连测试全部保留适配统一Budget，混合真实handler计数及不同busy code，health独立 |
| Shared password budget preserves registration isolation | TestMixedBudgetCancellationAndFaultRecovery、TestAuthenticationDatabaseFailuresAndCancellation、原TestRegistrationWithPostgreSQL/UNIQUE/旧凭据/原始Unicode/全状态OpenAPI测试；登录专属fault不泄漏槽/污染注册，session缺表注册仍成功 |

## CI 与交付边界

CI保持固定PG18 digest/Goose版本，新增登录分支push，gofmt覆盖cmd/internal/tests；verify执行vet/unit/race，integration执行真实PG suite后顺序integration-race。本文件创建时托管CI尚未开始，PR与最终提交的实际CI状态在交付时单独核对，不把本地通过冒充托管通过。

已实现部分没有已知阻塞未解决项。以下限制按已批准设计保留：注册409仍可枚举；24h为绝对期限无idle/refresh；重新登录不自动撤销旧会话；入口认证/已在途请求仅具有快照语义；直接SQL解禁可能恢复未撤销会话；清理固定吞吐不提供审计保留或无限容量保证；两个槽不承诺公平调度、全局限流或共同DB故障隔离。

公开部署前**必须另行实现并评审**登录限流、防暴力破解/撞库及注册反滥用，并审查HTTPS入口、时间/容量预算和清理负载；这些控制没有被标记为本轮已实现。PR等待用户最终代码审查，不自动合并，不同步/归档主Specs，不公开部署。

## 2026-10-15 代码审查：Cookie 令牌严格格式

修正任务2.2的解析边界：只移除Cookie pair前用于分隔的ASCII空格/制表符，不再修剪pair尾部或`=`后的原始值。非规范值交给现有43字符规范Base64URL校验拒绝；重复同名Cookie仍拒绝。这里校验的是应用收到的Header值，无法恢复HTTP传输层已经移除的字段外围空白。

新增 `TestCookieDigestPreservesTokenBytes`，17个场景覆盖分隔符空格/制表符、多Header、值前后/内部空白、分号前空白、Unicode空白及含非规范值的重复Cookie。先在原实现运行，8个拒绝场景如预期失败；修复后全部通过，有效令牌的摘要保持相同。

本次实际执行：

- `go test ./internal/session -run '^Test(TokenCanonicalAndDigest|CookieDigestPreservesTokenBytes)$' -count=1`：通过。
- `go test ./internal/session ./internal/auth -count=1 -timeout=120s`：通过（0.239s / 0.483s）。
- `go test -race ./internal/session ./internal/auth -count=1 -timeout=120s`：通过，无data race（1.502s / 1.514s）。
- `go vet ./internal/session ./internal/auth`、`gofmt`及`git diff --check`：通过。
- `openspec validate add-user-login --strict`：通过；原27项任务保持完成。本次本地未重跑PG18或Argon资源测量，原始证据保留，托管CI按更新后的提交重新运行完整验收。

## 2026-10-15 审查批准、合并与归档

用户最终代码审查通过并明确批准合并与归档。[PR #3](https://github.com/rjgczp/devpilot/pull/3) 已于2026-10-15合并至main，合并提交 `9e55635129fdbcb314458d6c8d632463d92b5902`；最终实现提交为 `b39fb3ec96b1c76905acd583c02808a2763332ef`。该提交的 [PR CI](https://github.com/rjgczp/devpilot/actions/runs/38668544223) 和 [push CI](https://github.com/rjgczp/devpilot/actions/runs/38668539793) 均成功，包含verify、真实PostgreSQL 18 integration及integration-race。

归档检查确认27/27任务完成、全部规划artifact完成。主规格新增user-authentication的12项Requirement、session-management的8项Requirement，user-registration修改1项并新增1项；逐块比对确认delta全部同步，既有Purpose及未涉及要求保留，`openspec validate --specs --strict`通过（6/6）。Change及原始验收证据保留于 `openspec/changes/archive/2026-10-15-add-user-login/`，README验收链接同步更新。原文中的“等待审查/不归档”描述为当时交付边界，本节记录其后的明确批准；公开部署前置条件仍未交付，本次未部署。

# Verification

2026-10-15（Asia/Shanghai）。用户确认六份规划产物通过独立技术评审并明确批准Apply、创建PR后实施。基线 `main=origin/main=99f3f516db284ef31b70fa9131d3653dd61cd40a`，开发分支 `codex/add-workspace-foundation`。本记录区分实际执行、受控故障注入和设计边界；不自动合并、归档或公开部署。

## 环境与数据保护

- Go 1.24.3，Darwin arm64；pgx/v5 v5.8.0，Goose v3.26.0；未改go.mod/go.sum或升级框架/依赖。实际服务器为 PostgreSQL **18.6**（Debian 18.6-1.pgdg13+2，aarch64），详情见 [environment.json](evidence/environment.json)。
- 独立可丢弃容器 `devpilot-workspace-test`，固定与CI相同PG18镜像digest，仅发布回环55432，无开发数据卷；数据库仅为既有允许名 `devpilot_registration_test`。未读取开发.env，不回退应用DSN；所有DDL/Down/删除/账户状态/成员变更仅在受保护测试库。
- 普通集成与integration-race串行访问该库。负向缺配置/缺Goose测试在迁移前硬失败；库名和PG17/18/19边界由既有testdb单元测试验证，实际运行的服务器仅PG18，不冒充启动了PG17/19。
- users迁移SHA-256仍为 `e09f6476b25ce0a60cbd6607486a9b4208f2c3413957c83e54110e0e661b5490`；sessions迁移仍为 `27ce132375459ea1e061aac1e2c00de1010c9dc8f94c21489e7c24243f02c96a`。版本1/2与主Specs均未修改，只有新增版本3。API启动不执行DDL或Goose。

## 实际验收

下列命令从仓库根执行，测试缓存显式使用临时目录；集成使用专用TEST_DATABASE_URL和固定Goose。证据仅含安全测试输出，无原始迁移SQL错误诊断或真实凭据。

| 检查 | 实际结果 | 证据 |
| --- | --- | --- |
| gofmt、git diff --check、go mod verify、旧迁移/主Specs差异检查 | 通过，格式无输出、模块完整、旧文件无差异 | [format-and-modules.txt](evidence/format-and-modules.txt) |
| `go vet ./...` | exit 0，无诊断 | [vet.txt](evidence/vet.txt) |
| `go test ./... -count=1 -timeout=120s` | 全部通过，含原有进程与优雅退出 | [unit.txt](evidence/unit.txt) |
| `go test -race ./... -count=1 -timeout=120s` | 全部通过，无race报告 | [race.txt](evidence/race.txt) |
| `go test -tags=integration ./... -v -count=1 -timeout=120s` | PG18完整套件通过 | [integration.txt](evidence/integration.txt) |
| `go test -race -tags=integration ./tests/integration -v -count=1 -timeout=120s` | PG18完整套件通过，无race报告 | [integration-race.txt](evidence/integration-race.txt) |
| 精确约束SQLSTATE的PG18定向race验收 | 通过；NULL/FK/CHECK/重复关系分别验证实际数据库类别 | [constraints-race.txt](evidence/constraints-race.txt) |
| Workspace原始JSON/Unicode解析fuzz，5s、parallel=2 | 143,795次执行，通过 | [fuzz-json.txt](evidence/fuzz-json.txt) |
| 未配置TEST_DATABASE_URL显式集成 | 预期非零失败，无skip、无DDL | [missing-db.txt](evidence/missing-db.txt) |
| 缺固定Goose显式集成 | 预期非零失败，在迁移前停止 | [missing-goose.txt](evidence/missing-goose.txt) |
| `openspec validate add-workspace-foundation --strict` | 通过 | [openspec.txt](evidence/openspec.txt) |

实施早期沙箱禁止回环端口及依赖下载，相关命令失败不算测试通过；已在允许回环端口的执行环境完成上述全量验收，未跳过用例或降低密码参数。一次vet指出测试context缺显式cancel收尾，已修正并重新通过。

首次integration-race中，旧注册取消测试的1秒窗口未观察到Argon2之后的INSERT锁等待而失败，见 [integration-race-first.txt](evidence/integration-race-first.txt)。将观察窗口对齐既有5秒处理预算、取消传播窗口对齐2秒DB预算，仍强制观察真实PG锁、取消HTTP并确认DB等待消失，未删除断言或改业务超时。之后完整integration-race通过。约束用例也加强为精确SQLSTATE断言，避免把其他参数/SQL错误误算为约束拒绝。

## Requirement 与实际用例映射

每个Requirement的Success/Exception/Security/Concurrency场景按以下实际用例验收；未来租户原则、RLS选择和发布限制属于设计/操作审阅，不冒充不存在的功能测试。

| Capability / Requirement | 实际验收 |
| --- | --- |
| persistence / Workspaces have database identities and bounded names | `TestWorkspaceMigrationAndConstraints`、`TestWorkspaceTenantQueriesAndConcurrentCreation`、`TestStrictInputAndNamePolicy`：UUIDv7、等初始时间、数据库名称边界、同名并发独立 |
| persistence / Membership represents a constrained many-to-many relationship | `TestWorkspaceMigrationAndConstraints`直接PG约束/用户RESTRICT/租户CASCADE；`TestWorkspaceTenantQueriesAndConcurrentCreation`多对多、局部role及重复pair竞态一成功一唯一冲突 |
| persistence / Workspace and initial owner commit atomically | `TestWorkspaceCreationLockOrdering`另一连接观察不到半创建；`TestWorkspaceTransactionFaultsAndUnknownCommit`真实PG第二写CHECK失败，两行均回滚；HTTP严格输入拒绝伪造owner |
| persistence / Creation coordinates with current identity state | `TestWorkspaceCreationLockOrdering`以pg_blocking_pids验证User→Session及create/change两种顺序；`TestWorkspaceExpiryAndCleanupBoundaries`验证锁后新语句时间、post-gate到期与cleanup跳锁 |
| persistence / Unknown creation outcomes are not retried automatically | `TestWorkspaceTransactionFaultsAndUnknownCommit`、`TestWorkspaceHTTPDatabaseFailuresAndRecovery`：提交后丢失结果/确认回滚两种实际存储状态、提交调用计数、503无Location、无自动重试；取消后无写入/悬挂事务 |
| persistence / Workspace migrations are additive explicit and recoverable | `TestWorkspaceMigrationAndConstraints`保留已有用户/session的2→3、up/up、v3 down保留旧数据及0→3；旧`TestMigrationAndConstraints`失败v4原子回滚、`TestSessionStoreAndUpgrade`独立v2语义；HTTP缺表500 |
| access / Workspace creation accepts only a bounded name | `TestStrictInputAndNamePolicy`、`TestRepresentationMethodsAndSelectors`、`FuzzWorkspaceNameJSON`：结构/重复/未知键、Unicode原始转义、4096含边界、chunked/尾随空白、媒体、并发请求隔离 |
| access / Display names have a deterministic Unicode policy | 上述单元测试及真实PG名称约束：ASCII裁剪、中文/补充字符/合法U+FFFD、组合字符不归一、Unicode边缘空白/Cc、参数化SQL样式名称、同名独立 |
| access / Workspace success responses have a minimal projection | `TestWorkspaceOpenAPIContract`把实际Handler响应按OpenAPI闭合字段/错误分支/时间/headers逐项核对；HTTP/TLS创建与成员详情验证role/Location，无Set-Cookie |
| access / Workspace lists are bounded and user scoped | `TestWorkspaceTenantQueriesAndConcurrentCreation`稳定分页无重漏；`TestWorkspaceListSnapshotAndCursorChanges`当前权限；`TestWorkspaceHTTPAuthenticationAndTenantIsolation`外租户/不存在cursor均只排序不授权 |
| access / Workspace detail is authorized without existence disclosure | `TestWorkspaceHTTPAuthenticationAndTenantIsolation`真实HTTP/TLS比对非成员/未知/非法UUID完整相同404；`TestWorkspaceReadSnapshotMembershipAndIdentityChanges`成员移除/role变化前后快照 |
| access / Workspace reads use live identity and tenant checks together | 上述PG测试覆盖未知/过期/撤销/禁用401、有效身份无成员200[]、无缓存；详情和列表各自验证资源快照之前的变化及早先合法快照可完成 |
| access / Workspace mutations reuse the same-origin CSRF protocol | `TestCSRFAndCookieReuse`来源/唯一头/Fetch Metadata/重复Cookie/环境协议、拒绝时零DB调用；真实HTTP的CSRF403、GET只读、OPTIONS405无CORS |
| access / Workspace responses and processing fail safely | `TestFailuresPrivacyCancellationAndBudgets`、`TestPanicAndConcurrentRequestIsolation`；PG真实锁超时503、连接不可达503、schema500、57014/意外约束500、同进程恢复、脱敏和取消不响应 |
| access / Tenant boundaries and acceptance remain verifiable | `TestWorkspaceHTTPAuthenticationAndTenantIsolation`register→login→三API与真实越权矩阵；必需依赖负向运行；全量CI配置保留PG18/Goose和schema串行；租户字段/复合关联原则在README/design审阅 |
| auth / Authentication does not grant transaction-level Workspace authority | 实际查询同时校验Session/User/membership，事务按User→Session再新INSERT时间门控；所有原auth/registration/runtime/cleanup单元/集成/race回归通过，未以入口UUID或普通事务SELECT作为权限保证 |

## 故障注入与保证范围

- 真实数据库约束失败：仅隔离测试临时增加不允许owner写入的CHECK，调用原始创建事务的第二条INSERT，数据库返回约束错误；恢复夹具后验证无Workspace或成员残留。产品migration中没有该测试约束、trigger或故障入口。
- 未知Commit：测试实现pgx的窄接口包装，底层执行真正PG事务Commit后将返回值替换为连接错误，另一路先实际Rollback再返回错误。行数据和提交次数由真实PG与计数验证；这是受控传输结果注入，**没有声称实际切断网络或证明所有网络故障模型**。生产Store不含故障开关。
- 并发屏障暂停真实SQL的Scan之后；保护行由真实事务持锁，以pg_blocking_pids观察等待，读取快照后变更实际提交再恢复响应。时钟边界轮询数据库时间，不靠固定sleep认定授权顺序。
- GET的有效性是同一语句快照，既有快照可完成；创建的时间有效性在取锁后的INSERT语句时点，之后到期可提交。没有全请求有效身份租约或撤回已提交业务的承诺。
- Workspace响应从不写Cookie；安全响应与日志哨兵测试验证无token/digest/PHC/DSN/原始SQL/请求头体/name/邮箱泄露。主Specs无同步/归档，版本1/2未修改，无新RLS/Redis/JWT/框架。

## 已接受的限制与待代码审查事项

当前没有已知未修复的测试失败。仍保留设计已接受的限制：应用层租户授权（无RLS）、创建非幂等且显式重试可能产生同名重复、未实现配额/公开反滥用系统、任意受信任SQL可破坏最后Owner条件；账户有任何成员关系时不能直接删除。成员管理、更新/删除、完整加入流程均不在本轮。

PR面向main；交付回复提供PR链接、最新提交SHA及对应GitHub CI运行结果，本文记录本地可复现验收，PR工作流日志提供远端证据。完成后等待独立代码审查，不自动合并、归档或公开部署。


## Archive completion（2026-10-15）

用户明确授权合并PR #4、同步主Specs及归档。PR以merge commit `b28562ff7c19d5dc42af2c23725cc1abaa641dae` 合入main，合并前当前PR头的全部CI检查成功。23/23任务完成，全部产物齐全。

同步新增workspace-access（9项Requirement）、workspace-persistence（6项），修改user-authentication的事务授权边界（1项）；所有delta描述/场景逐块比对一致，原认证Purpose及其余Requirement未改。常规`openspec validate --specs`为8/8通过；额外的`--strict`为7/8，唯一原因是已评审认证授权Requirement正文569字符超过500建议上限。为保持已批准契约，没有仅为长度校验缩写或拆分该Requirement；这项格式警告不代表缺失需求或场景。归档CLI另提示本Change超过10项delta的非阻塞建议，保持已评审范围不拆分。

主Specs通过逐块同步比对后，用原生archive命令保留验证地移动Change；`--skip-specs`仅避免原生命令再次应用已经核验同步的delta，并未省略同步。本次仅合并与文档同步归档，不修改业务代码、执行真实迁移或公开部署。归档目标为`openspec/changes/archive/2026-10-15-add-workspace-foundation/`，原验证证据保持不变。

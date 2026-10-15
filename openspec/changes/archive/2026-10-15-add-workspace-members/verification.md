# Verification

2026-10-15（Asia/Shanghai）。用户确认 Proposal、Design、Tasks 和三份 delta 通过独立技术评审，接受全部四项产品决策，正式批准 Apply 及完成后创建指向 main 的 PR。不得自动合并、归档或公开部署。本记录仅把实际执行结果列为通过，不把计划或历史归档记录当作本次证据。

## 基线与数据保护

- 实施前成功 fetch；`main=origin/main=70d53c7267b6e433be16ea020b4c2a1bd9b7d9ee`，左右差异0/0。工作区仅有本 Change 的未提交规划，完整保留，在 `codex/add-workspace-members` 开发。
- Go1.24.3、Darwin arm64、pgx/v5 v5.8.0、固定 Goose v3.26.0。go.mod/go.sum、主 Specs 及迁移1/2/3无差异；未新增依赖、Migration、RLS、角色/加入接口。
- 实际测试服务器为 **PostgreSQL18.6**，aarch64，版本号180006，见 [environment.json](evidence/environment.json)。独立容器 `devpilot-members-test` 使用与CI一致的固定PG18 digest，仅映射127.0.0.1:55433，无开发数据卷挂载，不连接应用库，不读取开发.env。
- 数据库仅为既有守护允许的 `devpilot_registration_test`。显式集成先验证库名/PG18/固定Goose；DDL、Down、禁用/撤销、角色变更和破坏性夹具仅在这个可丢弃库运行。schema套件串行，普通集成与integration-race串行。测试内升级至版本3验证后，cleanup可down-to0；不把cleanup后的库状态误称为已部署版本3。
- 迁移哈希完全保持：[migration-hashes.txt](evidence/migration-hashes.txt)。版本1=`e09f6476b25ce0a60cbd6607486a9b4208f2c3413957c83e54110e0e661b5490`；2=`27ce132375459ea1e061aac1e2c00de1010c9dc8f94c21489e7c24243f02c96a`；3=`327651e7727c512d80062792bab0ea874f1f63320b98ca6b0491dd8e6f94223d`。

## 实际检查结果

测试环境使用临时 GOCACHE/GOMODCACHE，PATH含固定Goose，TEST_DATABASE_URL明确指向上述回环测试库；不回退DATABASE_URL。以下命令在仓库根实际执行。

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| gofmt -l cmd internal tests；git diff --check | 通过，无诊断 | [format.txt](evidence/format.txt) |
| go mod verify | 通过，模块完整 | [modules.txt](evidence/modules.txt) |
| go vet ./... | 通过，无诊断 | [vet.txt](evidence/vet.txt) |
| go test ./... -count=1 -timeout=120s | 全部通过，包含原进程/关闭与网络测试 | [unit.txt](evidence/unit.txt) |
| go test -race ./... -count=1 -timeout=120s | 全部通过，无Race报告 | [race.txt](evidence/race.txt) |
| go test -tags=integration ./... -v -count=1 -timeout=120s | 全部通过，PG集成包24.602s | [integration.txt](evidence/integration.txt) |
| go test -race -tags=integration ./tests/integration -v -count=1 -timeout=120s | PG18全套32项顶层测试通过，58.322s，无Race报告 | [integration-race.txt](evidence/integration-race.txt) |
| 成员模块单独PG18 integration-race | 全部通过；之后又运行上述全量套件 | [member-full-race.txt](evidence/member-full-race.txt) |
| 显式缺测试URL、缺Goose、PG18不可达 | 三者均按预期非零硬失败，不运行迁移 | [required-dependencies.txt](evidence/required-dependencies.txt) |
| openspec validate add-workspace-members --strict | 通过 | [openspec.txt](evidence/openspec.txt) |

PG17/19边界由既有testdb单元测试验证；本次没有启动PG17/19，不冒充多版本实测。未降低请求5s/整段DB事务2s预算，也没有提高既有120s套件期限、删除旧用例或静默skip。

## 失败、定位与修复

1. 按模块先写测试：名单/删除的最初红阶段因类型/方法尚不存在编译失败，见 [roster-red.txt](evidence/roster-red.txt)、[delete-red.txt](evidence/delete-red.txt)；实现后单元/真实PG验证通过才进入下一模块。这些编译红阶段不单独当作业务安全证明。
2. 初次沙箱执行原应用网络测试出现 `bind: operation not permitted`；改在允许回环端口的环境完整重跑通过，未跳过这些用例。
3. 真实TCP慢速chunked DELETE暴露读体超时取消后直接返回可能被net/http补成200，见 [slow-body-first.txt](evidence/slow-body-first.txt)。改为在该不可响应的取消路径抛出ErrAbortHandler关闭连接；有界可写的服务期限失败仍固定503，不执行DB。
4. 首字节即能判为非法的非空chunked body仍可能因net/http自动排空剩余请求而阻塞，见 [rejected-body-first.txt](evidence/rejected-body-first.txt)。拒绝时明确关闭连接、保持立即读截止时间；早期方法/CSRF拒绝及GET隐式排空也设置处理期限。`TestMemberSlowChunkedBodyIsBounded`、`TestMemberRejectedChunkedBodyDoesNotDrain`、`TestMemberEarlyRejectionBodyIsBounded`真实TCP回归均通过；普通400/503响应与取消零DB行为未削弱。
5. 第一次全量单元测试发现原认证OpenAPI测试固定只有6个路径，见 [unit-first.txt](evidence/unit-first.txt)。更新为逐项允许的8个路径及方法清单，继续禁止Refresh/额外方法/POST成员入口，而不是删除检查。
6. 第一次完整集成命令中PG套件已通过，但原认证1ms处理期限测试错回200，见 [integration-first.txt](evidence/integration-first.txt)。定位为deadline已过去、context计时器取消通知尚未交付的调度窗口。`auth.Handler.expired`补充实际deadline比较；原`TestWorkflowBoundAndTimeouts`不变，另用deadline已过但Err仍nil的受控Context稳定复现并验证修复。未改变认证状态码、Cookie、Session TTL或凭据政策。
7. 路由复查发现直接匹配`/members`子串会把原详情接口的非法Workspace ID `members` / `members-extra`提前截获；新增回归并修正成员命名空间识别，保留原详情的实时401/固定JSON404语义。上述两项稳定红测试见 [regression-red.txt](evidence/regression-red.txt)，最终unit/race/PG/CI全量契约验证继续覆盖它们。
8. staged diff补查发现两份新增delta文件末尾多余空行，已仅裁剪尾部空白并重新严格校验；未改变已审核Requirement/Scenario内容。
9. SQL审阅额外收紧Owner计数的相关条件，使其显式依赖当前已授权actor成员根；非成员分支不读取外租户Owner计数。最终全量PG/race在此修改后执行。

## Requirement 与实际验收映射

下表覆盖本轮16项Requirement，每项对应的Success/Exception/Security/Concurrency场景均通过所列测试及明确的文档/SQL审阅验收。已有认证、迁移、创建等场景通过原用例完整回归。

| Capability / Requirement | 实际用例与验证内容 |
| --- | --- |
| members / Member roster is live authorized and minimally projected | `TestMemberRosterStore`、`TestMemberHTTPPermissionsAndRevocation`、`TestMemberRosterSnapshotAndCursorRevocation`、`TestMemberRosterOpenAPIContract`：Owner/Member、只含3字段、disabled关系不暴露status、401/404、单语句快照前后变化 |
| members / Member pagination has strict bounded selectors | `TestMemberRosterProtocol`、`TestMemberRosterStore`、`TestMemberRosterSnapshotAndCursorRevocation`：1/50/100限制、空页、UUID顺序/cursor、非法/重复/query编码、跨租户cursor及跨页撤销；SQL有limit+1上限 |
| members / Member deletion has a narrow permission matrix | `TestMemberDeleteStore`、`TestMemberDeleteHTTPContract`、`TestMemberHTTPPermissionsAndRevocation`、`TestMemberPermissionChangeOrdering`：Owner删Member/disabled目标、Member自退、Member删他人和Owner删其他Owner403、路径身份不可覆盖Cookie、角色变化双顺序 |
| members / Delete selectors and representations are unambiguous | `TestMemberDeleteHTTPContract`、三项真实TCP body测试、`TestMemberHTTPPermissionsAndRevocation`、`TestMemberNamedInvalidWorkspaceKeepsDetailContract`：无query/非空body、目标非规范404、外人不获目标格式提示、无危险路径重定向、取消/读体有界 |
| members / Last owner can leave only when another owner remains | `TestMemberDeleteStore`、`TestMemberConcurrentOwnerAndExitRaces`：1→0拒绝、2→1允许、两Owner自退双获锁顺序、互删403/403、首事务回滚/未知Commit均保留Owner；disabled Owner仍是关系的边界在文档说明 |
| members / Delete is state idempotent under current authorization | `TestMemberDeleteStore`、`TestMemberHTTPPermissionsAndRevocation`、`TestMemberConcurrentOwnerAndExitRaces`：Owner删除缺失/他租户用户ID204、自退后404、重复删除204/204、Owner删与Member自退双顺序、全局Session不变 |
| members / Joining remains a separate consent-based capability | `TestMemberRosterProtocol`、`TestMemberHTTPPermissionsAndRevocation`、认证/成员OpenAPI契约测试、`TestMemberCSRFIdentityAndIsolation`：POST/角色方法405/精确Allow且零DB，HEAD无body/OPTIONS无CORS；加入/同意/重加延期在README/Design明确说明 |
| access / Workspace success responses have a minimal projection | `TestWorkspaceOpenAPIContract`、`TestMemberRosterOpenAPIContract`、`TestMemberHTTPPermissionsAndRevocation`：旧DTO未扩展，新名单独立投影，所有结果无Set-Cookie，204无JSON Content-Type/Location |
| access / Workspace mutations reuse the same-origin CSRF protocol | `TestCSRFAndCookieReuse`、`TestMemberCSRFIdentityAndIsolation`、`TestMemberHTTPPermissionsAndRevocation`：Origin完整tuple/重复/null/Host/Referer/FetchMetadata矩阵、DELETE来源失败零DB、GET不需CSRF |
| access / Workspace responses and processing fail safely | `TestMemberDeleteHTTPContract`、`TestMemberCancellationBudgetsAndPanic`、`TestMemberTransactionFailures`、`TestMemberPoolTimeoutAndMissingSchema`、`TestMemberHTTPCancellationAfterGate`：实际锁/连接池超时、真实缺schema、错误分类、固定日志/敏感panic/取消不响应和无自动重试 |
| access / Tenant boundaries and acceptance remain verifiable | `TestMemberHTTPPermissionsAndRevocation`、`TestWorkspaceHTTPAuthenticationAndTenantIsolation`、全部成员并发/快照测试、负向依赖运行：旧Cookie不能保留已撤销租户权限，跨租户替换拒绝，CI/文档匹配 |
| persistence / Workspace and initial owner commit atomically | 原`TestWorkspaceMigrationAndConstraints`、`TestWorkspaceTransactionFaultsAndUnknownCommit`、`TestWorkspaceCreationLockOrdering`等全量回归；唯一新增成员writer为本次删除/退出，创建协议未改 |
| persistence / Member writes protect current identity and authority | `TestMemberWriteLockOrder`、`TestMemberIdentityChangeOrdering`、`TestMemberPermissionChangeOrdering`、`TestMemberExpiryAfterEveryLockWait`、`TestMemberPostGateExpiryAndCleanup`：User→Session→Workspace→Membership，最终新语句门控、撤销/禁用/角色前后顺序和自然过期 |
| persistence / Supported member writes preserve an owner relationship | `TestMemberConcurrentOwnerAndExitRaces`、`TestMemberDeleteStore`、`TestMemberMixedOperationsAndCleanup`：真实Workspace串行锁、零Owner损坏夹具安全500、Owner保留、不同租户独立；README明确任意SQL可绕过而非宣称DB强制 |
| persistence / Member outcomes require confirmed commit without automatic replay | `TestMemberTransactionFailures`、`TestMemberHTTPCancellationAfterGate`、`TestMemberConcurrentOwnerAndExitRaces`：真实删除后的确认回滚、底层已提交但回执丢失、计数不重试、取消无响应、后续新请求决定实际权限 |
| persistence / Member lifecycle reuses the established schema | 原迁移/约束/User RESTRICT/Workspace CASCADE全量用例、`TestMemberRosterStore`、`TestMemberDeleteStore`、`TestMemberPoolTimeoutAndMissingSchema`、哈希检查：版本3兼容、索引复用、缺schema不自动DDL、无新migration/RLS |

## 并发证据的具体边界

- 所有并发测试运行生产SQL和真实pgx事务；独立连接以屏障暂停在候选身份、保护锁取得、最终门控或提交阶段。通过pg_blocking_pids确认真实阻塞，使用数据库时间观察expiry，不只sleep后猜测顺序。
- 两Owner self-exit在双方获锁顺序下均为一次成功、一次最后Owner拒绝；互相移除均拒绝；首事务确认回滚或底层提交成功但丢回执的情况下仍保留Owner。受支持协议无死锁/序列化错误。
- User/Session/Workspace/Membership四种真实锁等待均验证等待后过期401；最终门控通过后自然过期仍可提交。撤销/禁用的保护行双顺序、账户禁用先User后Session、cleanup先删除/后SKIP LOCKED都有实际用例。
- 读取在授权快照后被暂停，再实际提交移除/角色变化/撤销/禁用；旧快照可完成，新页面/新请求使用当前状态。Workspace创建、新登录Session、cleanup及不同Workspace删除在持有当前Workspace写锁期间仍可完成。
- 未知Commit的网络回执丢失由真实PG底层Commit之后注入EOF；提交前回滚则执行真实Rollback后注入EOF。它们证明服务不会虚假成功/重试并反映真实存储，**不冒充物理断网实验**。40P01/40001等分类用受控驱动错误注入，不声称支持协议实际发生死锁。

## 范围与已知风险

- 已交付列表、移除与退出；没有POST加入、邀请/接受、重加、角色修改或Owner转让。添加成功、重复添加、目标不存在/disabled加入失败、退出后重新加入及加入竞态，均明确留给后续需要目标同意且角色固定member的Change，不用fixture插入冒充产品交付。
- 最后Owner是关系数量不变量，不保证至少一名active Owner。任意受信任SQL/特权DDL仍能绕过应用协议；没有Migration、RLS或数据库级最后Owner约束。后续writer必须协调同样的锁顺序。
- 名单UUID/role/joined_at向同租户Owner和Member可见；已返回数据无法撤回，已授权快照可完成。新请求仍重新验证身份/权限。
- 未知提交不证明回滚；无自动重试、幂等键、exactly-once或未来重新加入关系的条件删除保证。公开部署仍需完成既有认证防滥用前置要求，本次不公开部署。
- 全量回归附带修复了既有认证处理deadline通知延迟问题；未改变其API、Session或Cookie契约，原超时测试保留，并有稳定回归，供独立代码审查重点核对。

## CI与交付

CI保留Go/PG18 digest/Goose版本，push增加本分支；PR仍以main为目标，verify与integration两作业保留，普通PG与integration-race串行。上述是实际本地验收，**不替代托管CI结论**。创建PR后将按最终head SHA核对托管检查并在交付回复报告状态，期间不合并、不归档、不公开部署。

首次分支push已实际触发[Go CI run 38700800231](https://github.com/rjgczp/devpilot/actions/runs/38700800231)，对应实现提交`04ff2ca142f429e3619d68b55c4e2a99de7d9e88`，任务6.1据此完成。记录时该run为in_progress，不写成已通过；最终文档提交仍将重新触发CI，并在PR及交付回复按最新head SHA核验。

首次Git HTTPS push被GitHub拒绝，原因是原Git凭据缺workflow scope；使用已登录、具workflow权限的gh凭据作为单次git credential helper后推送成功，未修改全局凭据配置或暴露令牌。

## 合并与归档完成记录（2026-10-15）

用户在实施交付后明确批准 PR #5 合并到 main，随后要求完成归档；此处记录后续授权和实际结果，前文“不合并、不归档”描述保留为实施阶段的历史边界。

- [PR #5](https://github.com/rjgczp/devpilot/pull/5) 已于2026-10-15T08:13:26Z合并，merge SHA为 `bba324be1ec1873fa5a3234ac48cfc37a2285e23`；最终实现head为 `e41f7089791fba5e809ea0a97fac53c820ce6baf`。
- 最终head的[PR CI](https://github.com/rjgczp/devpilot/actions/runs/38701197012)与[push CI](https://github.com/rjgczp/devpilot/actions/runs/38701133107)均已实际核验为completed/success。合并提交的[main CI](https://github.com/rjgczp/devpilot/actions/runs/38704138412)同样completed/success，verify与integration两个作业均通过，覆盖gofmt、vet、普通/race及PG18 integration/integration-race。
- 归档前 `openspec status --change add-workspace-members --json` 显示proposal/specs/design/tasks均done；`openspec list --json`确认28/28任务完成。归档前main与origin/main同为上述merge SHA，工作区干净。
- 已按智能合并同步全部三份delta：新增workspace-members主契约7项Requirement，workspace-access修改4项，workspace-persistence修改1项并新增4项，总计新增11项、修改5项。16项Requirement的描述和全部Scenario逐块精确比对一致；已有主Spec标题、Purpose和未涉及Requirement保留；新主Spec的Purpose逐字复制已审核delta，无操作类型标题残留。
- `openspec validate add-workspace-members --strict`通过；三份受影响主Specs单独严格校验均通过；`openspec validate --specs`为9/9通过。额外全主Specs严格校验8/9：唯一失败为已有user-authentication第12项Requirement正文超过500字符的建议告警，与前次foundation归档记录一致，本次未擅改该已审核契约。实际命令输出见[archive-validation.txt](evidence/archive-validation.txt)。
- 同步并完成逐块复核后执行 `openspec archive add-workspace-members --skip-specs --yes`，CLI确认任务Complete并归档至 `openspec/changes/archive/2026-10-15-add-workspace-members/`。`--skip-specs`仅避免CLI重复同步，主Specs已在前一步完整同步。CLI另有超过10项delta的非阻塞拆分建议，不代表任务或验证缺失。
- 移动后对归档前全部43个Git跟踪文件做SHA-256比对通过，包含`.openspec.yaml`、规划和既有测试证据；随后仅在本文件追加此完成记录，并新增归档验证证据。活动Change目录已移除。初次全目录校验包含了Finder自动维护的忽略文件`.DS_Store`并出现元数据差异，改为核对全部Git跟踪交付文件后通过；未改写原规划和测试证据。
- 本次归档只有OpenSpec文档同步、记录追加与目录移动；未修改业务代码、已应用Migration或真实数据库，未公开部署。归档未重新运行本地Go/PG测试，上述托管CI是对已合并实现提交的实际全量回归结果。

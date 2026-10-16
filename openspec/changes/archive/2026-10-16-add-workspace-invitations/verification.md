# Verification

2026-10-15（Asia/Shanghai）。用户确认Proposal/Design/Tasks/四份delta通过独立技术评审，正式批准Apply、条件DELETE兼容性变更、Migration4、PG18锁协议和完成后创建main PR；不得自动合并、归档或公开部署。下列通过项只来自实际执行；CI和本地结果分开报告。

## 基线、环境与数据保护

- 实施前main、origin/main与GitHub main均为`fbafb4a890396293d137889ecc85ea7c706f923e`，第五Change已经归档/同步。原工作区只有本Change规划，完整保留，在`codex/add-workspace-invitations`实施。
- Go1.24.3 / Darwin arm64、既有pgx/v5 v5.8.0、固定原生Goose v3.26.0，无go.mod/go.sum变更。
- 真实服务器为**PostgreSQL18.6 / server_version_num=180006**，见[environment.json](evidence/environment.json)。专用容器`devpilot-invitations-test`使用CI相同digest `postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336`，仅映射127.0.0.1:55434，无开发数据卷挂载。
- 只连接守护认可的`devpilot_registration_test`，不读取开发.env、不回退应用连接、不连接真实应用库。DDL、Down、禁用/角色/破坏性夹具都仅在可丢弃库；schema套件、普通integration和integration-race串行。并发仅在稳定schema中使用独立连接。
- Migration1/2/3 SHA256完全不变，见[migration-hashes.txt](evidence/migration-hashes.txt)：1=`e09f6476b25ce0a60cbd6607486a9b4208f2c3413957c83e54110e0e661b5490`，2=`27ce132375459ea1e061aac1e2c00de1010c9dc8f94c21489e7c24243f02c96a`，3=`327651e7727c512d80062792bab0ea874f1f63320b98ca6b0491dd8e6f94223d`。未修改主Specs。

## 旧 DELETE 红→绿证据

先于Migration4/条件删除开发，使用**未修改的生产DeleteMember**及真实PG18事务，在旧请求完成Session保护、尚未取得Workspace锁时暂停。另一请求按生产协议删除M1，再由隔离夹具按Workspace锁协议建立替代关系，提交后恢复旧请求。`go test -tags=integration ./tests/integration -run '^TestDelayedDeletePreservesRejoin$' -v -count=1 -timeout=30s`预期失败，明确报`OLD DELETE REMOVED THE REJOINED RELATIONSHIP`，原始证据[delete-red.txt](evidence/delete-red.txt)。这不是对SQL的mock，也不把当时fixture插入称作产品加入API。

最终`TestDelayedDeletePreservesRejoin`已改为通过**真实邀请创建/接受**建立M2，旧请求只携带捕获的M1条件，返回412且M2保留。`TestInvitationDeleteHeldLockAndSelfReplay`进一步覆盖Owner移除/Member自退、旧请求未获锁/已经持Workspace锁两种顺序：已持锁时PG阻塞被`pg_blocking_pids`确认；旧DELETE完成后才重加，M1重放仍不能删除M2。最终绿证据在完整[integration.txt](evidence/integration.txt)和[integration-race.txt](evidence/integration-race.txt)。

所有旧DeleteMember调用改为显式condition参数，无无条件重载/后门。成员夹具只在创建关系时捕获validator；后续helper从已捕获map读取，不查询新membership_id。新加入结果的validator由调用方明确取自该次Acceptance，不在重试时刷新。Owner重复删除由204/204改为204/412，自退丢权限仍404，最后Owner409保持。

## 实际执行命令与结果

仓库根运行；本地GOCACHE/GOMODCACHE为临时目录，PATH包含固定Goose，TEST_DATABASE_URL明确指向上述独立库。下列数据库套件结束cleanup可down-to0，结果不代表真实部署版本4。

| 检查 | 实际结果 | 证据 |
| --- | --- | --- |
| `gofmt -l cmd internal tests`、`git diff --check` | 通过，无诊断 | [format.txt](evidence/format.txt) |
| `go mod verify` | 通过 | [modules.txt](evidence/modules.txt) |
| `go vet ./...` | 通过 | [vet.txt](evidence/vet.txt) |
| `go test ./... -count=1 -timeout=120s` | 通过 | [unit.txt](evidence/unit.txt) |
| `go test -race ./... -count=1 -timeout=120s` | 通过 | [race.txt](evidence/race.txt) |
| `go test -tags=integration ./... -v -count=1 -timeout=120s` | 通过（134个top-level测试，48.377s） | [integration.txt](evidence/integration.txt) |
| `go test -race -tags=integration ./tests/integration -v -count=1 -timeout=120s` | 通过（50个top-level测试，80.557s） | [integration-race.txt](evidence/integration-race.txt) |
| `openspec validate add-workspace-invitations --strict` | 通过 | [openspec.txt](evidence/openspec.txt) |
| `openspec validate --all --strict` | **9通过/1失败**；既有user-authentication requirements[11]超过500字符的warning在strict下失败 | [openspec-all.txt](evidence/openspec-all.txt) |

额外将配额/重复创建竞争明确改为两个不同Owner，并实际运行 `go test -race -tags=integration ./tests/integration -run '^TestInvitationQuotaAndLazyExpiry$' -v -count=1 -timeout=30s` 通过，见[quota-distinct-owners.txt](evidence/quota-distinct-owners.txt)。此为全套通过后的测试覆盖补充，生产代码没有再变动。

另逐字节对照HEAD，确认全库失败的user-authentication主规格没有被本Change修改；不把局部通过表述成全库通过。检查所有OpenAPI `$ref`可解析，并通过实际handler JSON契约测试校验精确字段、错误码/消息、状态与响应头；没有跳过旧认证路由白名单测试，而是加入已批准的六项操作。

依赖负向真实运行`TestInvitationMigration`：缺URL、不可达、非测试库名、PATH缺Goose均非零失败且不skip，见[missing-url](evidence/dependency-missing-url.txt)、[unreachable](evidence/dependency-unreachable.txt)、[wrong-database](evidence/dependency-wrong-database.txt)、[missing-goose](evidence/dependency-missing-goose.txt)。非18版本/错误库身份守护的边界用例由`go test ./internal/testdb -v -count=1`实际通过，见[version-guard](evidence/dependency-version-guard.txt)；不声称启动过非18数据库服务。

## Requirement 验收映射

四份delta共26个Requirement。下表映射到实际unit/PG18用例；各Requirement的成功/异常/安全/并发场景与既有未改行为同时回归，Schema/隐私/发布边界另辅以SQL和文档审阅。不是用fixture-only插入替代最终同意/重加产品验证。

| Capability / Requirement | 实际验收 |
| --- | --- |
| invitations / Invitations require explicit consent from the bound recipient | `TestInvitationLifecycle`、`TestInvitationHTTP`、`TestInvitationAcceptanceVisibilityAndIndependentTenant`：同意前无关系/detail404、本人Session、固定member、提交前两者不可见/提交后同时可见 |
| invitations / Owners create strictly bounded directed invitations | `TestInvitationInputProtocol`、`TestInvitationLifecycle`、`TestInvitationListsAndTenantPermissions`、`TestInvitationQuotaAndLazyExpiry`：严格JSON/Unicode/TTL、422统一、Owner scope/非Owner不锁目标User、duplicate |
| invitations / Invitations have a fixed lifecycle and exact expiry boundary | `TestInvitationLifecycle`、`TestInvitationExpiryAfterEveryLockWait`、`TestInvitationPostGateExpiry`：五态、终态409、过期409、GET零写/effective时间、门控后自然到期 |
| invitations / Invitation ownership belongs to the workspace | `TestInvitationLifecycle` creator-lost/demoted/revoked、`TestInvitationProtectedIdentityOrderings`：creator退出/降级/禁用/Session撤销仍有效；管理动作当前Owner重新验证 |
| invitations / Invitation lists are scoped bounded and minimally projected | `TestInvitationListsAndTenantPermissions`、`TestInvitationResponseContracts`、`TestInvitationMethodsAndCSRF`：分页/游标/终态名称null、精确DTO、身份与租户scope、GET零写 |
| invitations / Recipient acceptance is a one time bounded operation | `TestInvitationLifecycle`、`TestInvitationActionRaces`、`TestInvitationHTTP`：existing409、ownerless500、重复/并发接受、退出后旧accepted409及新邀请新代际 |
| invitations / Recipients decline and current owners revoke pending invitations | `TestInvitationActionRaces`、`TestInvitationHTTP`、`TestInvitationLifecycle`：双向accept/revoke/decline，终态不可覆盖/不删历史/不影响membership |
| invitations / Invitation outcomes require confirmed commit and no automatic replay | `TestInvitationFaultAtomicity`、`TestInvitationHTTPFaultsAndCancellation`、`TestInvitationClientDisconnectCancelsAction`：真实Commit后EOF/真实Rollback后EOF、计数至多1、503/500/取消、HTTP真实断开 |
| invitations / MVP invitation abuse boundaries are explicit | `TestInvitationQuotaAndLazyExpiry`、`TestInvitationIndexPlans`、`TestInvitationInputProtocol`及README审阅：99→100/101竞争、单对pending、expired不计配额、有限可邀请性与公开前门槛 |
| members / Member roster is live authorized and minimally projected | 原`TestMemberRosterStore`/`TestMemberRosterProtocol`/OpenAPI/HTTP/Snapshot测试改为四字段，排序/权限/disabled关系投影保留 |
| members / Member deletion has a narrow permission matrix | 原`TestMemberDeleteStore`、HTTP、PermissionChangeOrdering、OwnerAndExitRaces：Owner/Member/自退/禁止其他Owner、401/403/404/409；条件匹配才204 |
| members / Delete selectors and representations are unambiguous | 原`TestMemberDeleteHTTPContract`、TCP慢body/路径测试及`TestMemberConditions`：空body/无query、严格强header、权限顺序、编码别名不重定向 |
| members / Delete is state idempotent under current authorization | 更新的Store/HTTP/OwnerAndExitRaces、DelayedDelete测试：旧条件重复412、失权404、204/412、不能刷新代际 |
| members / Joining remains a separate consent-based capability | Member未批准方法/认证OpenAPI白名单回归、InvitationInputProtocol/HTTP：POST members保持405、无role接口、唯一加入为目标接受 |
| members / Membership deletion requires a current relation precondition | `TestMemberConditions`、`TestInvitationHTTP`、`TestDelayedDeletePreservesRejoin`、`TestInvitationDeleteHeldLockAndSelfReplay`：428/412、无无条件入口、M1不能删M2、自退与已持锁两顺序 |
| persistence / Membership represents a constrained many-to-many relationship | `TestInvitationMigration`、原Workspace约束测试、Lifecycle重加：复合PK/role/FK保留、非空/唯一代际、回填/重加生成新id |
| persistence / Workspace and initial owner commit atomically | 原WorkspaceCreationLockOrdering/TransactionFaults/UnknownCommit全量回归；创建Owner契约不变，新邀请只插member |
| persistence / Member writes protect current identity and authority | `TestInvitationLockOrderAndIdentity`、`TestInvitationCreationUserUUIDOrder`、ExpiryAfterEveryLockWait/ProtectedIdentityOrderings/SessionCleanupOrderings及原Member测试：真实锁、双方顺序、排序Users、最终时钟 |
| persistence / Member outcomes require confirmed commit without automatic replay | InvitationFaultAtomicity/ActionRaces/HTTPFaults、原MemberTransactionFailures：确认与未知结果、无自动Commit重放、无补偿误删新关系 |
| persistence / Member lifecycle reuses the established schema | `TestInvitationMigration`、原WorkspaceMigrationAndConstraints/RegistrationMigration与哈希：追加4、旧数据保留、4失败回滚/单步Down、旧3覆盖仍存在 |
| persistence / Invitations have scoped referential and pending uniqueness constraints | Migration、QuotaAndLazyExpiry、IndexPlans：FK/CHECK/部分唯一、历史共存、局部expire+replace、实际PG18 EXPLAIN |
| persistence / Invitation acceptance protects consent and relation generation atomically | AcceptanceVisibilityAndIndependentTenant、ActionRaces、FaultAtomicity、Expiry/Identity/Cleanup/DelayedDelete：原子性、空缺关系保护、无死锁/覆盖角色、不同租户独立 |
| access / Workspace success responses have a minimal projection | 原Workspace DTO/OpenAPI全部回归、名单四字段和InvitationResponseContracts，收件箱preview不放宽detail/roster、无Set-Cookie |
| access / Workspace mutations reuse the same-origin CSRF protocol | 原CSRF测试、InvitationMethodsAndCSRF、HTTP：所有邀请写同源proof、GET只读、method优先/精确Allow/HEAD/OPTIONS |
| access / Workspace responses and processing fail safely | InvitationHTTPFaultsAndCancellation、SlowBodiesAreBounded、CreateCancellationWithoutTransportDeadlines、ClientDisconnectCancelsAction及原panic/超时测试：预算、日志、错误/响应及取消 |
| access / Tenant boundaries and acceptance remain verifiable | 全量unit/PG18/race、负向依赖和最终OpenSpec局部/全库检查；原注册/登录/Session/health/Workspace全量保留 |

## 锁、过期与业务冲突的具体边界

- 真实SQL/pgx事务及独立连接屏障，`pg_blocking_pids`验证实际等待；非单纯sleep推测顺序。所有Users先按UUID升序SHARE，然后当前Session SHARE、Workspace NO KEY UPDATE、Membership按user_id UPDATE、Invitation按id UPDATE，再新语句门控。接受不要求已有Membership；缺失关系由Workspace保护。
- 四种邀请写均验证五级锁等待中的Session/邀请自然到期，使用PG时钟观察；门控之后到期仍可提交。cleanup先删除令新动作401，业务保护先则SKIP LOCKED不删当前Session，提交后才能清理。Owner移除/降级的前后顺序、目标禁用/revoke-all和发起者失权政策有实际用例。
- pending→expired是显式提交业务结果：accept/decline/revoke过期返回409之前先确认Commit。创建在Owner/目标校验之后expire本对旧pending；即使有效pending配额已满，仍确认该expiry Commit后返回409。有效pending重复409、已有成员409和授权/目标失败不作这个转换。SQL/Commit失败不冒充持久化成功；实际Commit后EOF可能已落库，不自动重试。
- 回执丢失由真实PostgreSQL Commit后注入EOF模拟；确认回滚用真实Rollback后注入EOF。**不冒充数据库网络物理断网实验**。另有真实HTTP连接断开与取消传播、真实池/锁超时及缺schema测试。
- Migration/默认值、PK/FK/CHECK和应用协议共同保护受支持writer；不宣称RLS、任意SQL的最后Owner保护或完整历史审计。无公共账户/角色/Workspace删除入口。

## 风险、发布与交付

- **BREAKING**：名单多membership_id；DELETE强制单tag强If-Match，旧无条件请求428、缺失/代际/role变化412。没有旧兼容入口；调用方捕获M1条件，不能自动刷新M2条件。未来新增可变成员属性须重审validator。
- 旧无条件DELETE二进制不能与新邀请/重加共存。迁移文档规定停流量→排空/停止所有旧写者→备份/唯一迁移4→切换条件版本/升级客户端→验收后开放邀请；即使关闭新接受，已有重加关系仍在，不能直接回退旧DELETE。共享库不Down，停相关写入口并前向修复。
- UUID需人工交换；Owner创建成功暴露有限可邀请性，已由用户接受；无搜索/邮件/邀请码。公开前还需网关账户/IP/Workspace限流、跨Workspace接收者频控/拒绝冷却、防注册绕过、脱敏监控及增长/历史保留评审；100配额不是完整防轰炸，本PR不实现Redis/监控/通知。
- 无邀请清理Worker不影响即时过期，但物理pending/终态历史会增长；保留/容量策略仍需发布评审。发起者失权后邀请仍有效是已确认的Workspace归属规则；最后Owner是关系数量不变量，不保证active Owner可用性。
- CI保留固定Go/Goose/PG18 digest与verify/integration两作业；普通PG与integration-race在同一作业内串行。最终PR链接、head SHA和托管CI状态将在交付回复中按GitHub实际结果报告，不将本地通过当作CI通过。


## 2026-10-16 合并、规格同步与归档

用户后续明确授权“完成合并与归档”，取代实施阶段仅交付PR、等待审查的工作流限制；未授权公开部署。

- GitHub核对：[PR #6](https://github.com/rjgczp/devpilot/pull/6) 已为 `MERGED`，实际合并时间 `2026-10-15T09:44:21Z`，合并提交 `edcb73fb2d79196803ce12c988ca50ae13bb8a55`，PR head `19dd61ab49210e68a9255450f266f49f9a5bb95d`。本次发现已合并，因此没有重复执行合并。主分支通过 `git fetch origin`、`git switch main`、`git pull --ff-only origin main` 同步。
- PR head 的 [push CI](https://github.com/rjgczp/devpilot/actions/runs/38713909988) 与 [PR CI](https://github.com/rjgczp/devpilot/actions/runs/38713955864) 均已完成，四项 `verify`/`integration` 检查均 `SUCCESS`；这是GitHub实际查询结果。
- 归档前 `openspec status --change add-workspace-invitations --json` 四类artifact均done，`openspec list --json` 明确29/29任务完成。使用 `spec-driven` schema，保留全部原任务及原测试证据；本次仅同步规格、补充归档记录并移动目录，没有修改业务代码、Migration或数据库，没有公开部署。
- 四份delta按已审核文本同步至主规格：新增workspace-invitations，更新workspace-access、workspace-members、workspace-persistence。26项delta Requirement、122个Scenario逐项匹配；既有标题/Purpose和未涉及Requirement保持原文，既有Scenario名称全部保留，没有delta操作标题遗留。见 [archive-delta-sync.json](evidence/archive-delta-sync.json)。
- 归档前 `openspec validate add-workspace-invitations --strict` 退出0，见 [archive-change-strict.txt](evidence/archive-change-strict.txt)。同步后 `openspec validate --specs` 为10通过/0失败（退出0），归档后再次相同，见 [archive-after-specs.txt](evidence/archive-after-specs.txt)。
- 同步后、移动前 `openspec validate --all --strict --json` 为7通过/4失败（11项含本Change），见 [archive-before-all-strict.json](evidence/archive-before-all-strict.json)。归档后相同命令为**6通过/4失败（10项主规格，退出1）**，见 [archive-after-all-strict.json](evidence/archive-after-all-strict.json)。所有问题均是Requirement描述超过500字符的WARNING，在strict下计失败：既有user-authentication requirements[11]；本次同步文本的workspace-access requirements[7]/[8]、workspace-members requirements[2]/[3]、workspace-persistence requirements[1]/[8]。未把失败归因仅限于旧认证问题，也未将局部严格通过冒充全库严格通过。为保持审核文本，未在归档步骤擅自拆分/改写Requirement。
- 整目录移动到 `openspec/changes/archive/2026-10-16-add-workspace-invitations/`，包括 `.openspec.yaml`。移动后逐文件SHA256核对全部30份原始文件，均与移动前相同；之后仅本verification附加归档记录。归档后 `openspec list --json` 返回空active changes，见 [archive-active-changes.json](evidence/archive-active-changes.json)。
- 归档仅涉及OpenSpec文档和证据迁移，复用已通过的完整本地Go/PG18/race及PR CI验收，不宣称本次重新执行数据库测试。`git diff --check` 通过；主规格标准校验通过，严格格式警告如上保留为已知问题。

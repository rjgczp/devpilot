# Design

## Context

动机与授权范围见 [proposal.md](proposal.md)。以下是 2026-10-20 的只读核验；设计选项不表示已实施或已验收。

### 已观察的基线

| 项目 | 事实与来源 |
| --- | --- |
| Git | HEAD/main/origin/main 均 `1d3b8266e494061beccc3ccdf47694ba4b0339c5`；GitHub branch API 独立确认同 SHA；初始 status 无输出；origin 为 rjgczp/devpilot |
| 第十阶段 | [PR10](https://github.com/rjgczp/devpilot/pull/10) merged=true，merge `85402ab7877e67062e2d48b24e9ad9c31b721461`；归档提交即当前基线；archive receipt 记录 23/23 tasks、404 原文件保留及五 capability 同步 |
| 主规格 | `openspec list --specs` 共 17 份；十份归档覆盖 runtime/health、用户、会话、Workspace/成员/邀请、Project、Monitor 配置、单次执行及激活意图；重点五份 Monitor 主规格已逐场景读取 |
| Schema1–7 | users；sessions（digest-only）；workspaces/workspace_members；membership_id/邀请；projects；http_monitors；monitor_activation_states。没有 run、lease、调度或生产系统角色 |
| Migration7 | 完整三元组 PK/FK；enabled 需授权版本/actor/冷却；next_due_at CHECK 始终 NULL；身份/时间/冷却不可逆、变化须 g+1；禁止 DELETE/TRUNCATE；Monitor INSERT 初始化；配置 revision guard 及 AFTER UPDATE 原子失效 |
| 用户协议 | `internal/monitor/activation_store.go`、`store.go`：READ COMMITTED，User SHARE→Session SHARE→Workspace NO KEY UPDATE→Membership UPDATE→Project UPDATE→Monitor UPDATE→Activation UPDATE；等待后新语句重查 active/Session 严格到期/Owner/scope；只在确认 Commit 后成功，不自动重放 |
| 执行内核 | `internal/monitoring/{snapshot,executor,dns,policy,admission,result}.go`；快照字段私有、同语句完整读取、2s 释放连接；执行构造器私有，仅测试调用；API 不依赖执行器。32/4/2/1 Admission 及速率仅单进程 |
| 历史安全证据 | 第八阶段 `2026-10-19-add-http-monitor/verification.md` 保存 CGO/NSCD 反例、原精确白名单修复及 PG18 normal/race；第九阶段 monitoring-engine 和第十阶段 archive 的网络汇总均 76 项：72 正常 audit0、4 故意绕过 audit1，child 均0。第十阶段包括 activation 零目标审计、schema7/PG snapshot normal/race；均只属于记录的旧 SHA/fixture |
| 当前 CI | [基线 run39356475239](https://github.com/rjgczp/devpilot/actions/runs/39356475239) 精确同 HEAD，verify/integration/execution-security 及要求步骤均 success；Go1.24.3、Goose3.26.0、固定 digest PG18；normal/race 串行 schema suite、Linux Docker NET_ADMIN/SYS_PTRACE、IPv6、fuzz、原零目标审计及独立执行验收。没有 scheduler/fault/生产出口 job；新实现分支需增加 push 触发规则 |
| 部署与本机 | compose.yaml 只有 PostgreSQL18/loopback 端口；没有生产 Probe 镜像、部署清单、路由/防火墙、独立 kill 服务或生产访问证据。本机 Darwin，当前 sandbox 访问 Docker socket 被拒；本轮未运行 PG 或网络验收，也不因历史 CI 通过推定本机可运行 |

Migration SHA256（本轮实际重算，全部与 HEAD 字节相同；1–6 亦与第十阶段 baseline.json 的历史清单交叉比较）：

| 文件 | SHA256 |
| --- | --- |
| 00001_create_users.sql | e09f6476b25ce0a60cbd6607486a9b4208f2c3413957c83e54110e0e661b5490 |
| 00002_create_sessions.sql | 27ce132375459ea1e061aac1e2c00de1010c9dc8f94c21489e7c24243f02c96a |
| 00003_create_workspaces.sql | 327651e7727c512d80062792bab0ea874f1f63320b98ca6b0491dd8e6f94223d |
| 00004_create_workspace_invitations.sql | 0374475bb78a985a9baf3de72055032bcc08e6f18512b850442a59940f20fdfb |
| 00005_create_projects.sql | 4715106905a92991c7f1dcb2da08afb7fd93217716bfa83eb4a33bb8145ac519 |
| 00006_create_http_monitors.sql | 26ac26c66da96b0f16076342f0f11258bb6ea92be5f71a6b015c97a3075ee4e2 |
| 00007_create_monitor_activation_states.sql | 713c39b60a3ede8156c1163404a6883cea00b6c8979b94b36a96047a5532cfaa |

### 真实主规格差异

| Capability / 既有要求 | 本次 delta 的必要变化 | 保留的边界 |
| --- | --- | --- |
| monitor-activation / disabled/distinct；transport projection；ownership audit | 明确废弃旧执行字段并以固定迁移提示替代；execution-status为实际状态权威；迁移使全部旧tag失效，新增限期授权；无有效Owner拒绝新Start | 保留DTO字段名/类型、权限/Owner意图及tag格式，常量值变化属于BREAKING；100 enabled/W、60s冷却、PATCH撤销和Cookie/CSRF不变 |
| monitor-scheduling / Dormant；Production boundary；Future worker；Acceptance | 独立 schedule 中允许 fixture due；旧意图撤销、Claim/Start/Result/恢复；生产保持硬关闭 | activation.next_due_at NULL 原保护保留；无每 Monitor goroutine/重启补偿 |
| http-monitor-execution / internal invocation；system snapshot；immutable results；failure projection | 增加 fixture Worker 桥与 run/g/fence 包络，允许最小结果入独立表；原快照契约不变，新增Start绑定；不开放生产入口 | 所有原 DNS/TLS/HTTP/IP/预算要求及单进程 Admission；原固定预检/网络分类 |
| http-monitor-persistence / minimal config；future execution boundary；新增 Migration8 | 状态不塞入配置表；独立 schedule/run/预算；保留历史6/7阶段迁移含义 | 三元组 FK、父锁协议、旧 migrations 字节、无运行时 DDL |
| http-monitor-management / configuration resources；offline networking | 为授权/status 独立子资源列明确例外；“创建无任务”限定该事务，不与后来 fixture 调度矛盾 | 原四配置操作、字段/URL隐私/ETag及零目标网络 |
| 新 monitor-runs / worker-security | 现有15项执行契约没有持久结果/分布式许可与配额的所有权；分成两个内聚能力 | 不创建重复 URL/认证/SSRF capability |

未涉及的12份主规格不改；新增路由在后续 Apply 的 API contract 中登记，不扩大网络审计的目的白名单。

## Goals / Non-Goals

**Goals:** 通过真实 Owner API、PG18、两个以上 Worker 和独立接收端，在无上游 Linux 拓扑连续执行至少三个周期；验证暂停、过期、崩溃与下周期恢复。将数据库一次有效领取与物理 HTTP 交付分别描述、分别验收。A 的所有可运行入口只存在专用测试构建/runner，普通发布产物无 Probe 启动路径。

**Non-Goals:** HTTP 端到端 Exactly Once；暂停后绝对没有在途包的默认保证；任何生产公网执行许可；通知、Incident、AI诊断、完整历史/SLA/前端/计费、自定义 Header/body/内网探测、新队列或认证重构。

## Decisions

### D1. 选择 A，给出 B/C 的成本与拒绝条件

| 方案 | 交付与依赖 | 验收成本与结论 |
| --- | --- | --- |
| A 推荐 | Migration8、授权/status API、真实领取/Start/Result、共享预算、隔离 Probe/出口监督、故障恢复；独立 PG18 与 Linux IPv6 runner | 20–30工程日+5–8独立验收日；验收时实际 interval 最短60s至少3周期，另有24h积压模拟。可独立交付，生产出口仍未验证 |
| B 暂不选择 | A+具体云/集群、节点/路由/默认deny、独立可撤销许可/停止通道、运维密钥和上线/回退 | 至少另5–10工程日+5–10环境安全验收日，环境未知不能可靠估工；缺真实拓扑/证据不可宣布完成 |
| C 备选 | 仅数据库领取/lease/fence，不可发网络 | 约8–12工程日+3–5并发验收日；不能证明周期探测，本轮不以其作为达成 A 的替代 |

这些是评审估算，未证明节点权限/runner性能之前不作工期承诺。A 缺物理 kill/PG/IPv6任一能力则对应验收失败，不悄悄退为 C 或减弱 SSRF。

### D2. 组件、身份与信任边界

| 组件 | 能力/凭据 | 不允许的能力 |
| --- | --- | --- |
| API | 原 Cookie Session/Owner；只写配置、意图和独立授权、读 status | 无执行内核依赖；无目标 DNS/GET；无出口管理员权限 |
| Scheduler Controller | 独立 LOGIN、仅指定函数 EXECUTE；扫描候选、调度、恢复与保留清理 | 不使用用户 Cookie；不持出口管理员权限；不自行 HTTP |
| Worker 控制面 | 独立、绑定实例代次的系统身份；仅 Claim/Start/heartbeat/result 函数和所需配置读取 | 无 sessions/token_hash/password_hash 读取、用户写、DDL、BYPASSRLS、superuser/CREATEROLE/REPLICATION |
| Probe | 经过内核验证的一个冻结执行快照、run 信封、短期单次 IPC/出口许可；只读根文件系统、非root、capabilities=0、no_new_privs、受限 FD/PID/CPU/内存 | 无 Cookie、Session摘要、DB密码、云凭据、Docker socket、API配置卷；不读控制面环境；无规则管理权限 |
| 出口 Supervisor | 与 Worker 分离的 OS 身份；创建/销毁私有 namespace/veth，维护默认deny/超时 permit，独立停止与签发终止回执 | 不向 Probe 下发长期管理凭据；不能以 Worker 自报“已取消”作终止证明 |
| DNS Resolver | 精确固定 IP/TCP53，独立服务/受控转发；测试为恶意/正常fixture | 无系统 resolver/search/备用上游；resolver 的基础设施例外不成为 target 例外 |

PG 最小权限采用非 LOGIN owner 拥有的窄 SECURITY DEFINER 函数封装父锁协议；固定 search_path=pg_catalog/专用可信schema，完全限定对象，无动态SQL；REVOKE PUBLIC EXECUTE、CREATE/TEMP，函数入口绑定 session_user 与已登记 worker incarnation，不相信参数中的“system=true”。LOGIN 只 EXECUTE，不给父表 UPDATE（PG 行锁本身需要 UPDATE 权限，故不可误称 read-only SELECT 就能取锁）。授权函数只给新 API 运行角色；旧角色无函数权限与 schedule/run DML；迁移账号与运行账号分离。历史 API 的既有权限需盘点并显式拒绝新表/函数，若旧实例用 superuser 则不能混跑，先停旧实例再换凭据。

普通发布二进制无执行入口、默认 deployment gate=closed，调度函数还要求 fixture deployment 身份；测试构建不能凭环境字符串进入生产。A 的测试命令能读取 scope，但只连接自己创建的 guarded testdb，监督器核实无上游接口/路由才签发 fixture-only 许可。无可用签发端或 namespace 不匹配则零网络。B 未来必须另交付生产入口和策略，不能把 A 的测试镜像投入生产。

### D3. 有限授权、旧 API 与旧 enabled 的安全隔离

保留原activation GET/PUT的DTO字段集合/类型、Cookie/Owner权限、意图命令及强ETag格式；废弃其中execution_state/blocked_reason的执行断言，改为固定 `not_reported` / `execution_status_required` 迁移提示（D11）。实际状态只由execution-status报告，不能以旧常量声称正在运行的Monitor被阻断。新增 `PUT .../monitors/{mid}/execution-authorization`，不是手动run：强If-Match沿用 `monitor-activation:<mid>:<v>:<g>`；单对象<=1024 bytes，必填configuration_version字符串、consent_profile=`periodic-http-get-v1`、authorization_seconds整数60..86400。校验/CSRF/错误优先级/5s请求/2s数据库预算与activation相同。成功200只在确认Commit后返回v/g、期限及effective/blocked字段，无URL。

新命令一个事务完成原 enabled 状态更新（g+1、100/W和60s规则不变）以及新 schedule 授权：绑定完整scope、v、新g、scheduler_epoch、协议版本1、数据库 issued_at/expires_at（严格<=24h）、部署类别。授权属于 Workspace，Session/logout/原作者退出不追溯撤销；Claim/Start 必须有至少一个当前 active Owner。Owner 再确认才能延长授权，超时不自动续期；过期仅使 effective=false/expired，desired_state 可以仍 enabled，不能伪称仍获执行许可。

期限采用同一Start Commit模型：过期后不得提交新Start；过期前已提交的Start属于在途，实际发包可能晚于到期，target-permit阶段再次查期限可减少该窗口但不是物理原子保证。该承诺与暂停文案一起交产品评审，不能宣传授权到期瞬间已撤回所有网络数据。

任意旧 activation 命令（包括 enabled→enabled）、pause 或 PATCH 改变 g 时，由数据库触发器在同事务清除 schedule 授权/next_due、推进 epoch，绝不从 enabled 自动创建授权。新授权函数先执行受原保护的 g+1（触发旧授权撤销），再在同一事务写新的 v/g授权。不能用“新字段默认值”识别新 API；旧 API 不能调用新授予函数。新旧 API 共存时旧实例可再次 enabled，但其无新 schedule 许可；若覆盖已有新授权，只会撤销它。

### D4. 独立表而不是把 Worker 更新塞入 Activation

扩展原表会让 heartbeat/next_due 的任意变更触发 g+1，污染 Owner ETag，并诱使移除 guard。选择保留 `monitor_activation_states.next_due_at IS NULL` 的旧约束，明确弃用该占位字段；Migration8 新增独立 schedule.next_due_at，合法非NULL以新表约束/授予协议演进。没有删除或放松 Migration7 的状态保护。这也是对“演进 next_due”的安全选择：不把其旧字段直接变成可调度字段。

| 对象/每行身份 | 最小字段与责任 | 恢复/保护 |
| --- | --- | --- |
| Activation / 完整scope PK | Owner desired_state、activation_configuration_version、g、cooling、last actor/reason/time | 原保护保留；不含 lease/结果；g 溢出使整个事务失败 |
| monitor_schedules / 完整scope PK/FK | authorized v/g、consent_protocol、issued/expires、scheduler_epoch、next_due_at、last_started_at、next_start_not_before、last_attempt_run_id、latest_result_run_id、固定blocked reason | epoch及fencing单调正bigint、不回绕；授予/撤销持父锁；未授权next_due=NULL；新授权不重置 last_started/cooling |
| monitor_runs / (完整scope,run_id) PK | 唯一UUID run_id；v/g/epoch/fencing_token、scheduled/claimed/started/finished DB时间、lease_owner/incarnation/lease_until、attempt_state、有限result_commit_id、duration_ms、可空200..599 status、success、固定failure_code | FK全三元组；scope/v/g/epoch/scheduled不可变；同scope至多1个未结束或物理隔离未确认run；run_id唯一附加约束不替代复合引用；配置完整URL只在内存快照，不复制进run |
| scheduler_budget_state / 有类型完整key | global/W/P/M/origin/target 的令牌/补充时间与保留期限；原子预订数、容量与速率 | 全局1、W/P/M按完整祖先key；origin/target为受控HMAC摘要、不含Path或原始DNS；不允许任意高基数无限新增 |
| scheduler_reservations / reservation_id | 完整scope/run/FK、固定槽、出口 permit/namespace代次、release/quarantine状态、终止回执摘要 | 不是HTTP结果；物理状态独立于lease；active/quarantine不可按TTL逐出 |
| scheduler_control / 固定deployment及worker登记 | gate=closed/fixture、emergency_stop、incarnation、规则/路由/镜像摘要、有效期/最近可信监督回执 | 系统身份无权把production gate打开；无环境变量覆盖；回执不包含URL/凭据 |

执行快照在 Start 事务读取全部当前配置及 v/g/epoch，释放后离线校验，长度有界<=4KiB，通过匿名pipe传给单run Probe；禁止命令行URL/env/日志。DB只保存版本与判定所需数字（如 timeout/expected_status/interval）和结果，不存 name、URL任何部分、DNS答案、HTTP Header/Body、raw error、Cookie/证书/凭据。旧配置不会从历史表恢复后重发。

CHECK涵盖状态枚举、时间次序（claimed<=started<=finished，未开始/未知字段可空）、status范围、success/failure一致、lease配对、v/g/epoch/fence正数；跨表当前版本由锁后新语句门控，不冒称跨表CHECK。函数/trigger阻止身份改写、状态倒退、旧fence、孤儿与裸ID关联；索引限于复合FK、due范围、active lease和保留清理。Result 的时长为内核单调duration，DB started_at 表示许可提交边界，二者不混为物理发包时间。

run另持久化 `physical_release_confirmed_at`（可空）及固定 `stop_reason`；同scope部分唯一索引覆盖state为leased/started或physical_release_confirmed_at仍NULL的记录。这让unknown与物理槽解耦而不依赖跨表索引。reservation以完整scope/run唯一引用run，run不反向引用reservation，避免循环FK。回执同时更新reservation与run的释放标记，保留相同父锁序。schedule保存单调last_fencing_token，不能随run清理而重置。

网络结果failure_code只接受原dns_failed、blocked_target、connect_failed、tls_failed、timeout、canceled、response_limit、protocol_error、unexpected_status；成功为NULL。控制面stop_reason固定为authorization_revoked、authorization_expired、lease_expired、commit_unknown、worker_lost、resource_limited、invalid_configuration、egress_unavailable、emergency_stop、retention_capacity；unknown的HTTP字段/success/failure_code均NULL，不混成网络失败。blocked_reason另固定为scheduler_unavailable、authorization_required、authorization_expired、owner_unavailable、egress_unavailable、emergency_stop、resource_limited、retention_capacity、quarantined、clock_untrusted。

### D5. 状态机与事务边界

`disabled → authorized → due → leased → started → completed` 是多个维度的投影，不是单字段枚举。due 是 DB 时间派生；运行态 attempt_state={leased,started,completed,canceled,expired,unknown}；blocked 是调度门禁原因，目标策略拒绝则是 started 的固定失败结果而不是门禁健康故障。

| 转移 | 数据库前置条件/锁 | 原子动作与失败行为 |
| --- | --- | --- |
| disabled→authorized | 用户全锁序；fresh v/g/Owner/有效Session；quota/cooling；协议1 | g+1；记录期限/epoch；deployment未许可则blocked、next_due=NULL；失败零写 |
| authorized→due | 有效v/g/epoch、未过期、有效Owner、无stop、fixture gate和共享容量 | 启动调度时首个due=DB now；不发网络；缺许可只投影blocked，无空转run |
| due→leased / Claim Commit | 系统父锁序；fresh条件；schedule/run锁；原子预算锁 | 创建run/lease/fence与预订；每scope一个有效run；提交确认前不Start |
| leased→started / Start Commit | 同父锁序；lease仍有效/owner+incarnation+fence匹配；版本授权、期限、stop和监督许可有效；最终新SQL时间门控 | 设置started_at/last_started/not_before；冻结配置；一次Start；未确认不得发Probe输入；断连不猜成功 |
| leased→canceled/expired | 父序后Run；发现pause/PATCH/expiry或lease过期且从未Start | 标记终态，不发送；因为Probe尚无输入/permit可安全回收；新的正常周期不重发旧run |
| started→completed / Result Commit | 父序后Run及资源记录；run/fence/epoch与固定result_commit_id校验 | 存一次最小结果；当前投影还必须匹配最新v/g/epoch；终态不可改写；连接释放后提交确认 |
| started→unknown | 父序；deadline/lease到期或ACK未知；无可信完整结果 | 表示无法判定远端是否收到/执行，不表示failed/未发送；不重发、不复用该run；并发槽quarantine |
| started→canceled（明确未获目标许可） | 父序；可信单次信封/内核permit状态证明未拨目标且独立Supervisor确认终止 | 预检/配额/许可拒绝只存stop_reason，无HTTP结果；无法证明未发送则unknown，不能仅信Worker自报 |
| unknown→物理隔离已确认 | 回收器父序；独立Supervisor确认旧namespace代次全部出口drop、conntrack失效、进程退出且不可能恢复旧许可 | 只释放资源，不把unknown伪造为completed；之后才可安排新周期 |
| completed→下一due | 授权仍有效、非stop、无未释放物理资源；fence不回退 | next_due=max(DB now+interval,last_started+interval,next_start_not_before)；错过周期折叠为下一次，不逐次补偿 |
| 任意→blocked/expired/disabled投影 | 查询最新授权/门禁；pause/PATCH本身按原用户事务 | 阻止新Start；已Start尽力cancel但不作物理未发承诺；不重置历史资源账 |

lease 建议45s，heartbeat每5s，Start需至少剩余timeout+5s；网络最多30s，监督器permit固定硬截止<=Start+35s且不能由Worker延长。heartbeat只在deadline内续租，不改变permit；相同Monitor下个run须先证明前次物理结束。heartbeat/Result无Session权限且锁序同父链。用户API保持锁后新客户端语句的statement_timestamp；后台受限函数按D6在最终新SQL/SPI命令用等待后一次clock_timestamp取gate_now，不用事务/外层调用开始时间。时钟后跳取max(已存时间,DB now)，大幅跳变关闭新执行，物理截止用监督器单调时钟。

### D6. Claim / Start / Result 的锁图与并发证明

[PG18 SELECT](https://www.postgresql.org/docs/18/sql-select.html) 明确 SKIP LOCKED 适合队列但不提供一致查询视图，行锁仍不避开表锁；故不能先锁 due Activation/Run 再锁父资源。[行锁冲突](https://www.postgresql.org/docs/18/explicit-locking.html) 与 [READ COMMITTED](https://www.postgresql.org/docs/18/transaction-iso.html) 支持下述应用协议，具体正确性仍需真实PG屏障验收。

用户：User SHARE → Session SHARE → Workspace NO KEY UPDATE → actor Membership UPDATE → Project UPDATE → Monitor UPDATE → Activation UPDATE → Schedule UPDATE。配置trigger只沿Monitor→Activation→Schedule撤销，不获取Run或预算锁。

系统Claim完整顺序唯一规定为：**候选无锁读取 → 候选Owner User SHARE → Workspace NO KEY UPDATE SKIP LOCKED → Membership UPDATE → Project UPDATE → Monitor UPDATE → Activation UPDATE → Schedule UPDATE → Run UPDATE → Budgets → Reservation**。无锁阶段只生成hint，不得取得业务行锁；Workspace必须在候选User已保护之后尝试。Start重新执行同一父序（W可有界等待），不沿用Claim授权。授权属于Workspace，无需原作者或Session；但本事务须证明候选User active及同一membership_id仍为Owner。

Result/回收/清理：不要求仍有活Owner，使用 Workspace→Project→Monitor→Activation→Schedule→Run→预算→reservation 的相同子序；绝不在后面补取User/Session/Membership。预算维护若只锁预算就不能再进入业务父链。多资源按类型和完整key排序，一事务只处理一个Monitor；无全局API资源锁。部署stop/control在末段用SHARE保护；停止方只锁control提交，不回头等业务父资源；物理kill走独立OS通道。

**有界扫描与逐语句伪代码**（规划说明，不是Migration或实现代码）：无锁due索引扫描最多64个Workspace、每W最多8个Monitor候选，以无锁同语句读取完整scope、v/g/epoch和候选Owner uid/membership_id。每tick1s、每实例最多32次候选尝试、每W每轮最多领取1个；**一个候选使用一个事务**，不能持W再挑另一个Owner。轮转游标不授予权限。

```text
hint := read_due_scope_and_owner_without_locks()  // 无FOR锁，仅候选
若无Owner：只读投影blocked，结束候选，不写Schedule/Run
BEGIN READ COMMITTED                         // 全事务<=2s，lock_timeout<=250ms
SELECT id,status FROM users WHERE id=hint.uid FOR SHARE
  缺行/非active/等待超限 -> ROLLBACK，下一轮重新无锁选择
SELECT id FROM workspaces WHERE id=hint.wid FOR NO KEY UPDATE SKIP LOCKED
  无返回行(被锁或不存在) -> ROLLBACK，立即释放User锁并轮转
SELECT membership_id,role FROM workspace_members
  WHERE workspace_id=hint.wid AND user_id=hint.uid FOR UPDATE
  缺行/非owner/代际!=hint.membership_id -> ROLLBACK全部锁，再无锁选Owner
SELECT scoped Project FOR UPDATE
SELECT scoped Monitor FOR UPDATE
SELECT scoped Activation FOR UPDATE
SELECT scoped Schedule FOR UPDATE SKIP LOCKED
  任一缺失/被跳过/等待超限 -> ROLLBACK，不在原事务换scope/Owner
SELECT scoped outstanding Run FOR UPDATE
  已有未释放run -> ROLLBACK/轮转；否则在父锁下建立未提交新Run
LOCK/conditionally reserve Budgets(global,W,P,M，固定类型/key序)
INSERT scoped Reservation                    // 预算/容量不足全事务回滚
按原协议末段保护deployment control，不反向请求业务父资源
FRESH SQL/SPI COMMAND: 全部等待后取一次gate_now=clock_timestamp()
  重新JOIN并门控实际claim DML，成功才可COMMIT
  active User + 相同Owner membership_id/role + 完整W/P/M归属
  当前v/g/epoch + enabled + 协议1授权 + expires_at>gate_now
  next_due<=gate_now + 无其它未释放run + control许可/stop + 原子预算
  任一不符 -> ROLLBACK新Run/预订/全部锁，下一轮重新选择
COMMIT并释放连接；仅确认成功才允许后续Start
```

origin/IP预算在DNS后的Target Permit阶段才预订，Claim只处理已知global/W/P/M及容量key，所有阶段同一类型/key序；不把DNS搬进Claim。不存在Run时由W/M/Schedule锁与部分唯一约束保护插入，Run创建先于Budgets且失败全回滚，不在预算锁后回头取Run锁。每步独立语句、完整scope，不使用可能先锁子表的joined locking SELECT。

**等待/失败**：SKIP LOCKED仅作用于已到达该层的行锁，不绕过User、不保证跳过表锁。User/成员/父资源等待及表锁受250ms/2s预算约束；超时、deadlock或取消整体回滚，稍后有界新轮次不等于重放未知Commit。持W时发现另一Owner可用也不得补锁该User，必须ROLLBACK后重新无锁读取/BEGIN。Claim Commit未知仍只对账原run_id，不按失败路径新建替代run。

**READ COMMITTED复验**：锁前hint和等待前判断不是授权。全部保护等待（包括预算/control）结束后，执行**独立的新SQL command**重读User/成员代际/Owner、scope、v/g/epoch，条件化Claim DML。不能折入锁前快照CTE，不能用transaction_timestamp。User失权先提交则拒绝；User SHARE先保护则变更等待、Claim可先完成；成员变化先提交按新语句失权，不能同事务换Owner。

伪代码在非LOGIN owner的窄 `VOLATILE` PL/pgSQL函数内逐条执行，Worker仍只有EXECUTE权限，不获得父表SELECT/UPDATE。函数内最终SQL/SPI查询须获取新快照，不能用STABLE函数固定外层快照；最终gate在所有锁等待之后一次求值clock_timestamp()，该次DML及lease时间共用gate_now。statement_timestamp()在同一外层函数调用中仍可能是调用开始时间，不能因写成“内部新语句”就假定时间已更新；用户API原有锁后新客户端命令的时间协议保持不变。依据[PG18函数快照](https://www.postgresql.org/docs/18/xfunc-volatility.html)及[当前时间函数](https://www.postgresql.org/docs/18/functions-datetime.html#FUNCTIONS-DATETIME-CURRENT)。PG18测试须在函数内部真实锁等待跨过期限，证明快照和时钟均更新、零新Claim/Start；不是只检查函数返回值或用Go时间替代DB时间。

**真实PG18锁序验收**：两个Owner U1/U2、两个Worker和用户PATCH各用独立连接/事务屏障。覆盖同Owner/不同Owner同时Claim，hint后Owner禁用/退出/降级/移除重入双顺序，以及W1持U1/W、U2用户PATCH持U2/Session等W。W2候选U2须在W SKIP后释放User锁；W1不能反向等U2，失效U1只能全回滚后新事务选U2。以pg_locks/pg_blocking_pids和事务时间线证明无Activation/Run先W、无W后补User、最终至多一有效fence；不能仅用无deadlock或sleep-only证明锁序。有限租户负载继续测p95/饥饿，不声称无限租户严格等待上界。

**Claim Commit**：唯一(run scope活跃谓词)和W/M行锁让两Worker无法同时创建两个有效run；lease/fence只在持父锁后推进。Claim ACK丢失只按已生成run_id+worker incarnation查询，不再次领取替代run。查明leased且有效才允许Start；查不明则等待/过期，不发网络。

**Start Commit**：重新完整门控，不复用Claim快照。提交ACK成功后才发一次匿名pipe输入并登记独占Probe句柄；管道重连不重放、进程重启不恢复发送能力。Start ACK丢失即使数据库读回started也不能重新下发输入，保守unknown；因为DB不能证明另一个发送者是否已收到旧能力。未Start的lease可终止并另建未来run；绝不把started改回leased。

**Target Permit Commit**：原内核完整DNS验证后、literal TCP之前增加窄的内部许可钩子：仅向控制面短pipe报告本次选中IP（只在内存）；控制面父序重查run/授权/lease/stop，原子预订跨租户origin+IP预算，再按下述外部协议取得Supervisor许可。失联/超时/不确定一律不拨号，不缓存许可。回调耗时包含原timeout，不能增加重解析/重试。此钩子只减少原内核可做的连接，不替换DNS或HTTP；原76项及新增卡点必须复验。若不能保持内核契约/预算，该桥不通过评审，A不能验收。

预算提交与外部Supervisor并非原子事务。具体次序为：事务外准备默认deny的namespace及短期监督回执；Start事务核验回执并确认Commit/释放连接后才下发单次信封；DNS完成后Target Permit事务只原子预订目标预算，确认Commit并释放连接，随后Supervisor安装本run单literal目标许可，最终只发一次allow响应给Probe。任一步ACK未知不重放发送/开放命令，Probe不拨号，预订转隔离对账；只有独立终止证明才能释放。准备/规则安装/管道等待/kill回执均不在业务事务或占用的DB连接中进行。这里的“取得许可”是有序、失败关闭的协议，不宣称数据库与网络存在两阶段原子提交。

**Result Commit**：确认Probe进程退出/出口不可恢复后，提交结果/槽释放/下一due。相同result_commit_id仅允许对账同一摘要；ACK丢失可查询或幂等提交相同结果数据，禁止调用内核。旧fence不写；已经unknown的迟到结果拒绝改写，保持unknown。版本/g/epoch已变化时，可在原run保留最小完成结果，不更新latest_result/current状态；只有原run本身的fence有效才可落库。网络调用栈不占DB连接；快照/每次控制事务后连接立即归池，pg_stat_activity无跨网络长事务。独立短heartbeat可能使控制面瞬时InUse非零，不能错误要求整个共享池在所有网络时刻为零；需关联backend/事务观测而非仅看defer断言。

此处有两道不同门控：run捕获的epoch/fence必须与该run尚有效的身份一致；当前schedule epoch/v/g只控制当前投影。Pause改变schedule epoch，不等于给仍有效的旧run伪造新fence，也不禁止其原谱系审计；一旦run lease已失效或已转unknown，普通Worker不能再落其结果，回收器只可记录终止回执。

### D7. 暂停/PATCH 的七种交错与物理边界

推荐 A 的用户承诺：**“暂停成功后不会再批准新的执行开始；已经批准的请求可能仍发送或完成。状态显示是否仍有在途/未知请求，无法撤回已到达目标的请求。”** 暂停不等所有网络结束，继续用原2s DB预算；API cancellation是尽力通知，独立监督器负责硬终止。原配置DTO不加入执行状态。

| Worker位置 | Pause/PATCH先赢的PG顺序 | Start先赢或已跨边界的行为/接收端预期 |
| --- | --- | --- |
| 1 尚未Claim | W/M保护下改变g/epoch；Claim锁后复查失败，无run或取消 | Claim先提交仍需再次Start门控 |
| 2 Claim已确认未Start | Pause锁先更新；Start条件不匹配→canceled，零Probe/GET | 如Start先提交，按第4类处理 |
| 3 Start正在等锁 | 暂停持W/M/Schedule；Start等到后新SQL看到失效，不能只用锁前snapshot | Start先保护则Pause等其Commit；这是唯一数据库顺序，不证明已发网络 |
| 4 Start已提交未连接 | 暂停可在发pipe前/后提交；尽力cancel/关闭permit；不再授权新Start | 旧一次许可仍可能晚于Pause响应才执行DNS/TCP/GET；新target-permit门控可减少但不能消除该窗口 |
| 5 TCP/TLS已完成未GET | 尽力close、网络Supervisor切断；弱模型不等待终止回执 | 已建立流仍可能在Pause后发GET，应用CAS没有物理保证 |
| 6 GET已到远端 | 只能取消后续I/O，无法撤回远端副作用 | 结果属旧v/g；不再重发；暂停不能承诺回滚 |
| 7 已结束未Result | Result父锁后看到新v/g/epoch，只存原run审计 | latest/current不被旧结果覆盖；已完成HTTP不等于成功持久化 |

PG证明针对1–3：同一W/M锁在双方提交之间形成全序；锁后新语句读到对方已提交状态，写条件含完整scope/v/g/epoch/fence。4–7属于数据库与外部系统间的非原子窗口，PG不能证明没有GET，必须由接收端卡点记录实际到达/时间界限。

更强模型供B评审：每run独立namespace/veth及epoch permit；暂停提交撤销意图后，独立控制通道阻断双栈该namespace所有流（包括ESTABLISHED）、清conntrack、关闭/杀死Probe，确认旧epoch不可恢复，然后强暂停响应才成功。不得只删除NEW连接规则或只杀Worker控制面。若监督器/节点分区，强暂停返回pending/失败而非成功，资源继续quarantine；已经进入网络的包和远端动作依然不可撤回。这个模型要另设异步停止接口/预算及生产验收，A不声称提供强暂停。

### D8. 崩溃、重复与恢复语义

| 故障点 | 处理 | HTTP/结果保证 |
| --- | --- | --- |
| Claim前崩溃 | 无run；另一实例正常扫描 | 无该次请求 |
| Claim成功ACK丢失 | 查询既定run_id；无法确认则不Start，lease后终止该run | 不因未知Claim创建第二个同周期run |
| Start成功ACK丢失 | 不重发执行信封；将无法确认run归unknown并隔离旧permit | 可能零次或一次GET；允许丢弃本可成功结果 |
| 发GET后进程死、尚未存结果 | started→unknown，独立kill证明后释放 | 远端可能执行，结果可能丢失；绝不自动重试此run |
| Result成功ACK丢失 | 同result_commit_id读取/幂等写同份结果；无网络重试 | 最多一个数据库终态；读取确认后才显示完成 |
| Lease过期但旧Worker仍活 | fence失效，拒绝heartbeat/结果/再授permit；原网络资源quarantine | lease不证明物理执行停止；没有kill证明就不释放槽、不启动同Monitor下一run |
| 两Worker/网络分区重连 | 原子claim/fence；旧incarnation永久不可重新获得旧permit | DB至多一个有效fence；不保证目标端Exactly Once；僵尸收到连接恢复也不能重放 |
| API/服务重启、大量积压 | 不从started重建Probe；下次due按当前DB时间和interval推进，跳过错过周期 | 不逐次补偿24h每个interval；无无限补发 |

健康只能是已确认匹配当前v/g/epoch的最近一次观察，不是可用率。unknown不能编码为HTTP0、success=false或timeout来伪造网络结论。下一正常周期允许继续，但前提是旧物理权限不可恢复、共享预算可用且原授权仍有效；否则blocked/quarantined持续。

### D9. 集群预算、同目标滥用与有界磁盘

共享PG原子预订替代无锁count(active_leases)。初始保守参数：集群32槽、W4、P2、完整M1；global8/s burst8、W1/s burst2、P1/2s burst1；Monitor至少 max(旧not_before,last_started+当前interval)，跨version/g/重启保留至少3600s。原单进程32/4/2/1及32/4/2速率继续作为第二层，不宣称它实现集群限流。

同canonical origin跨W：1槽、每60s一个开始、burst1；同选中literal IP跨origin：2槽、每秒最多1次/burst1。origin在控制面离线解析scheme/host/default-port后用HMAC-SHA256生成，排除Path；IP只在完整DNS验证后以HMAC-SHA256(type|native address)生成预算key，不保存原始DNS答案或可逆IP。私钥只在控制面，Probe无key；key版本轮换前排空活跃槽，旧预算至少1h共存，不以轮换清空限速。结果/API不含这些hash或跨租户占用信息，拒绝统一resource_limited，避免暴露另一个Workspace。

原始DNS解析仍有shared global/W/P Start token控制；Target permit获取失败不发TCP、不退还已耗令牌。Claim预订并发槽，Start消耗令牌，Target permit再保留origin/IP槽；全部使用固定锁序和DB时间，有条件更新，整数定点token防浮点溢出。没有足够令牌不进入等待队列/无限忙循环。到期租约的所有physical槽不按时间自动返还；必须Supervisor回执。DB/控制面断连不自动续permit；硬截止/规则超时独立生效，未收到终止证明继续占槽，安全优先于可用性。

HMAC预算不是完整反滥用证明：域名别名/共享CDN/多IP/恶意DNS轮转仍可能集中同一组织服务，无法通过无侵入协议识别共同运营者。A只允许fixture目录中的预注册目标，做跨W同origin与别名同IP实验；B公开注册必须另有目标所有权/同意验证、允许目标目录及分层组织预算，否则生产公开执行保持关闭。单纯origin hash不能作为公网匿名探测许可。

保留策略建议成为A验收硬参数：每M最多100条终态run且至多7天；全库最多100000终态+32个活跃/quarantine run；每W最多10000；每M一schedule、一当前投影，active reservation<=32，预算key总数<=10000（global/W/P/M/origin/IP合计）。按作用域/全局有锁计数预留新run数量，达到cap先清理可删终态，否则blocked retention_capacity；不能先INSERT再异步删以致无上限。active/unknown未收回物理权限者不删，不逐出仍在冷却key。清理每次<=100行、2s事务、父序及keyset，不长事务全表扫描；latest pointer在同事务清空或替换，读返回无结果而非悬空。

按每run含索引/页放大预留4KiB：100032条约391MiB；预算/schedule初始10000 Monitor部署上限另预留128MiB；含vacuum/bloat/WAL预留独立10GiB测试磁盘、运行数据预算1GiB、>70%告警/>80%拒绝新Claim。SQL行数上限不等于物理磁盘硬上限，WAL/备份/日志另限额与轮转；测量pg_total_relation_size、WAL速率和长事务阻塞vacuum，若超预算关新执行。增加容量须重审；既有配置数量不被Migration拒绝，超过10000仅阻止新调度登记/授权，不能阻止pause/PATCH。

容量不足时先回滚Claim，再以新的父序清理事务释放可删终态；不能持global预算锁反向寻找/锁Monitor。冷却预算key若被TTL清除，新建key的初始token须等价于已过完整补充窗口；active/quarantine及未过最大1h安全保留的目标key不能逐出。Migration不为所有旧Monitor回填schedule行；仅首次明确授权时延迟登记，schedule总行数硬限10000。没有schedule即没有执行授权，旧g变更对不存在schedule只需维持无许可。无授权/无活跃资源且冷却已过完整1h的登记可在父序下清理；有run未清理时不删除schedule，以免重置fence。原配置数量不作为安全升级的拒绝条件。

### D10. 生产出口候选架构与禁止上线条件

A只实现测试拓扑。B候选为专用Linux执行节点：API及PG在控制网；Controller/Worker只通过TLS访问最小数据库函数；独立Supervisor位于受信节点管理域，通过专用控制pipe给每run创建无凭据sandbox；Probe只拥有两个方向的受限匿名pipe与本run namespace。没有Docker socket传入；容器权限和宿主防火墙由独立基础设施管理，不把NET_ADMIN给Probe。

出口策略须先于进程启动生效：双栈默认DROP；只允许精确resolver IP TCP53及通过策略/route审核的TCP80/443；拒绝metadata、私网/特殊用途、集群Pod/Service、节点/宿主、组织内部公网CIDR、管理网；未知接口/路由/透明代理、目的NAT、任意NAT64/隧道、IPv6不受控则拒绝准入。TLS保持原域名验证；独立出口规则在转换后实际路由处也要验证危险目标不可达，不能只看应用RemoteAddr。

规则/namespace/veth/镜像/Resolver/节点incarnation绑定短期许可；监督器持续验证，规则集替换原子默认deny、permit有内核到期机制，规则漂移/守护失效/丢网自动阻止新执行。B需在可信节点边界设置不可由Probe/Worker关闭的上游默认deny或外置网关，证明本地规则被删时仍拒绝；“定时轮询发现漂移”本身不能保证无窗口，无法证明就不放行。重启/扩缩容/迁移必须先封锁旧节点epoch、得到物理隔离回执再签新节点许可，分区旧节点不能与新节点同时恢复权限。

运维紧停独立于API/PG：管理域撤销整个部署epoch、切断现有连接与新流并杀Probe；DB stop只负责禁止新Start，不能取代网络kill。恢复需重新验拓扑/规则、签新epoch、对账quarantine及quota，不自动重放旧run。EGRESS_APPROVED=1、容器名、namespace存在、CI success都不是安全凭证。

无生产部署清单、实际IPv4/6路由/转换证据、规则丢失负向控制、节点重建连续性、独立kill、最小身份、跨租户目标反滥用、产品暂停承诺任一项，生产出口未验证且硬关闭。不能标记B“可部署完成”。

### D11. 最小API与用户可见投影

新增 `GET .../monitors/{mid}/execution-status`，同语句Cookie active Session/实时Membership/三元组授权，Owner/Member可读、无写；query/条件头/GET body不支持，5s/2s、no-store/nosniff、固定401/404/500/503。输出有界：scope、当前v/g、desired_state、effective_authorization、expires_at、调度投影、next_due、in_flight、pause_semantics=start_commit、固定blocked_reason、最近attempt及最近completed最小观察（各最多1条）。无URL、actor、worker/lease所有者、IP/hash、raw error。

execution-status是**实际运行观察的唯一权威接口**：同一数据库语句快照给出意图/期限/调度/attempt及谱系，不根据旧Activation字段推断。区分authorized_waiting、due、running、completed、blocked、unknown、disabled、expired；旧run不作为新当前健康。最新attempt unknown不能被较早success掩盖；current_observation失配当前v/g/epoch则null。Activation不新增run字段；配置列表/detail/PATCH不变，无历史分页/聚合/可用率/前端。

DTO以独立字段表达这些维度：authorization_state=none/valid/expired，scheduling_state=disabled/authorized_waiting/due/blocked，latest_attempt携自身v/g/epoch及leased/started/completed/canceled/expired/unknown，current_observation仅匹配最新授权。即使Owner已暂停、当前scheduling_state=disabled，旧started/unknown仍须在latest_attempt/in_flight显示其旧谱系；不能用disabled掩盖在途，也不能用旧unknown给新配置制造健康结论。running/completed是attempt的用户可见状态，不复用enabled或混成一个总状态字段。

#### D11.1 明确废弃旧执行字段，保留意图资源

比较方案：Activation动态复制运行摘要会让后台状态改变响应内容却不改变v/g，破坏强ETag覆盖稳定意图表示的语义；追加run字段也扩大旧DTO。选择废弃两个旧观察字段、保留键/类型并使用固定迁移提示。强验证器须区分表示数据变化，不能仅用no-store解释同tag下动态不同内容（[RFC9110强验证器/ETag](https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8.3)）。

| Activation字段/协议 | schema7/第十阶段历史契约 | schema8新API契约 |
| --- | --- | --- |
| execution_state（string） | const blocked，当时系统强制休眠 | const not_reported：本资源不报告实际执行，不是运行状态枚举 |
| blocked_reason（string） | const scheduler_unavailable | const execution_status_required：纯迁移提示，不是当前阻断原因 |
| next_due_at | NULL休眠占位 | 仍NULL且弃用占位；真实时间读execution-status.next_due |
| shape/权限/命令 | 原字段集合、Owner/Member读、Owner写、Cookie/CSRF、强v/g If-Match | 全部保持，不增删字段，不将enabled变为执行授权 |
| ETag | monitor-activation:<mid>:<v>:<g> | 格式/强条件不变；后台运行不改变该表示或g，仍不支持条件GET/304 |

GET及确认成功PUT均用新固定对，返回固定成功响应头 `DevPilot-Activation-Projection: intent-v2`；无该头表示旧服务，不能用旧常量判断当前执行。新客户端必须到规范execution-status读实际观察，404/503/不支持时显示“运行状态不可用”，绝不回退为“被阻断/未运行/健康”。错误仍为原安全封装，无成功data/tag；新增头只标识协议，无执行能力。

这是**BREAKING**：旧const校验/封闭枚举客户端须升级，不能声称第十阶段响应值逐字节兼容。保留字段形状/类型、意图语义、权限和tag算法；改变两个常量及旧tag有效性。第十阶段主规格/归档/证据保持原文，只在本Change的monitor-activation MODIFIED块提出替代，不修改历史文件绕过原断言。

#### D11.2 兼容迁移与接流量门禁

1. 先升级/确认客户端：识别intent-v2，实际状态读execution-status；旧/未知协议只显示“状态不可用”。后续Apply更新OpenAPI const/弃用说明，保留JSON字段和认证测试；新增语义回归替代旧常量断言，schema7历史fixture继续验证第十阶段原值，不改归档。
2. 维护窗口停并排空旧HTTP入口/所有写者后，Migration8令**每条存量Activation g恰好+1一次**（enabled同时撤销，disabled仍disabled），last_transition_kind=upgrade_revoked、actor=NULL、updated_at非递减，冷却逐值保留。表示协议改变必须使全体旧强tag失效，原enabled不额外+2。与DDL/版本记录原子提交，任一disabled或enabled的g耗尽均整体失败；不得留下部分新版许可。
3. 仅新API接流量，逐实例验证intent-v2头/固定对、权威status路由及schema8；此HTTP门禁通过前fixture部署gate保持closed，不能因Owner新授权而执行。旧API可作为非接流量DB写者做安全回归，其enabled无执行许可；**不支持旧HTTP实例向可运行Monitor继续返回blocked常量的混合接流量**。路由/实例不能证明全新则不启动fixture，不能靠客户端猜测掩盖矛盾。
4. 回退旧HTTP协议前先关新Start、撤销有限授权，独立终止/对账所有物理资源；维护窗口排空读写，再经schema8受控失效协议令所有Activation g+1、保留冷却、清授权/推进epoch后才切旧表示，避免同强tag跨表示复用。日后再次v1→v2切换也须显式退役所有旧tag，不能以Goose up/up的no-op冒充协议切换；普通重启/up/up不推进g。保留Schema8、不Down恢复授权；g耗尽或终止未确认则旧路由不放行。本轮只规划受控函数/运维步骤，不执行。

#### D11.3 跨接口一致性验收矩阵

schema8 Activation GET/确认PUT恒为 `not_reported/execution_status_required`、intent-v2头，只表示本接口不报告执行。下表在受控同快照/无并发变更窗口比较意图与权威观察；生产始终无执行入口，fixture running不代表生产能力。

| 场景 | Activation真实意图 | execution-status权威观察及必须保留信息 |
| --- | --- | --- |
| 新建/暂停disabled，无在途 | disabled，授权版本NULL | scheduling_state=disabled、effective=false，无当前健康 |
| authorized_waiting | enabled，v/g匹配 | authorization_state=valid、scheduling_state=authorized_waiting；leased未Start只在latest_attempt=leased显示，不谎称running |
| due | enabled | scheduling_state=due，不断言已Start |
| running | enabled，或已暂停而旧run在途 | latest_attempt=started/显示running、in_flight=true及旧谱系；当前意图可disabled，不能据此隐藏运行 |
| blocked | enabled，无有效新Start许可 | scheduling_state=blocked及准确原因，如authorization_required、scheduler_unavailable/egress_unavailable；不伪造目标失败 |
| unknown | enabled或disabled | latest_attempt=unknown，终止未确认in_flight=true且阻止新Start；回收后保留unknown但in_flight=false，不被旧success遮盖 |
| 配置PATCH失效 | v+1、g+1、disabled，清授权 | scheduling_state=disabled/current_observation=NULL，旧started/unknown按旧v/g/epoch显示，不更新新观察 |
| 授权过期 | desired可仍enabled，查询不改v/g | authorization_state=expired、effective=false、scheduling_state=blocked/reason=authorization_expired；过期禁止新Start，旧在途仍显示 |
| completed且版本匹配 | enabled，意图不因完成改变 | latest_attempt=completed/最近观察可用；失配则current_observation=NULL，无完整健康统计 |

两次HTTP读可能处于不同快照，v/g不匹配须刷新权威status，不能拼接，不承诺两请求原子一致。运行变化不刷新Activation tag/updated_at、不消费Owner g；GET无条件缓存/304语义。真实PG18/HTTP验收覆盖全表、两个Owner、配置失效/过期读取竞态，旧HTTP路由与fixture执行并存必须被门禁拒绝；所有API仍零目标网络。

### D12. 实际可运行验收拓扑与分层门禁

以下命令/路径是后续Apply需交付的接口，本次不创建脚本、不启动容器。目标新增 `tests/scheduler/run-docker.sh`：宿主只用于准备镜像，测试期无上游；创建独立PG18控制网、两个Controller/Worker容器、可信Supervisor、每run独立双栈Probe netns、受控DNS/HTTP/TLS接收端。PG只在控制网，Probe无PG路由/凭据；fixture网用独立无上游veth模拟公网形状/metadata/内部公网/DNAT，所有流仍在测试namespace，绝不触达宿主/云metadata。与第九阶段单namespace fixture相比，新拓扑必须真实验证进程/DB/出口权限分离，不能直接复制就宣布生产安全。

Supervisor在测试管理容器有NET_ADMIN；Probe降权后caps0，rootfs只读、memory128MiB/cpu1/pids128/nofile256，env白名单、无source/DB卷，输出有界4KiB。Probe不能修改规则；可信harness单独运行恶意bypass对照。独立接收端记录DNS、SNI、GET及是否带凭据，固定fixture数据；strace process/network保留来源，不抓真实客户URL/环境。规则/路由/cgroup/二进制SHA/退出码与run/fence时间线分目录保存。

| 层级 | 必须实际执行的内容与证据 |
| --- | --- |
| A Go/静态/race | 状态/expiry/v/g/fence/overflow、共享槽token、取消/错误分类、redaction哨兵、API import无执行能力、单次信封不可重放；gofmt/vet/unit/race，新增hook不降低原预算 |
| B PG18 | 两Owner/两Worker同或不同User Claim；hint后Owner失权/成员代际变化；用户PATCH持U2/Session等W与Worker持U1/W锁图；W skip立即释放User；全部等待后新SQL复验；Claim/Pause、Start/PATCH；lease/旧fence/未知Commit；三元组/连接释放；Migration0→8/7→8/up/up/全体g失效及disabled溢出回滚/Down；旧DB写者不授予、旧HTTP禁止与fixture运行并存；D11.3跨接口矩阵 |
| C Linux网络 | 原76项全部保留；真实rebinding/双栈/TLS/proxy/redirect/慢大响应；七个暂停阶段卡点的接收端观测；断permit/既有连接/僵尸/强杀；应用IP绕过由独立规则阻断；Probe无凭据、无PG路径；正常允许目标对照必须真正收到GET |
| D 分布式故障 | 2+Worker同时Claim；SIGKILL前后重启、DB断连/分区、lease过期仍活、Result ACK丢失、24h虚拟时间积压（另保留60s真实周期）、1000W同时到期公平与资源边界、同origin跨W/alias同IP、节点namespace重建/旧permit不可恢复、quota不提前返还 |
| E 历史回归 | 第八配置与第十激活零目标审计（新授权/status也纳入，目的白名单不变）、Cookie/CSRF/Owner/Member/强ETag/固定错误；Migration1–7哈希；完整normal integration及integration-race串行；execution snapshot normal/race；OpenSpec strict |

PG并发用独立连接、事务屏障、pg_locks/pg_blocking_pids及pg_stat_activity，不用sleep-only或mock代替。所有未知提交通过真实Commit后丢确认代理/测试包装观察，对照数据库与接收端；不是仅令Commit返回错误。D必须证明expired started不再被发送，且无kill证明时配额持续占用。

CI新增独立scheduler-security job和scheduler PG normal/race步骤，各破坏性schema suite串行且用内部testdb守护；job缺Goose/PG18/IPv6/namespace权限则失败。保留原3个job与精确零网络policy；按同一实现SHA记录工具链/镜像digest/命令/exit/receiver/firewall/trace/正负对照、scenario→test→artifact映射。不得将旧76项证据或本文规划当作本次执行通过。生产出口独立一栏永远为“未验证/关闭”，直到另行B环境验收。

## Risks / Trade-offs

- [Start与发包非原子] → 明确较弱暂停承诺，接收端七阶段验收；产品不接受则生产保持关闭，不能靠CAS宣传物理停止。
- [监督器/内核/节点被突破] → 独立上游deny/管理域与短期许可；A只测隔离fixture，不作生产安全结论。
- [安全回收降低可用性] → 无终止证明就quarantine，最多32个槽耗尽后全局blocked；运维独立kill后对账恢复，不以lease时间冒充网络终止。
- [共享预算热点和W串行化] → 全局预算锁只在短事务末段、有界扫描；测p95/饥饿；不通过则降低吞吐或优化同一协议，不引入未批准队列。
- [跨租户同目标识别不完整] → HMAC origin/IP保守共享，fixture目录；公开生产必须目标同意/允许目录及实际证据。
- [M8回填规模、冷却和bigint溢出] → 维护窗口/备份/全事务失败；表示协议升级须退役全部旧tag，任何旧Activation含disabled的g耗尽均阻止迁移，不部分授权；人工前向修复需独立审查。
- [磁盘不是只由run行决定] → 数量硬cap、vacuum/WAL/日志预算与拒绝新Claim；无物理空间测量不宣称无限期可运行。
- [新IPC/hook引入内核回归] → 私有最小桥，原内核协议不另写；76项全复验，hook超时失联零dial；风险无法关闭则A不验收。

## Migration Plan

这里只规划 `00008_...sql`，不创建文件或执行。普通启动/readiness仍不DDL；迁移角色独立。

1. 部署前保留生产硬关闭；盘点旧API数据库角色、连接与在途事务、备份/锁预算。推荐维护窗口停止写入并排空旧API，不采用未证明安全的滚动升级。即使旧实例意外存活，下面数据库协议仍保证其不能授予新执行权限。
2. 单Goose事务先锁http_monitors、monitor_activation_states的DML（与7相同方向），建新对象/窄权限并增加upgrade_revoked原因。旧enabled改disabled/授权NULL；**所有存量Activation含原disabled都g+1恰好一次**，隔离新旧表示的强tag，actor=NULL/reason=upgrade_revoked、updated_at非递减、冷却逐值保留；任何行溢出全回滚。新schedule表空/gate closed，无due/run，仅首次明确Owner授权限额登记。相比原案仅推进旧enabled，disabled的tag也失效是修订2显式兼容成本。
3. 保留g/cooling/身份/DELETE/TRUNCATE保护及配置revision guard；扩展activation变更trigger仅向下撤销Schedule/epoch，任何旧命令/g变化都不能留下匹配授权。原activation.next_due CHECK不删；独立schedule.next_due的新约束承担调度演进。迁移写入、约束替换、回填与Goose8记录同Commit，异常全部回滚；锁外无授权窗口。
4. 创建窄函数/权限，旧角色新表零DML、无grant函数；新Worker验证schema8/协议1/gate/epoch及D11.2新HTTP投影接流量门禁，schema7 fail closed。旧DB写者跨迁移只生成无schedule意图，新授权被旧写覆盖即撤销；数据库保证不意味着允许旧HTTP在fixture运行时返回误导常量。superuser旧连接未排空/权限不清或仍有旧投影HTTP路由则不启动Worker。
5. 在隔离测试拓扑部署新API，Owner读新v/g后明确授权fixture。Production deployment没有执行签发端/入口，因此授权也只能显示blocked；只有外部审查批准的A runner能运行其新建testdb目标。任意迁移/重启不能自动启用历史配置。
6. 应用回退按D11.2先关授权/新Start、撤销授权、独立kill并确认所有in-flight；旧API接流量前排空读写并退役所有旧tag，保留Schema8/冷却。共享环境只前向修复，不Down恢复enabled。8→7原生Down仅guarded可销毁testdb，先物理停止；将upgrade_revoked按g+1转为7兼容disabled/configuration_changed且保留冷却，再清新增子表/函数/权限并恢复7定义；授权不复活。历史up-to固定，原故意失败next8移9并含真实1–8。

## Review gates before any Apply

第一轮独立技术评审对A原则通过，本轮两项修订仍待再次审核，不是Apply批准。请产品确认：①A且生产硬关闭；②Start Commit暂停/到期在途文案；③最长24h授权；④D11旧执行字段废弃、常量breaking change、全部存量g+1失效旧tag、客户端迁移及禁止旧HTTP与fixture混合接流量/回退成本；⑤既定quota/保留及fixture目录；⑥Linux/PG18/Supervisor条件。不同选择先更新工件，再由新请求明确授权Apply；B生产CIDR/规则连续性/目标所有权仍未验证。

## Initial Propose validation record

2026-10-20 初稿完成只读核验与规划：proposal/design/tasks及7份Delta Specs；当时42项任务全部未勾选。`openspec validate add-monitor-scheduler-worker --strict --json` 通过且issues为空；`openspec validate --all --strict --json` 为14通过/4失败，仍仅user-authentication（1）、workspace-access（2）、workspace-members（2）、workspace-persistence（2）七个历史>500字符WARNING，与第十阶段记录一致，未改无关主规格。此段记录初稿，不代替下述评审修订校验。

所有MODIFIED块保留原要求名和全部原场景标题，必要场景内容按本阶段范围更新；新能力含独立Purpose。Git检查确认已有跟踪文件、代码/CI/主规格、Migration1–7均未变，仅本Change规划目录新增。未实施Migration8、未跑新Go/PG18/网络/故障测试、未创建实现PR或执行部署/合并/Archive、未探测实际互联网目标；生产出口状态为未验证/关闭。CLI的4/4工件complete仅表示规划齐备，不能作为Apply或上线批准。

## First review revision record

2026-10-20 用户报告第一轮独立技术评审对方案A原则通过，授权仅修订两项一致性问题。本次修改7个既有工件：proposal/design/tasks，以及monitor-scheduling、monitor-activation、monitor-runs、http-monitor-persistence四份Delta Specs；另外三份Delta不变，没有新增文件/Capability，也未同步主规格或归档。

| 问题 | 修订及验收位置 |
| --- | --- |
| Claim锁序含糊 | D6统一无锁候选→User SHARE→W SKIP→成员/父子链→预算/预订，补逐语句伪代码、超时/全回滚/重选及受限函数内的新快照/新DB时间；tasks4.2–4.6和scheduling场景要求两Owner/两Worker/失权/用户PATCH真实PG锁证据 |
| 旧接口blocked与实际running冲突 | D3/D11及activation完整MODIFIED表示契约明确废弃旧观察字段；固定not_reported/execution_status_required/intent-v2，无实际执行断言；status唯一权威，矩阵覆盖disabled/等待/running/blocked/unknown/失效/过期；标记常量/全体旧tag失效/切换限制为BREAKING，tasks2.2–3.5/7.4–7.5/10.5验收 |

两项修订不改变生产硬关闭、旧enabled原子撤销、最长24h新授权、Start Commit暂停、Supervisor物理终止证明、共享预算/同目标限流、最小结果/有界保留，以及前十阶段认证/零网络/76项内核回归。全体存量g+1（含disabled）是废弃表示的明确兼容成本：仍只推进一次、冷却不变、新授权不自动产生，等待再次独立审核及产品确认，未视为已获Apply批准。

本轮实际执行 `openspec validate add-monitor-scheduler-worker --strict --json`：exit0、1通过、issues=[]；全库strict：exit1、14通过/4失败，仅原七项长度WARNING（user-authentication[11]，workspace-access[7,8]，workspace-members[2,3]，workspace-persistence[1,8]）。原有要求及场景标题完整保留；修订前快照对比确认仅上述7份Markdown变化，47项任务均未勾选，已有跟踪文件SHA256全部不变。未执行Go/PG18/网络测试或迁移、未创建Migration8/实现PR、未合并/部署/Archive。规划完成后停止，等待再次独立审核和明确Apply授权。

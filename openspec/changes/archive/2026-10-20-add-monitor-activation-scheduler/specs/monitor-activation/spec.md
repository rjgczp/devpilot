# monitor-activation Delta

## Purpose

定义 HTTP Monitor 的版本授权意图、当前 Owner 启用和暂停权限、强条件并发、配置失效、默认关闭、配额及最末审计。意图与真实网络执行分离，持久化成功不能被解释成已经运行或已经通过生产出口安全审查。

## ADDED Requirements

### Requirement: Activation intent is disabled by default and distinct from execution
历史和新建Monitor SHALL 默认desired_state=disabled、授权配置版本NULL、activation_generation=1、next_due_at=NULL。enabled SHALL 仅表示Owner确认的配置意图；本阶段所有状态 SHALL 返回execution_state=blocked和blocked_reason=scheduler_unavailable，无探测/健康结果。

#### Scenario: Success - Existing and newly created monitors remain dormant
- **WHEN** 存量升级、新Monitor创建、API启动或重启
- **THEN** 每Monitor有且仅有一条完整scope状态，初始化不启用，不发目标DNS/TCP/HTTP，不把interval转换为任务

#### Scenario: Success - Enabling records intent without claiming execution
- **WHEN** Owner成功设置enabled
- **THEN** 保存指定配置版本意图但仍返回blocked、next_due_at=NULL，无运行中/健康/结果字段

### Requirement: Activation reads and writes enforce current scoped authority
GET/PUT `/api/v1/workspaces/{wid}/projects/{pid}/monitors/{mid}/activation` SHALL 复用Cookie-only身份；Owner/Member读、仅当前Owner写。读取 SHALL 绑定同语句实时身份/成员/完整scope；写入 SHALL 在最终数据库DML门控active User、有效Session、当前Owner及三元组，不能依赖Handler预查。

#### Scenario: Security - Owner and Member permissions
- **WHEN** 当前Owner读取或写入、当前Member读取或写入真实scope
- **THEN** 前三种允许，Member写403 monitor_forbidden且不泄露tag；创建者无额外权限

#### Scenario: Security - Lost identity and substituted scope
- **WHEN** Session撤销/过期/账户禁用，或非成员/未知/非规范UUID/双租户成员混合wid/pid/mid
- **THEN** 身份无效401；已认证但不可见scope统一404 monitor_not_found，无裸ID存在性查询、URL或状态泄露

#### Scenario: Concurrency - Authority and expiry are checked after waits
- **WHEN** 写等待任一级保护锁期间发生退出/移除/重入Member/撤销/清理/禁用或Session过期
- **THEN** 最终新数据库语句重新门控；失权先提交则401/403/404、零写，写先保护可以先完成，不能用事务开始时间冻结时效

### Requirement: Commands confirm configuration and consume a separate generation
每次成功启用/暂停 SHALL 消费activation_generation一次，不改变Monitor配置version。启用 SHALL 显式确认当前配置version并记录activation_configuration_version；暂停 SHALL 清空授权版本。即使同值命令也消费代次，正bigint溢出 SHALL 整体失败500且不回绕。

#### Scenario: Success - Explicit enable pause and resume
- **WHEN** Owner以新鲜条件启用v7、暂停、再恢复v7
- **THEN** 状态依次enabled/disabled/enabled，授权版本7/NULL/7，g逐次增加，恢复不复用旧代次

#### Scenario: Concurrency - Same value and ABA
- **WHEN** 最新tag下提交同值命令，或enabled经过disabled再enabled后重放旧请求
- **THEN** 前者仍消费g，后者412；字段值相等不恢复旧授权条件

#### Scenario: Exception - Generation exhaustion
- **WHEN** g已是正bigint最大值而命令需递增
- **THEN** 固定500，意图、代次、冷却和审计均不变

### Requirement: Every configuration revision atomically revokes activation
任何成功配置PATCH SHALL 原子递增配置version、将意图disabled、清空授权版本并递增g，包含名称及同值PATCH。URL变化 SHALL 无例外撤销。配置失败/回滚 SHALL 不改变激活；状态缺失或代次溢出 SHALL 使配置与失效整体回滚。

#### Scenario: Success - Conservative revocation for all fields
- **WHEN** Owner修改URL/name/interval/timeout/expected_status/method，或提交同值PATCH
- **THEN** 新配置版本与configuration_changed失效状态同一Commit可见；新版本需Owner显式重确认

#### Scenario: Concurrency - Configuration and activation serialize
- **WHEN** enable/pause与v7到v8的合法PATCH竞争
- **THEN** PATCH先提交则旧v/g意图命令412；enable先提交则PATCH撤销它；PATCH先提交使旧pause412时当前已disabled，无跨表半提交

### Requirement: Activation uses one strong precondition over configuration and generation
GET/确认PUT SHALL 返回强ETag `"monitor-activation:<mid>:<v>:<g>"`。PUT SHALL 要求单一强If-Match，最大128字节、规范UUID/正bigint；body配置版本也须匹配。语法非法400；实际身份/scope/Owner门控之后缺条件428、条件不匹配412。系统 SHALL 不刷新条件或自动重放。

#### Scenario: Exception - Strict conditional grammar and error precedence
- **WHEN** weak/wildcard/多值/重复/空/超长/越界tag，或合法请求来自无效身份/不可见scope/Member
- **THEN** 语法非法400；语法有效后的401/404/403先于428/412；Owner旧mid/v/g或body旧version412且错误不含当前tag

#### Scenario: Concurrency - Two Owners consume one condition
- **WHEN** 两个Owner同时以同一v/g启用或暂停
- **THEN** 最多一个200及g+1，另一个412，无自动合并或第二次提交

#### Scenario: Exception - Unknown commit and response loss
- **WHEN** 真实Commit成功后丢弃确认、响应丢失或Commit结果未知
- **THEN** 未确认请求不得返回成功data/ETag；不补偿/重放，授权GET对账；若已提交则原tag重试412，若回滚则仍可能消费原tag

### Requirement: Activation transport and response projections are bounded and private
PUT SHALL 只接受必填字符串desired_state/configuration_version的单JSON对象，最多1024原始字节，复用严格Unicode/重复键/媒体校验。请求 SHALL 复用精确Origin/CSRF、5秒请求和2秒DB预算、固定安全错误及no-store/nosniff；无Set-Cookie。成功 SHALL 只在确认Commit后返回200。

#### Scenario: Exception - Body path and method rejection
- **WHEN** body结构/未知重复键/null/多JSON/Unicode非法，字段枚举或版本范围非法，超限或媒体错误，query/GET body、路径别名或不支持方法
- **THEN** 分别400/422/413/415；路径别名404；其它方法405 Allow GET, PUT且HEAD无body；GET带If-Match或任何其它条件头400，PUT不接受Content-Encoding

#### Scenario: Security - CSRF and fixed failure behavior
- **WHEN** 精确同源proof不符，或DB依赖失败/确认超时/内部错误/客户端取消
- **THEN** proof失败403 csrf_rejected且零DB；依赖或确认超时503、内部/schema500；取消停止工作不继续响应，日志无Cookie/URL任何部分/raw error/SQL

#### Scenario: Success - Exact status representation
- **WHEN** Owner/Member GET或Owner确认PUT成功
- **THEN** data仅有完整scope、当前configuration_version、desired_state、授权版本、g、next_due_at、next_enable_allowed_at、last_transition_kind、created_at/updated_at及固定blocked字段；版本/代次为字符串、时间UTC RFC3339Nano；无URL/actor/lease/run/health，原配置DTO与ETag不变

### Requirement: Intent quotas and enable cooling are shared and durable
每Workspace SHALL 最多100个enabled意图；每Monitor成功enabled后60秒内 SHALL 不再接受enabled，冷却使用数据库时间持久化且跨实例共享。pause和配置失效 SHALL 不受此限制，亦不重置/缩短冷却。最新tag下enabled到enabled SHALL 仍消费代次和冷却，无额外数量占用。

#### Scenario: Concurrency - Last available intent and shared cooldown
- **WHEN** 两实例竞争同Workspace最后名额，或同Monitor冷却内再enable
- **THEN** Workspace保护后计数保证最多100；超限409 activation_limit_reached，冷却429 activation_rate_limited附Retry-After；失败不改变状态/代次/审计

#### Scenario: Security - Pause and configuration changes remain available
- **WHEN** quota已满或冷却未到时Owner以合法条件暂停或PATCH
- **THEN** 不因该限制拒绝撤销；再启用仍不能通过换配置/g或重启绕过原冷却

### Requirement: Intent ownership survives its author while audit remains bounded
已确认意图 SHALL 属Workspace，不因原Owner正常退出或Session撤销而追溯消失，前Owner后续访问按当前成员权限。最末状态审计 SHALL 仅保存当前g、固定原因、DB时间及Owner命令的可信actor，不保存URL/Session/网络详情或无界命令事件；不公开actor。

#### Scenario: Success - Another Owner retains control
- **WHEN** 启用者退出但还有当前有效Owner
- **THEN** 意图保留，其他Owner可按新条件暂停；前Owner失去写权，重新加入Member也不能恢复旧请求

#### Scenario: Security - No active Owner never causes execution
- **WHEN** 异常禁用导致Workspace没有有效Owner
- **THEN** 本阶段仍固定blocked且零执行，不把owner行计数当作活Owner证明；未来引入执行须另行验证失权门控

#### Scenario: Success - Last-state audit has a fixed row bound
- **WHEN** 同一Monitor发生大量成功命令/配置失效
- **THEN** 只有一条状态行，覆盖最后原因/时间/actor，默认及配置失效actor为NULL；不产生事件历史或伪造完整审计承诺

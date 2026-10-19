# http-monitor-persistence Specification


## Purpose

定义 HTTP Monitor 配置的数据库身份、不可变租户和来源、复合引用及版本一致性，规定实时授权写入、锁等待后的时效检查、未知提交结果和 Migration6 的安全边界；验收必须基于受保护的真实 PostgreSQL18，不将规划测试或未来探测安全契约当作已完成证据。

## Requirements

### Requirement: Monitors store minimal constrained configuration and provenance
Monitor SHALL 保存数据库生成UUIDv7 id、非空workspace_id/project_id/name/url/method/interval_seconds/timeout_seconds/expected_status/created_by_user_id/version/created_at/updated_at。默认值 SHALL 为GET/60/10/200、version1及相等数据库时间。不存在状态/探测结果字段；身份、双父归属、创建者、created_at SHALL 在普通SQL更新中不可变。

#### Scenario: Success - Database generated configuration
- **WHEN** 受保护的Owner创建最小有效配置
- **THEN** 数据库生成UUIDv7和默认值，creator来自受保护Session，不依赖客户端身份字段；保存不创建调度/结果记录

#### Scenario: Exception - Documented database validation subset
- **WHEN** 普通SQL提交必填NULL、空/超长名称、名称ASCII控制或ASCII边缘空白、空/超2048字节/含ASCII空白控制/非ASCII或不以规范http(s)://开头的URL、method非GET、interval不在枚举、timeout越界或不小于interval、status不在200–599、非正version或updated_at早于created_at
- **THEN** CHECK/NOT NULL拒绝；完整URL/punycode/Unicode名称策略由应用验证，数据库CHECK不宣称完整URL或SSRF保障

#### Scenario: Security - Identity and tenant immutability
- **WHEN** 普通SQL更新id/workspace_id/project_id/created_by_user_id/created_at为不同值，包括移到同Workspace的另一个真实Project
- **THEN** 不可变保护拒绝实际变更，等值赋值允许，不改变来源或授权依据

### Requirement: Composite references preserve project and monitor tenant ownership
Monitor SHALL 通过FK(workspace_id,project_id)引用projects(workspace_id,id)，creator引用users(id)，均DELETE RESTRICT且无Membership FK。SHALL 有PK(id)、UNIQUE(workspace_id,project_id,id)及creator索引，无name/url唯一约束。未来子表 SHALL 以完整三元组引用Monitor；本轮只验证临时子表，不创建生产子表。

#### Scenario: Security - Mixed tenant parent cannot persist
- **WHEN** SQL组合Workspace A与Project B，即使两个UUID各自真实且actor同时属于两Workspace
- **THEN** 复合FK拒绝，正确组合成功；HTTP/Store仍独立执行实时授权，不以FK替代权限

#### Scenario: Success - Safe future child identity and list index
- **WHEN** 临时结果fixture以三元组引用Monitor，或按同一Workspace/Project及id范围读取
- **THEN** 正确引用成功，任何wid/pid/mid混合被拒绝，复合唯一索引同时支撑列表，无额外重复列表索引

#### Scenario: Exception - Independent creator lifetime and restricted deletion
- **WHEN** creator退出/被移除/禁用，或SQL试图删除被引用的Project/User
- **THEN** 前者不删除配置或延续creator访问权，后者RESTRICT防孤儿，无public删除API

#### Scenario: Concurrency - Foreign key protection
- **WHEN** 真实PG18独立连接顺序控制父删除与子插入
- **THEN** 正确FK锁保证有效引用或拒绝一方，不能提交孤儿；fixture遵循父先于子顺序且schema测试隔离

### Requirement: Monitor writes protect live authority before actual DML
每次写入 SHALL 在一个有界READ COMMITTED事务内保护User→Session→Workspace→Membership→Project→Monitor（创建无现存Monitor锁）。授权保护锁 SHALL 与既有写协议相容；全部保护等待之后的新数据库语句 SHALL 校验active身份、严格Session时效、Owner及完整归属并门控实际INSERT/UPDATE，不能只信任Handler或锁前读取。

#### Scenario: Success - Compatible protected write
- **WHEN** 必要锁后身份/Owner/Project/Monitor范围有效
- **THEN** 在可信digest绑定的实际DML中再次门控并持锁至事务完成；配置写不改变Project属性/版本

#### Scenario: Security - Direct Store invocation is still tenant authorized
- **WHEN** 绕过HTTP预查直接调用Store，或客户端伪造actor/role/tenant字段
- **THEN** 无调用者断言或id-only写入口，Session/User/Membership/Project/Monitor的实际SQL门控阻止未授权修改

#### Scenario: Concurrency - Authority loss in both orders
- **WHEN** Monitor创建/PATCH与撤销、禁用、Session清理、Owner退出、条件移除或受协调角色变更竞态
- **THEN** 写先保护可先完成，变更先提交则新门控401/404/403；退出后重入为Member不恢复旧Owner请求，已有member-generation协议不被削弱

#### Scenario: Concurrency - Time is rechecked after all waits
- **WHEN** Session在User/Session/Workspace/Membership/Project/Monitor任一保护锁等待期间过期
- **THEN** 最终新语句以expires_at>statement_timestamp()拒绝401；最终门控之后自然过期可提交，锁不冻结时间

### Requirement: Lock compatibility is verified with existing project and authority writers
Monitor写 SHALL 先取得User/Session SHARE、Workspace NO KEY UPDATE、actor Membership UPDATE、匹配Project UPDATE，再在PATCH取得匹配Monitor UPDATE。SHALL 避免在子锁后请求父锁和同一行弱锁升级；多个同类对象按稳定ID排序。真实PG18验收 SHALL 检查FK父/creator锁与既有Project、邀请、成员及清理路径，无全局资源锁。

#### Scenario: Concurrency - Project update and Monitor write
- **WHEN** 同Workspace的Project PATCH与Monitor POST/PATCH或不同Owner写同Monitor交错
- **THEN** 既有Workspace保护先串行化、父行锁在子行前；Project版本仅自身PATCH递增，Monitor前提仅检查自身tag而非Project tag

#### Scenario: Concurrency - FK locks and independent scopes
- **WHEN** FK隐式父键锁与显式Project/User保护交互，或不同Workspace有并发写
- **THEN** 已保护父锁无反向依赖/跨事务升级；独立Workspace不用全局锁，共用actor身份仍可能合法争用；异常死锁安全失败不自动重放

### Requirement: Monitor versions atomically guard complete configuration
PATCH SHALL 在完整三元组及当前version条件下更新允许字段、递增version并设置非递减数据库updated_at；最终有效配置 SHALL 重新校验。缺条件/旧条件/非Owner等失败不改变配置，bigint耗尽 SHALL 安全500不循环。未来读取者 SHALL 获得同一快照内的整条配置和version，不能混合不同版本字段。

#### Scenario: Concurrency - Two distinct Owners and ABA
- **WHEN** 不同Owner以同一版本更新，或配置A变B再变A后重放旧tag
- **THEN** 最多一个写消费原版本，其他412，等值更新也消费版本，旧意图不可因字段相等而恢复

#### Scenario: Success - Complete future configuration handoff
- **WHEN** 将来经评审的引擎读取Monitor
- **THEN** 按同一三元组读取不可撕裂的配置及version，未来结果须记录所用version并按独立执行期安全契约验证；本轮不实现或声称运行该读取者

### Requirement: Monitor outcomes need confirmed commit without automatic replay
写成功 SHALL 仅在确认Commit后返回；单次请求 SHALL 最多提交一次，无自动重试/补偿/刷新条件。未知Commit、超时、取消或响应丢失 SHALL 不代表回滚。POST SHALL 无幂等保证，显式重试可重复；PATCH SHALL 保留原条件重新授权，无成功确认不能返回data/ETag/Location。

#### Scenario: Exception - Confirmed rollback and committed lost acknowledgement
- **WHEN** 真实事务回滚后注入错误，或真实Commit成功后丢弃确认
- **THEN** 分别保留旧状态或可能保留新状态，均不返回未确认成功；live客户端安全503/500，取消不尝试响应

#### Scenario: Concurrency - Response loss recovery
- **WHEN** 创建/修改已提交但响应丢失后用户读取或显式重试
- **THEN** 授权读取可观察真实状态；名称/URL不能唯一识别未知创建，旧PATCH条件若已提交则412，不自动重放

### Requirement: Migration six is additive and transactionally recoverable
Goose6 SHALL 只增加http_monitors及其约束/索引/不可变保护，Migration1–5字节不变。SHALL 支持0→6和有数据5→6、重复up；真实6中途失败 SHALL 回滚DDL及版本记录且保留旧数据。启动 SHALL 不迁移，readiness只测连接。Down SHALL 仅在受保护可销毁测试库执行；共享发布串行迁移、前向修复。

#### Scenario: Success - Fresh and populated upgrade
- **WHEN** 真实PG18从0或保有前七阶段用户/Session/Workspace/成员generation/邀请/Project数据的5升级6并重复up
- **THEN** 无默认Monitor/旧数据变动，schema6仅有规划对象，旧数据快照和值及历史migration哈希保留

#### Scenario: Exception - Actual migration rollback and absent schema
- **WHEN** 临时副本在真实6对象创建后注入失败，或Monitor API运行在schema5
- **THEN** 失败无Monitor表/索引/函数/trigger/applied6记录、旧状态保留；缺表API固定500无自动DDL

#### Scenario: Security - Guarded native Down
- **WHEN** 隔离PG18使用原生down从6到5，随后验证并可down-to0
- **THEN** 只移除Monitor对象及其数据，前七阶段数据保留；错误DB/应用库拒绝执行破坏性测试

#### Scenario: Concurrency - Historical migration boundaries
- **WHEN** 加入真实6后运行所有schema测试
- **THEN** 历史1–5使用明确up-to/down边界，原失败next-version6移至7并包含真实6副本；迁移suite串行且不掩盖历史故障

### Requirement: PG18 race and complete regression evidence is mandatory
验收 SHALL 包括Go单元、真实隔离PG18 integration、Race Detector、API文档对应及前七阶段回归。显式integration缺URL/PG18/固定Goose、不可达或误指应用DB SHALL 失败而非跳过。SHALL 用独立连接/屏障/数据库锁观测证明竞态，并验证配置端点零目标DNS/连接；计划不是已执行证据。

#### Scenario: Security - Required tenant privacy and no network matrix
- **WHEN** Owner/Member/outsider/双租户Actor替换路径/游标、保留失权Cookie、提交危险URL或敏感sentinel
- **THEN** 验证401/403/404/412和零越权写/数据/日志泄漏，配置流程没有目标解析/连接；URL单测只能证明输入策略

#### Scenario: Concurrency - Required wait and fault coverage
- **WHEN** 验收执行每级锁等待过期、两Owner版本竞争、父修改、移除/退出/角色/撤销/禁用/清理及未知Commit的两个顺序
- **THEN** PG18锁和数据证据配合normal/race全回归，不能用sleep-only、mock-only或Go race代替DB证明

#### Scenario: Security - Network detector positive control
- **WHEN** 隔离Linux环境人为制造违规DNS或连接尝试，随后运行真实Monitor配置端点
- **THEN** 探针准确记录违规尝试并使该运行审计非零失败，真实端点运行零目标尝试且审计成功；仅精确排除批准的PG/本地HTTP通信，保存可复验脚本、命令、退出码和证据，不以超时/不可达/无响应代替零尝试证明

### Requirement: Future probing requires a separately verified SSRF execution boundary
未来探测器 MUST 在每次解析/新连接时验证完整A/AAAA结果并绑定已验证IP，拒绝非公网/特殊用途/元数据/转换绕过，保留原域名TLS验证/SNI，禁止不受控代理和重定向，施加出口网络限制及资源预算。此为后续阶段必须验收的前置契约；本轮 SHALL 不实现DNS/连接或声称完成这些安全检查。

#### Scenario: Security - Rebinding TOCTOU and redirect escape
- **WHEN** 未来域名答案变化、混合公网与私网、AAAA嵌入特殊IPv4或HTTP重定向到内部资源
- **THEN** 执行期拒绝不安全答案/重定向、不得在已校验后让默认客户端重解析host；未来独立测试包含DNS变化、IP绑定、TLS、代理/redirect及出口阻断，当前静态URL测试不算这类证明

# http-monitor-persistence Delta

## ADDED Requirements

### Requirement: Migration seven creates constrained dormant intent without rewriting history
Goose7 SHALL 事务性新增独立activation状态、三元组PK/FK、状态/代次/时间CHECK、enabled计数及actor索引、初始化和配置失效保护。历史行 SHALL 回填disabled/g1，不生成任务。Migration1–6 SHALL 字节不变，支持0→7/存量6→7、失败回滚及隔离库Down；启动不迁移。

#### Scenario: Success - Fresh and populated upgrades
- **WHEN** 受保护可销毁PG18从0或存量6升级7并重复up
- **THEN** 每Monitor有一条disabled意图，原配置/来源/version/时间与前九阶段数据不变，无自动enable或next_due；原六份migration哈希相同

#### Scenario: Exception - Atomic migration and configuration rollback
- **WHEN** 真实7中途注入失败，或配置更新期间状态缺失/代次耗尽/失效trigger出错
- **THEN** 前者DDL/函数/trigger/索引/applied7全部回滚，后者配置与意图全部回滚；不能留下已改URL但旧授权有效的状态

#### Scenario: Security - Composite identity and ordinary SQL guards
- **WHEN** 尝试混合wid/pid/mid、修改子行身份/created_at、单独删除子行、设置非法state/version/g/actor组合或非NULL next_due_at
- **THEN** 数据库约束/保护拒绝；enabled绑定当前version由持Monitor锁的DML及配置失效保护保证，不能依赖跨表CHECK

#### Scenario: Security - Generation and cooling cannot be reset
- **WHEN** 普通SQL尝试实际改变状态却不令g恰好old+1、回退updated_at、清空/缩短已有冷却，或变更配置version不是old+1
- **THEN** 窄数据库guard拒绝且整体回滚，enabled要求actor/授权版本/冷却非空，不能用重置状态复活旧tag或绕过冷却

#### Scenario: Security - Guarded native Down and old schema
- **WHEN** 可销毁隔离库执行7→6及down-to0，或新意图API遇schema6
- **THEN** Down仅移除7对象并保持前九阶段配置数据；错误应用DB拒绝破坏测试，新API固定500且不自动DDL，共享发布串行迁移/前向修复

## MODIFIED Requirements

### Requirement: Monitors store minimal constrained configuration and provenance
Monitor SHALL 保存数据库生成UUIDv7 id、非空workspace_id/project_id/name/url/method/interval_seconds/timeout_seconds/expected_status/created_by_user_id/version/created_at/updated_at。默认值 SHALL 为GET/60/10/200、version1及相等数据库时间。配置表不存在状态/探测结果字段；独立activation子表 SHALL 仅保存意图，不保存探测结果；身份、双父归属、创建者、created_at SHALL 在普通SQL更新中不可变。

#### Scenario: Success - Database generated configuration
- **WHEN** 受保护的Owner创建最小有效配置
- **THEN** 数据库生成UUIDv7和默认值，creator来自受保护Session，不依赖客户端身份字段；同一事务初始化disabled意图子行，不创建可执行任务或结果记录

#### Scenario: Exception - Documented database validation subset
- **WHEN** 普通SQL提交必填NULL、空/超长名称、名称ASCII控制或ASCII边缘空白、空/超2048字节/含ASCII空白控制/非ASCII或不以规范http(s)://开头的URL、method非GET、interval不在枚举、timeout越界或不小于interval、status不在200–599、非正version或updated_at早于created_at
- **THEN** CHECK/NOT NULL拒绝；完整URL/punycode/Unicode名称策略由应用验证，数据库CHECK不宣称完整URL或SSRF保障

#### Scenario: Security - Identity and tenant immutability
- **WHEN** 普通SQL更新id/workspace_id/project_id/created_by_user_id/created_at为不同值，包括移到同Workspace的另一个真实Project
- **THEN** 不可变保护拒绝实际变更，等值赋值允许，不改变来源或授权依据

### Requirement: Composite references preserve project and monitor tenant ownership
Monitor SHALL 通过FK(workspace_id,project_id)引用projects(workspace_id,id)，creator引用users(id)，均DELETE RESTRICT且无Membership FK。SHALL 有PK(id)、UNIQUE(workspace_id,project_id,id)及creator索引，无name/url唯一约束。activation子表及未来运行子表 SHALL 以完整三元组引用Monitor，禁止跨租户组合；不以裸monitor_id的FK代替。

#### Scenario: Security - Mixed tenant parent cannot persist
- **WHEN** SQL组合Workspace A与Project B，即使两个UUID各自真实且actor同时属于两Workspace
- **THEN** 复合FK拒绝，正确组合成功；HTTP/Store仍独立执行实时授权，不以FK替代权限

#### Scenario: Success - Safe future child identity and list index
- **WHEN** activation子表及临时结果fixture以三元组引用Monitor，或按同一Workspace/Project及id范围读取
- **THEN** 正确引用成功，任何wid/pid/mid混合被拒绝，复合唯一索引同时支撑列表，无额外重复列表索引

#### Scenario: Exception - Independent creator lifetime and restricted deletion
- **WHEN** creator退出/被移除/禁用，或SQL试图删除被引用的Project/User
- **THEN** 前者不删除配置或延续creator访问权，后者RESTRICT防孤儿，无public删除API

#### Scenario: Concurrency - Foreign key protection
- **WHEN** 真实PG18独立连接顺序控制父删除与子插入
- **THEN** 正确FK锁保证有效引用或拒绝一方，不能提交孤儿；fixture遵循父先于子顺序且schema测试隔离

### Requirement: Monitor writes protect live authority before actual DML
每次写入 SHALL 在一个有界READ COMMITTED事务内保护User→Session→Workspace→Membership→Project→Monitor→Activation（创建无现存Monitor锁）。授权保护锁 SHALL 与既有写协议相容；全部保护等待之后的新数据库语句 SHALL 校验active身份、严格Session时效、Owner及完整归属并门控实际INSERT/UPDATE，包括独立意图写入，不能只信任Handler或锁前读取。

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
- **WHEN** Session在User/Session/Workspace/Membership/Project/Monitor/Activation任一保护锁等待期间过期
- **THEN** 最终新语句以expires_at>statement_timestamp()拒绝401；最终门控之后自然过期可提交，锁不冻结时间

### Requirement: Lock compatibility is verified with existing project and authority writers
Monitor写 SHALL 先取得User/Session SHARE、Workspace NO KEY UPDATE、actor Membership UPDATE、匹配Project UPDATE，再在PATCH/意图命令取得匹配Monitor UPDATE，最后取得Activation UPDATE。SHALL 避免在子锁后请求父锁和同一行弱锁升级；多个同类对象按稳定ID排序。真实PG18验收 SHALL 检查FK父/creator锁与既有Project、邀请、成员及清理路径，无全局API资源锁；初始化及配置失效trigger只按Monitor→Activation追加子锁，不反向请求业务父行。

#### Scenario: Concurrency - Project update and Monitor write
- **WHEN** 同Workspace的Project PATCH与Monitor POST/PATCH/activation PUT或不同Owner写同Monitor交错
- **THEN** 既有Workspace保护先串行化、父行锁在子行前；Project版本仅自身PATCH递增，配置PATCH检查monitor tag、意图PUT检查activation tag，均不消费Project tag

#### Scenario: Concurrency - FK locks and independent scopes
- **WHEN** FK隐式父键锁与显式Project/User保护交互，或不同Workspace有并发写
- **THEN** 已保护父锁无反向依赖/跨事务升级；独立Workspace不用全局锁，共用actor身份仍可能合法争用；异常死锁安全失败不自动重放

### Requirement: Monitor versions atomically guard complete configuration
PATCH SHALL 在完整三元组及当前version条件下更新允许字段、递增version并设置非递减数据库updated_at；最终有效配置 SHALL 重新校验，且同一Commit将意图disabled、清空授权版本并递增activation_generation。缺条件/旧条件/非Owner等失败不改变配置，bigint耗尽 SHALL 安全500不循环。未来读取者 SHALL 获得同一快照内的整条配置和version，不能混合不同版本字段；意图代次溢出/状态缺失 SHALL 使配置更新整体回滚。

#### Scenario: Concurrency - Two distinct Owners and ABA
- **WHEN** 不同Owner以同一版本更新，或配置A变B再变A后重放旧tag
- **THEN** 最多一个写消费原版本，其他412，等值更新也消费版本，旧意图不可因字段相等而恢复，配置成功同时撤销激活授权

#### Scenario: Success - Complete future configuration handoff
- **WHEN** 将来经评审的引擎读取Monitor
- **THEN** 按同一三元组读取不可撕裂的配置及version，未来结果须记录所用version并按独立执行期安全契约验证；本轮不实现或声称运行该读取者

### Requirement: Future probing requires a separately verified SSRF execution boundary
探测器 MUST 在每个获准新连接前验证完整有界A/AAAA/CNAME观察并绑定已验证IP，拒绝非公网/特殊用途/元数据/转换绕过，保留原域名TLS验证/SNI，禁止代理/重定向跟随，并独立验收出口及资源边界。配置API SHALL 继续零目标网络。本阶段 SHALL 仅增加独立版本激活意图与Migration7状态基础，真实执行仍只允许受控内部单次测试，无生产执行入口/扫描/领取/结果持久化；第八阶段历史不实现探测的契约与证据 SHALL 保留其历史含义。

#### Scenario: Security - Rebinding TOCTOU and redirect escape
- **WHEN** 域名答案变化、混合公网与私网、AAAA嵌入特殊IPv4或HTTP重定向到内部资源
- **THEN** 执行期拒绝不安全答案且不跟随重定向，不在已校验后让默认客户端重解析host；独立真实测试验证DNS变化、IP绑定、TLS、代理/redirect及出口阻断，静态URL测试或第八阶段零网络审计不能替代这些证明

#### Scenario: Success - Configuration remains dormant without new schema
- **WHEN** 本阶段部署、历史/新Monitor存在、配置被保存或更新
- **THEN** 无自动执行、真实触发API/Worker或结果表；Migration7仅有默认disabled意图，Migration1–6字节保持；内部执行只用隔离fixture，未来生产运行仍需独立评审及Owner重新确认

#### Scenario: Concurrency - Internal execution does not extend write transactions
- **WHEN** 系统按完整三元组在同一语句快照读完整配置/version并在释放DB资源后执行，Owner同时更新配置或用户权限变化
- **THEN** 原User→Session→Workspace→Membership→Project→Monitor保护与未知Commit语义不变，网络不持这些事务锁；旧结果明确属旧version而非新URL，未来子表仍需完整三元组和版本谱系

#### Scenario: Security - Internal library is not production egress approval
- **WHEN** 内部单次网络/PG测试通过但缺生产出口隔离/Worker和分布式调度评审
- **THEN** 不能公开运行探测服务或声称生产SSRF安全；单元、真实网络、PG18及独立出口证据分开报告，第八阶段配置API审计继续以原精确白名单验证零目标尝试

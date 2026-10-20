# Spec Delta

## MODIFIED Requirements

### Requirement: Monitors store minimal constrained configuration and provenance
Monitor SHALL 保存数据库生成UUIDv7 id、非空workspace_id/project_id/name/url/method/interval_seconds/timeout_seconds/expected_status/created_by_user_id/version/created_at/updated_at。默认值 SHALL 为GET/60/10/200、version1及相等数据库时间。配置表不存在状态/探测结果字段；独立activation子表 SHALL 仅保存意图，不保存探测结果；Migration8的授权/调度/run/资源账SHALL 使用独立表，不能混入配置；身份、双父归属、创建者、created_at SHALL 在普通SQL更新中不可变。

#### Scenario: Success - Database generated configuration
- **WHEN** 受保护的Owner创建最小有效配置
- **THEN** 数据库生成UUIDv7和默认值，creator来自受保护Session，不依赖客户端身份字段；同一事务初始化disabled意图子行，不创建可执行任务或结果记录

#### Scenario: Exception - Documented database validation subset
- **WHEN** 普通SQL提交必填NULL、空/超长名称、名称ASCII控制或ASCII边缘空白、空/超2048字节/含ASCII空白控制/非ASCII或不以规范http(s)://开头的URL、method非GET、interval不在枚举、timeout越界或不小于interval、status不在200–599、非正version或updated_at早于created_at
- **THEN** CHECK/NOT NULL拒绝；完整URL/punycode/Unicode名称策略由应用验证，数据库CHECK不宣称完整URL或SSRF保障

#### Scenario: Security - Identity and tenant immutability
- **WHEN** 普通SQL更新id/workspace_id/project_id/created_by_user_id/created_at为不同值，包括移到同Workspace的另一个真实Project
- **THEN** 不可变保护拒绝实际变更，等值赋值允许，不改变来源或授权依据

### Requirement: Future probing requires a separately verified SSRF execution boundary
探测器 MUST 在每个获准新连接前验证完整有界A/AAAA/CNAME观察并绑定已验证IP，拒绝非公网/特殊用途/元数据/转换绕过，保留原域名TLS验证/SNI，禁止代理/重定向跟随，并独立验收出口及资源边界。配置API SHALL 继续零目标网络。Migration8 SHALL 在独立有限授权和fixture许可下允许受控周期领取/结果持久化，无生产执行入口。第八至十阶段的休眠契约/迁移与历史证据 SHALL 保留其原阶段含义，不作为新闭环或生产验收。

#### Scenario: Security - Rebinding TOCTOU and redirect escape
- **WHEN** 域名答案变化、混合公网与私网、AAAA嵌入特殊IPv4或HTTP重定向到内部资源
- **THEN** 执行期拒绝不安全答案且不跟随重定向，不在已校验后让默认客户端重解析host；独立真实测试验证DNS变化、IP绑定、TLS、代理/redirect及出口阻断，静态URL测试或第八阶段零网络审计不能替代这些证明

#### Scenario: Success - Configuration remains dormant without new schema
- **WHEN** 普通发布部署、历史/新Monitor存在、配置被保存或更新且未获新有限授权
- **THEN** 无自动执行或生产入口；Migration7保留休眠保护，Migration1–7字节保持；Migration8独立表仅在新授权/许可下支持隔离周期fixture，生产仍需另行部署评审

#### Scenario: Concurrency - Internal execution does not extend write transactions
- **WHEN** 系统按完整三元组在同一语句快照读完整配置/version并在释放DB资源后执行，Owner同时更新配置或用户权限变化
- **THEN** 原User→Session→Workspace→Membership→Project→Monitor保护与未知Commit语义不变，网络不持这些事务锁；旧结果明确属旧version而非新URL，独立子表必须完整三元组和run/v/g/epoch/fence谱系

#### Scenario: Security - Internal library is not production egress approval
- **WHEN** 内部单次网络/PG测试通过但缺生产出口隔离/Worker和分布式调度评审
- **THEN** 不能公开运行探测服务或声称生产SSRF安全；单元、真实网络、PG18及独立出口证据分开报告，第八阶段配置API审计继续以原精确白名单验证零目标尝试

## ADDED Requirements

### Requirement: Migration eight revokes legacy intent atomically without weakening guards
Goose8 SHALL 在Worker入口前事务性撤销旧enabled/授权版本，并令全部存量Activation含disabled的g恰好+1一次，失效表示协议升级前的强tag，冷却逐值不变。新增独立授权/调度/run/预算，Migration1–7字节不变；原身份/g/冷却/DELETE/TRUNCATE及activation.next_due=NULL保护 SHALL 保留，独立schedule.next_due安全演进。

#### Scenario: Success - Fresh and populated safe upgrade
- **WHEN** guarded PG18执行0→8或含disabled/enabled/冷却中Monitor的7→8及up/up
- **THEN** 旧enabled撤销，旧disabled保持disabled；所有存量行g恰好+1、reason=upgrade_revoked/actor=NULL、授权NULL且冷却不变，使两个旧表示常量的tag全部失效；无run/有效schedule许可，配置/来源/version保留，up/up不重复消费代次

#### Scenario: Exception - Full migration rollback and exhaustion
- **WHEN** 真实Migration8对象/部分回填完成后注入故障，或任何存量enabled/disabled的g及所需epoch耗尽
- **THEN** DDL/函数/权限/回填/Goose8记录全部回滚，旧数据不部分升级，Worker始终关闭；不删除guard绕过失败

#### Scenario: Concurrency - Old API transactions straddle upgrade
- **WHEN** 旧API作为非接流量DB写者在迁移表锁前已提交、锁后继续enabled或与新Owner授权共存
- **THEN** 前者被原子回填撤销，后者不能写新授权；任意g变更触发清schedule许可/推进epoch，旧API最多撤销新许可，绝不自动授予

#### Scenario: Security - Representation cutover does not reuse a strong validator
- **WHEN** 升级新Activation弃用提示协议或在物理停止后回退旧HTTP表示
- **THEN** 排空旧HTTP及写者、令全部存量g+1且不重置冷却后才切表示，任一步失败不开fixture gate；新旧HTTP不混合服务可运行Monitor，旧DB写者仍不能授予执行，历史schema7原契约及Migration1–7保持不变

#### Scenario: Security - Privileged legacy connections block rollout
- **WHEN** 旧API拥有superuser或可直接写新表/执行授予函数的角色且未排空
- **THEN** 拒绝Worker发布；混合版本保证仅对经最小权限隔离的旧角色成立，不能声称触发器约束管理员

#### Scenario: Security - Disposable down never resurrects authorization
- **WHEN** 已物理停止全部Probe的guarded testdb执行8→7→8
- **THEN** 仅移除8对象/权限并恢复7约束，必要时将upgrade_revoked转换为7兼容disabled原因且推进g，保留冷却，不复活旧enabled；共享环境只前向修复，API不自动DDL

### Requirement: Scheduler persistence separates owner changes from operational updates
调度心跳/next_due/lease/result SHALL 不推进Owner的activation_generation或配置version。所有子表 SHALL 绑定完整三元组，状态/时间/正版本与单调fence/epoch有数据库保护；跨表最新版本检查 SHALL 在父锁后新语句完成。持久记录不复制URL或DNS答案。

#### Scenario: Concurrency - Operational updates do not invalidate Owner validators
- **WHEN** Worker续租、存结果、推进下一due或回收资源
- **THEN** 原Owner的v/g条件不因后台更新时间失效；Owner命令仍消耗g并撤销旧schedule授权；状态时间不回退

#### Scenario: Security - Shape and scope violations fail in PostgreSQL
- **WHEN** 普通SQL混合租户、改run谱系、伪造状态时间/HTTP status、复用旧fence或创建同scope第二个未释放run
- **THEN** 约束/窄函数拒绝，网络仍不可获得许可；原有身份和配置revision保护不放松

#### Scenario: Concurrency - Parent first locks remain compatible
- **WHEN** 用户配置/意图授权、系统Claim/Start/Result/清理及FK隐式锁交错
- **THEN** 用户原User→Session→W→Membership→P→M→Activation顺序保留，后台使用相容父子序，禁止先Run/Activation后W/M；网络阶段无业务锁/连接

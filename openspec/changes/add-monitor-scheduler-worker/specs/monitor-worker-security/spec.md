# Spec Delta

## Purpose

定义周期监控在控制面、数据库身份、无凭据Probe与独立网络出口之间的安全边界，保证部署许可、资源回收和跨租户目标预算可验证，明确隔离测试证据的适用范围及生产缺证时的强制关闭行为。

## ADDED Requirements

### Requirement: Probe identity is isolated from users and database control
系统 SHALL 分离API、Scheduler Controller、最小权限Worker控制面、Probe、Resolver及出口Supervisor。Probe SHALL 无用户/会话/DB/云凭据或Docker Socket，仅获单run冻结快照和有界单次管道许可；后台身份 SHALL 不使用Cookie作为权限。

#### Scenario: Security - Probe compromise does not expose control credentials
- **WHEN** 检查Probe环境、mount、进程权限和可达网络
- **THEN** 无Cookie/Session摘要/数据库密码/云凭据/源码配置卷/管理socket；非root、caps0、no_new_privs，无法改规则或访问PG

#### Scenario: Security - Least privileged roles cannot bypass state transitions
- **WHEN** Worker角色尝试读Session/password、改配置、DDL、直接写run或伪造实例身份
- **THEN** 权限拒绝；只允许绑定session_user/incarnation的窄状态函数，固定可信search_path、无PUBLIC执行/CREATE/TEMP，旧API无新授权函数权限

### Requirement: Production execution stays hard closed without deployment evidence
本Change SHALL 只提供隔离fixture测试构建入口；普通发布产物无Probe启动路径，API无执行内核依赖。生产真实出口 SHALL 保持关闭；环境变量、历史CI或fixture namespace不能成为许可。未来开放须另行生产拓扑/出口验收及明确授权。

#### Scenario: Security - Environment flag cannot unlock a probe
- **WHEN** 设置EGRESS_APPROVED=1、启动普通API或存在旧enabled/新授权
- **THEN** 不能产生生产DNS/TCP/GET；缺可信fixture-only部署身份时返回blocked，无API内回退执行

#### Scenario: Security - Fixture runner is not a production worker image
- **WHEN** 受控runner在新建guarded PG18与无上游fixture拓扑执行多个周期
- **THEN** 只证明该测试镜像/namespace/规则；不能连接生产库或目标，也不能把同一镜像当生产入口交付

### Requirement: Network permissions default deny and fail closed across lifecycle
获许可Probe SHALL 受独立双栈默认拒绝出口保护，仅精确Resolver TCP53及受控TCP80/443；规则须在进程启动前生效，许可绑定节点/namespace/部署代次和硬截止。规则漂移/丢失、未知路由或不可验证转换 SHALL 拒绝新执行，运维停止须独立于API/PG。

#### Scenario: Security - Application policy bypass is independently blocked
- **WHEN** 恶意控制绕过应用分类直连IPv4/IPv6 metadata、集群/组织内部公网、DNAT或转换fixture
- **THEN** 独立规则阻断，接收端零GET，记录违规attempt及预期audit失败；不把connect失败当零尝试

#### Scenario: Security - Routing and node continuity are required evidence
- **WHEN** 存在未知NAT64/透明代理/目的NAT/隧道、规则被删、节点迁移或扩缩容
- **THEN** 无独立默认deny连续性与旧epoch不可恢复证明就保持blocked；重建节点不能重用旧permit，只有轮询漂移不构成无窗口保证

#### Scenario: Security - Exact resolver exception never authorizes target access
- **WHEN** DNS回答其专用resolver私有IP，或Probe尝试其它resolver/协议
- **THEN** target拒绝且无80/443访问；仅批准的固定IP/TCP53可以通信，无系统fallback

### Requirement: Physical resource release requires independent termination proof
租约、心跳、数据库fence或Worker取消 SHALL 不被当作物理网络终止证明。started/unknown的并发资源 SHALL 保持quarantine，直到独立Supervisor确认旧namespace/permit代次不可恢复、现有流已切断及进程结束。无证明 SHALL 不返还槽或启动同Monitor下一run。

#### Scenario: Concurrency - Expired lease leaves a live zombie
- **WHEN** Worker分区、租约过期但Probe仍可能发包
- **THEN** 拒绝旧fence更新/新许可，保留所有物理槽；独立切断已有连接/conntrack并确认终止后才能回收

#### Scenario: Exception - Database and supervisor are unavailable
- **WHEN** DB断连且监督器也无法给出终止回执
- **THEN** 不续网络permit，硬截止独立生效；仍不凭时间返还配额，安全停止后对账恢复，无旧run重发

#### Scenario: Security - Emergency stop affects established flows
- **WHEN** 运维在TLS建立后触发独立紧停
- **THEN** 同时阻止新流及已有流写入、终止Probe；仅删NEW规则或只停Controller不算通过；已到远端请求不能撤回

### Requirement: Cluster admission reserves shared concurrency and rate atomically
共享预算 SHALL 原子限制集群32/W4/P2/完整M1槽及global8/s burst8、W1/s burst2、P每2s一次burst1；Monitor最早开始时间 SHALL 不早于旧限制及last_started+当前interval。单进程Admission继续保留；无锁count、每Monitor常驻goroutine或租约TTL释放不能代替共享账。

#### Scenario: Concurrency - Many workers reserve the last capacity
- **WHEN** 多实例竞争最后global/W/P/M槽或token
- **THEN** 条件更新/锁保证不超额、失败零目标TCP；已接受Start消耗速率不因失败/取消/新g/重启退还

#### Scenario: Concurrency - Fair bounded work under simultaneous due tenants
- **WHEN** 1000Workspace同时到期或24h积压恢复
- **THEN** 有界轮转扫描，每tick最多32候选尝试、每W最多领取1次，积压不逐次补偿；测量饥饿/等待且不声称无限负载严格公平

### Requirement: Shared public target budgets protect privacy without claiming complete abuse prevention
同canonical origin跨租户 SHALL 共享1槽及60秒一次/burst1，同选中IP跨origin SHALL 共享2槽及1/s burst1。预算key SHALL 使用控制面HMAC、排除Path且不持久原始DNS/IP；目标permit必须在完整DNS验证后、literal连接前取得。A SHALL 仅允许预登记fixture目标。

#### Scenario: Concurrency - Same target appears across workspaces or aliases
- **WHEN** 不同W相同origin同时执行，或不同域名解析到同一IP
- **THEN** 共享目标预算阻止额外访问，拒绝只返回固定resource_limited，不向用户暴露别的租户、目标hash或占用情况

#### Scenario: Exception - Target permit acknowledgement is uncertain
- **WHEN** DNS后许可管道/预算事务/监督器确认丢失或超时
- **THEN** 不拨目标TCP、不重试许可导致重复GET，回调时间仍计入原整体timeout，预订资源按物理终止协议回收

#### Scenario: Security - Key rotation and alias abuse do not imply production approval
- **WHEN** HMAC密钥轮换、攻击者多域名/多IP轮转或共享CDN存在
- **THEN** 活跃槽先排空、旧预算至少保留1h；承认识别共同运营者的局限，无目标同意/允许目录及可验组织预算则生产公开执行关闭

### Requirement: Acceptance separately verifies database network fault and production boundaries
验收 SHALL 保留原76项受控执行安全测试、配置/激活零目标审计与PG18 normal/race，增加多实例/物理kill/重建/配额/凭据隔离实测。证据 SHALL 绑定准确实现SHA；缺IPv6/namespace/PG18依赖的显式job须失败。生产验收 SHALL 独立标记未验证。

#### Scenario: Security - Dedicated scheduler topology has independent witnesses
- **WHEN** 运行新的Linux/PG18多Worker周期runner
- **THEN** Probe与控制面/PG分网，独立DNS/HTTP/TLS接收端、规则/路由/cgroup/进程trace和正负控制验证实际行为，不复用API目标白名单掩盖探测

#### Scenario: Exception - Planning or old successful artifacts are insufficient
- **WHEN** 只有本次Propose校验、单元/mock或旧归档证据
- **THEN** 新PG并发、网络/故障及生产项均不能标记已通过；生产执行保持关闭

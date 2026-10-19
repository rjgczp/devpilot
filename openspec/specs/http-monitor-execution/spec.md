# http-monitor-execution Specification

## Purpose

定义 DevPilot HTTP Monitor 的内部单次执行契约，覆盖受信配置快照、执行期 DNS/目标连接/TLS 安全、资源上限、结果版本和分层安全验收；执行能力不构成激活授权，也不替代独立出口隔离或创建任何生产探测入口。

## Requirements

### Requirement: Single execution is internal and never activates configuration
系统 SHALL 仅通过受保护内部测试能力调用单次执行器，不增加生产HTTP/CLI/cron/启动/调度/Worker入口。历史与新建/修改Monitor SHALL 保持不执行；配置API、schema6及响应DTO SHALL 不增加启停/结果语义。interval SHALL 不被用来自动扫描或安排任务。

#### Scenario: Success - Deployment preserves inactive history
- **WHEN** schema6存在大量历史与新Monitor且API启动或重启
- **THEN** 无任何Monitor目标DNS/连接，仍只有既有Session清理Worker，保存配置不表示启动

#### Scenario: Security - No public or hidden trigger
- **WHEN** 请求手动运行/激活/暂停路径、传入环境开关或检查API依赖图
- **THEN** 不存在执行入口，API不依赖执行器包，路由/API白名单和原配置零网络契约保持不变

#### Scenario: Success - Controlled invocation without production targets
- **WHEN** 内部测试在独立隔离环境显式选择自己的完整scope和fixture
- **THEN** 可以验证一次真实执行与内存结果，不枚举或探测生产Monitor，不创建任何生产结果或启停状态

### Requirement: System snapshots bind complete scope and configuration version
系统读取 SHALL 由独立可信内部身份在同一数据库语句快照按完整Workspace/Project/Monitor归属读取全部执行字段与version，取回后离线校验并冻结。读取 SHALL 在2秒内关闭Rows/释放DB连接，网络I/O SHALL 不持有事务/业务行锁。裸Monitor ID或用户Cookie断言 SHALL 不授予系统执行能力。

#### Scenario: Security - Mixed scope and invalid stored configuration
- **WHEN** 组合真实但不匹配的wid/pid/mid、只提供mid，或SQL存入违反完整URL策略的配置
- **THEN** 前者不能取得snapshot，后者返回invalid_configuration；目标DNS/连接均为0，不能通过无scope查询补查存在性

#### Scenario: Concurrency - Snapshot never tears fields
- **WHEN** Owner在读取语句开始前或之后提交URL/timeout/expected_status及version更新
- **THEN** 单次读取只取得完整旧版或新版，不混合字段；查询返回后不保证仍是最新配置

#### Scenario: Security - Independent least privileged identity
- **WHEN** 受控PG18系统role读取配置并尝试写配置、读Session/User/Membership或执行DDL
- **THEN** 仅规定配置列/归属读取获准，其余被数据库权限拒绝；probe子进程不持DB凭据或用户Cookie，本轮不配置生产Worker账号

### Requirement: Every permitted target connection validates bounded complete DNS observations
每个获准目标新连接 SHALL 使用固定受控resolver重新查询绝对FQDN的A/AAAA及完整CNAME链，先完成全部校验才拨号。SHALL 不依赖系统resolver/search/hosts、不拼接独立查询形成虚假原子链、不跨调用缓存批准结果。任一危险地址 SHALL 整体拒绝，不能只选安全子集。

#### Scenario: Security - Mixed answers and hidden late record
- **WHEN** 任一报文中A/AAAA混合公私网、最后一个地址危险，或另一家族/CNAME终端给出危险地址
- **THEN** 返回blocked_target，无目标TCP尝试或HTTP/TLS负载，允许的专用DNS通信不被误认为目标探测

#### Scenario: Security - Alias consistency and bounded visibility
- **WHEN** CNAME有环/冲突、地址与别名共存、A/AAAA链或终端矛盾、DNAME不受支持，或alias为保留/非法域名
- **THEN** 链/协议故障dns_failed、目标策略拒绝blocked_target，不采用部分答案；不能宣称穷举域名所有未来/地域轮转地址

#### Scenario: Success - Legitimate native single family and zero TTL
- **WHEN** 两家族完成查询，一家族明确NOERROR/NODATA而另一家族有完整安全终端地址，或这些答案TTL为0
- **THEN** 可以立即绑定本次批准的一个原生IPv4/IPv6地址，不能跨连接复用批准集

### Requirement: DNS failure and message resource limits fail closed
DNS SHALL 仅连接一个运营固定IP的TCP53，不隐式切resolver/重连。DNS阶段共享最多2秒，最多8别名跳转/18次消息交换，单消息16KiB、累计64KiB、RR128、终端去重IP32。SHALL 验证消息身份/question/完整性，超限或无法确认完整答案时拒绝，不截取前N记录继续。

#### Scenario: Exception - Failure empty and incomplete families
- **WHEN** NXDOMAIN/SERVFAIL/REFUSED、两家族均空、截断/错误报文、question不匹配、任一家族出错或不完整
- **THEN** dns_failed且无目标连接，不把失败家族当NODATA，也不使用之前安全缓存答案

#### Scenario: Exception - Shared timeout and bounded allocation
- **WHEN** DNS不响应、CNAME连续慢答、报文宣称超长或记录/地址数超限
- **THEN** DNS超时为timeout，其余为dns_failed，在分配大消息前拒绝；所有查询累计不超过DNS与整体剩余预算

#### Scenario: Success - DNS infrastructure exception is narrow
- **WHEN** resolver使用专用基础设施私有IP且目标DNS回答该IP
- **THEN** 仅resolver TCP53通道获准，该地址仍作为target被blocked_target拒绝，例外不允许其80/443访问

### Requirement: Target policy rejects special use and translation addresses
允许目标 SHALL 为审核策略认可的原生普通公网候选，同时排除IANA特殊用途/保留空间及运营deny。SHALL 拒绝private/loopback/link-local/metadata/multicast、mapped/compatible、NAT64/6to4/Teredo/已知嵌入格式；IPv6只考虑2000::/3内非特殊原生地址。SHALL 不仅依赖IsGlobalUnicast，策略来源/版本 SHALL 可审查。

#### Scenario: Security - IPv4 and IPv6 evasion matrix
- **WHEN** 地址为0/8、RFC1918、100.64/10含100.100.100.200、127/8、169.254/16、文档/基准/保留/组播、ULA/link/site-local、zone、mapped IPv4、64:ff9b::/96、64:ff9b:1::/48、2002::/16、2001::/32或已知ISATAP格式
- **THEN** 即使普通全局单播判断为true或嵌入公网IPv4，也不连接；prefix首末/相邻与地址表示差异有独立测试

#### Scenario: Security - Special purpose is denied even if globally reachable
- **WHEN** IANA特殊用途项标记Globally Reachable=true、普通公网形状地址命中环境内部/转换deny，或分配/策略状态不能确认
- **THEN** 不豁免该地址，返回blocked_target；不能因只看注册表某个布尔值而放行

#### Scenario: Security - Unknown translation is a deployment boundary
- **WHEN** 自定义NAT64/6rd/隧道或目的NAT可把看似原生公网地址转向内部，且环境无法排除/阻断
- **THEN** 生产出口准入失败，不能宣称应用分类能识别任意转换前缀；隔离测试仍须证明网络最后防线阻断危险fixture

### Requirement: Actual dialing is pinned to one approved literal destination
目标连接 SHALL 直接拨号本次resolver给出且全部校验通过后选中的一个literal IP及配置默认端口，不把域名交给默认Dial。SHALL 使用IPv4优先/家族内升序的确定选择；每次最多一个目标TCP尝试，无happy-eyeballs/fallback/重试。建立后 SHALL 在TLS/HTTP写入前核对RemoteAddr精确匹配批准IP/port。

#### Scenario: Security - DNS rebinding between validation and dialing
- **WHEN** DNS验证时返回安全P、拨号前改为危险Q
- **THEN** 实际只可能拨号P，不进行默认host重解析；后续独立调用必须重查并拒绝Q，证据包含真实DNS及socket目标

#### Scenario: Security - Remote address mismatch and extra dial
- **WHEN** 已连接RemoteAddr不符/不可确认、origin被替换或transport尝试第二次目标dial
- **THEN** 关闭或拒绝连接，额外尝试不访问任何目标，TLS/HTTP负载不会发送到未批准目标

#### Scenario: Exception - One safe IP fails while another exists
- **WHEN** 完整答案都安全但确定选中的IP拒绝连接或超时
- **THEN** 返回connect_failed或timeout，不自动切换至另一个IP、不重复GET，后续显式调用须重新admission和完整DNS校验

### Requirement: HTTPS verifies the original hostname on the pinned connection
HTTPS SHALL 在批准的TCP连接上使用原URL域名SNI及标准证书链/主机名校验、TLS>=1.2，不能以IP/CNAME替换验证名或使用InsecureSkipVerify。SHALL 显式执行有界握手、保留整体deadline；测试CA/连接注入 SHALL 只存在受控测试路径，不作为用户能力。

#### Scenario: Success - SNI and hostname survive IP pinning
- **WHEN** 域名经CNAME解析到批准fixture IP且证书有效
- **THEN** 实际连接该IP，TLS的SNI及HTTP Host仍为原URL域名，真实TLS fixture观察到正确值并可成功

#### Scenario: Security - Invalid identity or hidden TLS lookup
- **WHEN** 证书过期/链不可信/hostname错误，或仅IP/CNAME的证书有效
- **THEN** tls_failed且不发送HTTP；无证书网络取回/用户client cert，无跳过验证或非批准连接

#### Scenario: Exception - Custom TLS dialing is still bounded
- **WHEN** 使用自定义TLS dial且服务端不完成握手或发送过量握手数据
- **THEN** 实际握手受3秒/整体剩余及128KiB原始读取限制，分别timeout或response_limit；不能只依赖不生效的transport默认TLS选项

### Requirement: HTTP execution has no ambient credentials proxies redirects or reuse
执行 SHALL 仅发送无body的GET、固定最小headers及原域名Host，不携带Cookie/Authorization等入站数据。SHALL 禁止所有环境/用户代理、重定向跟随、连接池复用、TLS session cache、隐式重试与HTTP升级；只使用HTTP/1.1。一次调用 SHALL 最多发送一个HTTP请求。

#### Scenario: Security - Every redirect is terminal
- **WHEN** 首跳返回301/302/307/308，Location指向内部IP/域名、其它协议或多跳
- **THEN** 不解析Location、不对其DNS/连接，首跳按配置精确比较状态；expected_status=302且实际302可success

#### Scenario: Security - Proxy and credential contamination
- **WHEN** 设置HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY及小写变量，或调用环境有Cookie/Authorization/client cert
- **THEN** 实际连接仍是批准目标，代理fixture收不到连接/CONNECT，出站请求不含敏感凭据

#### Scenario: Security - New probe means a new independently checked connection
- **WHEN** 连续调用同域名、目标支持keep-alive/HTTP2或返回Alt-Svc/升级响应
- **THEN** 不复用上次连接/批准集、不打开替代连接；下一次新建连接完整解析，101为protocol_error

### Requirement: Status checking never reads response bodies and bounds protocol input
探测 SHALL 只对完整最终响应头的200–599状态与expected_status精确比较；body应用读取预算为0，不drain/解压/保存。最终header最多32KiB，累计HTTP明文底层读取最多64KiB（含1xx和read-ahead），最多4个非终端1xx，读取必须先受限再分配/解析。结束 SHALL 关闭所有响应/连接资源。

#### Scenario: Success - Huge slow compressed or infinite body
- **WHEN** 完整最终头状态匹配，而body巨大/慢速/无限/chunked或gzip bomb
- **THEN** 可以返回头部success并立即关闭，无body读取/解压/排空；不声称响应体完整或没有任何parser预取

#### Scenario: Exception - Headers informational flood and invalid protocol
- **WHEN** 最终header/累计流/1xx计数超限、headers缓慢或HTTP无效/升级
- **THEN** 分别response_limit、timeout或protocol_error，累计预算不因1xx重置，活动连接被回收

#### Scenario: Exception - Unexpected status preserves safe final status
- **WHEN** 完整最终状态与快照expected_status不符
- **THEN** success=false/failure_code=unexpected_status并可记录该数字状态，不读取body/Location或泄漏原始headers

### Requirement: Probe deadlines share the saved configuration timeout
admission成功后的整体deadline SHALL 为min(parent deadline,start+snapshot.timeout_seconds)，T=1–30秒覆盖DNS/TCP/TLS/请求写/响应头与结束。各阶段 SHALL 取固定上限与剩余最小值：DNS2秒、TCP3秒、TLS3秒、写完后headers5秒。取消 SHALL 及时关闭连接和回收资源，不能重置整体T或阻塞API进程。

#### Scenario: Exception - Small saved timeout overrides stage defaults
- **WHEN** timeout_seconds=1而DNS/TCP/TLS/headers串行缓慢
- **THEN** 整次受同一个1秒deadline约束，不能累加阶段上限；明确phase timeout只返回固定安全分类

#### Scenario: Concurrency - Cancellation and terminal cause
- **WHEN** parent取消、deadline和最终响应竞态
- **THEN** 尚未终止的执行按记录的明确terminal cause稳定分类并关闭资源，已完成结果不被后续取消改写；不声称撤回已到远端的GET

### Requirement: Admission enforces bounded tenant consumption without queues
admission SHALL 在单进程原子限制active总数32、Workspace4、Project2、完整scope Monitor1，无等待队列或每拒绝请求新goroutine。速率 SHALL 至多全局32/s burst32、Workspace4/s burst4、Project2/s burst2及Monitor interval冷却；scope/cooldown状态合计最多1024。满额 SHALL 拒绝且零目标网络，完成/取消 SHALL 释放active资源。

#### Scenario: Concurrency - Nested limits and independent scopes
- **WHEN** 同Monitor重复、同Project/Workspace竞争及多个租户并发
- **THEN** 32/4/2/1均不能突破，按完整归属分别计数；单租户至多4槽，另一租户可在剩余容量和自身预算内完成，不能冒称无限多租户下严格公平

#### Scenario: Security - Cooldown exhaustion and version changes
- **WHEN** 上次调用失败/blocked/canceled、随后改version/缩短interval，或1024记录容量满
- **THEN** 已接受尝试仍消耗速率，next_allowed不早于旧界及last_start+新interval；last_start至少保留3600秒，未过期/active记录不被逐出绕过限制，容量满无网络拒绝

#### Scenario: Exception - Saturation cleanup and deployment scope
- **WHEN** 大量慢响应/取消/错误耗尽槽，或未来增加多个执行进程
- **THEN** 无无界连接/goroutine，所有终止路径回收槽/FD；当前上限仅单进程，多实例不能据此宣称分布式配额已完成

### Requirement: Results are immutable private observations of one configuration version
内部内存结果 SHALL 包含完整scope、configuration_version、UTC started_at/finished_at、单调duration、可空最终http_status、success和可空固定failure_code。旧版本已开始执行 SHALL 可按旧snapshot完成，不改变当前Monitor状态；后续读取取得新snapshot。SHALL 不持久化/公开结果，不把旧结果解释为新URL健康。

#### Scenario: Concurrency - Configuration changes during execution
- **WHEN** v7任务运行时Owner将URL/timeout/expected_status改为v8
- **THEN** v7结果仍属v7并按v7参数判定，新任务使用完整v8；旧结果不会更新v8的当前健康或覆盖其配置

#### Scenario: Concurrency - User authority loss is separate from system execution
- **WHEN** 用户退出/被移除、Session撤销/清理或账户禁用在系统snapshot之后提交
- **THEN** 新用户API依既有契约失权，系统已取得的旧snapshot可完成；不缓存或复用用户Cookie授权，父context取消仍停止执行

#### Scenario: Success - Future persistence must preserve lineage
- **WHEN** 后续独立Change持久化执行结果或展示当前健康
- **THEN** 必须保留完整scope/run身份和配置版本/必要配置快照，子引用用三元组，当前展示显式匹配version及运行代次；本轮不提前建表

### Requirement: Failure classification and diagnostic output are safe and deterministic
固定失败码 SHALL 区分invalid_configuration、dns_failed、blocked_target、connect_failed、tls_failed、timeout、canceled、response_limit、protocol_error、unexpected_status；未启动的配置预检/scope读取/admission拒绝 SHALL 不伪造目标结果。结果/日志 SHALL 不含URL任何部分、DNS/IP/CNAME、原始error、headers/body、证书、凭据或秘密；常规日志不带租户资源ID/高基数label。

#### Scenario: Security - Secret and network detail sentinels
- **WHEN** URL path、DNS答案、TLS错误/HTTP头/body或panic中含秘密sentinel
- **THEN** 结果只含规定ID/版本/时间/状态/固定分类，普通日志无sentinel/网络细节，内部ID只用于受信断言，不向用户提供新DTO

#### Scenario: Exception - Terminal classification and uncertain delivery
- **WHEN** 结构化策略/字节限错误、明确取消/deadline、TCP拒绝/TLS失败或执行后丢失结果
- **THEN** 保留结构化block/limit，其余按记录的取消/超时/阶段分类而非error字符串；零自动重试，结果丢失不表示远端没有收到GET

#### Scenario: Concurrency - Existing unknown configuration commit is unchanged
- **WHEN** 原配置POST/PATCH Commit未知后系统重新读取，或内部调用被显式重复
- **THEN** 只按数据库新快照的已提交配置执行，原写无自动重放/网络补偿；新调用仍需admission/DNS，不承诺跨进程exactly-once

### Requirement: Egress isolation is a separate mandatory security boundary
应用IP检查 SHALL 不被当作出口隔离替代。真实probe SHALL 在无特权独立进程/namespace中运行，无API/DB/云凭据，出口默认deny，仅专用resolver与批准的TCP80/443。IPv4/6、内部公网CIDR及转换/目的路由 SHALL 独立约束。缺必要出口保证 SHALL 不提供或宣称安全的公开探测服务。

#### Scenario: Security - Network control survives application check bypass
- **WHEN** 测试恶意probe刻意绕过分类器直连危险fixture
- **THEN** 网络规则独立阻断，审计记录违规attempt且fixture无HTTP负载；child退出0不等于审计通过

#### Scenario: Security - Controlled topology never reaches metadata or production
- **WHEN** 测试模拟私网/metadata/普通公网形状/转换目标与恶意DNS
- **THEN** 所有地址仅在无上游路由的fixture命名空间，不能实际访问云metadata、宿主/生产内网；测试子进程不能修改防火墙

#### Scenario: Exception - Missing network proof blocks acceptance
- **WHEN** 缺IPv6、namespace权限、路由/防火墙证据或只有容器/单元成功
- **THEN** 对应安全验收失败或明确未运行，不能给生产SSRF安全结论，未来生产拓扑须独立复验

### Requirement: Acceptance reports unit network database and egress evidence separately
验收 SHALL 分别报告静态/单元、受控Linux真实DNS/HTTP/TLS、真实隔离PG18及独立出口证明，记录准确实现SHA/工具链/命令/exit/正向控制。显式job缺依赖 SHALL 失败而非skip。第八阶段配置API零目标审计/精确白名单及构建来源守护 SHALL 原样持续通过，不能扩大为probe白名单。

#### Scenario: Security - Required real network matrix
- **WHEN** 执行验收
- **THEN** 独立DNS/HTTP/TLS fixture验证两次DNS变化/混合RR/特殊地址/TOCTOU/SNI/证书/全部redirect/proxy/慢大响应/取消/deadline/配额，危险目标零TCP尝试及零HTTP负载有真实观测，wrapper/mock证据另标

#### Scenario: Concurrency - Required PG18 and regression evidence
- **WHEN** 运行完整normal/race integration
- **THEN** PG18屏障/锁观测证明scope快照/版本并发及网络期间无长事务，用户权限变更与原写锁协议回归通过，Migration1–6哈希/schema6保持；无Migration7

#### Scenario: Security - Separate detector calibration and historical audit
- **WHEN** 新执行器runner允许DNS/fixture网络并运行，第八阶段配置API审计也运行
- **THEN** 两者独立runner/策略/证据目录；分别有准确positive control和预期attempt断言，旧配置审计仍零未批准attempt，不能以新网络白名单掩盖配置API出站

#### Scenario: Exception - Planning and missing infrastructure are not passes
- **WHEN** 仅有本次Propose/文档校验、旧归档通过、mock/unit或网络环境缺失
- **THEN** 不能标记新真实网络/PG18/出口测试通过，安全门禁保持未通过，托管CI另按最终实现SHA验收

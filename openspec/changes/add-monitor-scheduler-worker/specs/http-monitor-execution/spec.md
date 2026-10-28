# Spec Delta

## MODIFIED Requirements

### Requirement: Single execution is internal and never activates configuration
系统 SHALL 仅在受保护无上游fixture部署通过内部单次/周期Worker能力调用原执行器，不增加生产HTTP/CLI/cron/Worker入口。旧/普通activation意图与Migration7 SHALL 不作为执行许可；配置保存不启动探测，原配置DTO不增加状态/结果。独立有限授权及部署许可均有效时，隔离Scheduler才可按interval安排任务。

#### Scenario: Success - Deployment preserves inactive history
- **WHEN** schema6或schema7存在大量历史与新Monitor且API启动或重启
- **THEN** 无任何Monitor目标DNS/连接，普通发布仍只有既有Session清理Worker；没有Migration8新授权与fixture许可时保存配置不表示启动

#### Scenario: Security - No public or hidden trigger
- **WHEN** 请求手动运行/未定义暂停路径、调用activation意图子资源、传入环境开关或检查API依赖图
- **THEN** 不存在生产真实执行入口，API不依赖执行器包；新旧配置/意图/独立授权/status端点均零目标网络，执行开关不能解锁

#### Scenario: Success - Controlled invocation without production targets
- **WHEN** 内部测试或隔离周期runner显式选择自己新建测试库的完整scope和fixture
- **THEN** 可以验证单次或获新授权的周期执行和最小持久结果，不枚举/探测生产Monitor，不创建生产任务，已有意图不能授权生产目标访问

### Requirement: Results are immutable private observations of one configuration version
内核结果 SHALL 包含完整scope、configuration_version、UTC时间、单调duration、可空最终http_status、success和固定failure_code。控制面SHALL 以不可变run/g/epoch/fence包络持久化最小结果；只有匹配当前v/g/epoch才能投影最近观察。旧版本已Start可按旧snapshot完成，不能改配置或解释为新URL健康。

#### Scenario: Concurrency - Configuration changes during execution
- **WHEN** v7任务运行时Owner将URL/timeout/expected_status改为v8
- **THEN** v7结果仍属v7并按v7参数判定，新任务使用完整v8；旧结果不会更新v8的当前健康或覆盖其配置

#### Scenario: Concurrency - User authority loss is separate from system execution
- **WHEN** 用户退出/被移除、Session撤销/清理或账户禁用在系统snapshot之后提交
- **THEN** 新用户API依既有契约失权，系统已取得的旧snapshot可完成；不缓存或复用用户Cookie授权，父context取消仍停止执行

#### Scenario: Success - Future persistence must preserve lineage
- **WHEN** 本Change的隔离周期闭环持久化结果或展示最小状态
- **THEN** 必须保留完整scope/run及v/g/epoch/fence，子引用用三元组；当前展示匹配最新版本代次，不保存URL/DNS/头/body或构造完整健康统计

### Requirement: Failure classification and diagnostic output are safe and deterministic
固定失败码 SHALL 区分invalid_configuration、dns_failed、blocked_target、connect_failed、tls_failed、timeout、canceled、response_limit、protocol_error、unexpected_status；未启动的配置预检/scope读取/admission拒绝 SHALL 不伪造目标结果。结果/日志 SHALL 不含URL任何部分、DNS/IP/CNAME、原始error、headers/body、证书、凭据或秘密；常规日志不带租户资源ID/高基数label；受授权最小状态DTO遵守monitor-runs，不公开网络详情。

#### Scenario: Security - Secret and network detail sentinels
- **WHEN** URL path、DNS答案、TLS错误/HTTP头/body或panic中含秘密sentinel
- **THEN** 结果只含规定ID/版本/时间/状态/固定分类，普通日志无sentinel/网络细节，内部ID仅在受信断言或授权的最小状态DTO出现，不扩展原配置DTO或泄漏网络详情

#### Scenario: Exception - Terminal classification and uncertain delivery
- **WHEN** 结构化策略/字节限错误、明确取消/deadline、TCP拒绝/TLS失败或执行后丢失结果
- **THEN** 保留结构化block/limit，其余按记录的取消/超时/阶段分类而非error字符串；零自动重试，结果丢失不表示远端没有收到GET

#### Scenario: Concurrency - Existing unknown configuration commit is unchanged
- **WHEN** 原配置POST/PATCH Commit未知后系统重新读取，或内部调用被显式重复
- **THEN** 只按数据库新快照的已提交配置执行，原写无自动重放/网络补偿；新调用仍需admission/DNS，不承诺跨进程exactly-once

## ADDED Requirements

### Requirement: Worker handoff only narrows the approved single execution kernel
隔离Worker SHALL 复用既有内核，不重写HTTP Client或削弱完整有界DNS/危险地址整体拒绝/literal绑定/原域名TLS/无代理重定向重试/Header-only GET/预算/单进程Admission。冻结配置须在Start同语句绑定v/g并离线校验，网络前释放DB资源。原76项受控安全场景 SHALL 全部复验。

#### Scenario: Security - Credential free single run handoff
- **WHEN** 已确认Start向Probe下发有界匿名管道执行信封
- **THEN** 信封仅一个完整scope/v/g/epoch/fence/run及冻结配置，URL不走环境/命令行/日志；同信封不重放，Probe重启不能恢复旧发送能力

#### Scenario: Security - Additional target permit never bypasses address policy
- **WHEN** 内核完成全部A/AAAA/CNAME验证并选中literal地址、尚未TCP拨号
- **THEN** 独立控制面原子核验共享目标预算/物理许可后才允许原内核的一次拨号；任何拒绝/断连/ACK未知零dial，不重解析/重试，许可等待计入原timeout

#### Scenario: Concurrency - Configuration changes around handoff
- **WHEN** Start前后Owner PATCH或Pause，或Probe执行中用户Session变化
- **THEN** Start前失效拒绝；已Start快照保持原配置，适用明确在途暂停语义，结果不能覆盖新v/g/epoch；后台不复用用户Cookie

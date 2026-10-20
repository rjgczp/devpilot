# http-monitor-execution Delta

## MODIFIED Requirements

### Requirement: Single execution is internal and never activates configuration
系统 SHALL 仅通过受保护内部测试能力调用单次执行器，不增加生产HTTP/CLI/cron/启动/调度/Worker入口。历史与新建/修改Monitor SHALL 保持不执行；原配置DTO SHALL 不增加状态/结果；独立activation子资源与Migration7 SHALL 仅保存版本意图，不作为执行许可。interval SHALL 不被用来自动扫描或安排任务。

#### Scenario: Success - Deployment preserves inactive history
- **WHEN** schema6或schema7存在大量历史与新Monitor且API启动或重启
- **THEN** 无任何Monitor目标DNS/连接，仍只有既有Session清理Worker，保存配置不表示启动

#### Scenario: Security - No public or hidden trigger
- **WHEN** 请求手动运行/未定义暂停路径、调用activation意图子资源、传入环境开关或检查API依赖图
- **THEN** 不存在真实执行入口，API不依赖执行器包；activation仅管理意图，新旧配置/意图端点均零目标网络，执行开关不能解锁

#### Scenario: Success - Controlled invocation without production targets
- **WHEN** 内部测试在独立隔离环境显式选择自己的完整scope和fixture
- **THEN** 可以验证一次真实执行与内存结果，不枚举或探测生产Monitor，不创建任何生产结果或任务，已有意图不能授权此测试访问生产目标

### Requirement: Acceptance reports unit network database and egress evidence separately
验收 SHALL 分别报告静态/单元、受控Linux真实DNS/HTTP/TLS、真实隔离PG18及独立出口证明，记录准确实现SHA/工具链/命令/exit/正向控制。显式job缺依赖 SHALL 失败而非skip。第八阶段配置API零目标审计/精确白名单及构建来源守护 SHALL 原样持续通过，不能扩大为probe白名单。

#### Scenario: Security - Required real network matrix
- **WHEN** 执行验收
- **THEN** 独立DNS/HTTP/TLS fixture验证两次DNS变化/混合RR/特殊地址/TOCTOU/SNI/证书/全部redirect/proxy/慢大响应/取消/deadline/配额，危险目标零TCP尝试及零HTTP负载有真实观测，wrapper/mock证据另标

#### Scenario: Concurrency - Required PG18 and regression evidence
- **WHEN** 运行完整normal/race integration
- **THEN** PG18屏障/锁观测证明scope快照/版本并发及网络期间无长事务，用户权限变更与原写锁协议回归通过，Migration1–6哈希保持，历史schema6测试明确边界；schema7新增意图需单独迁移/并发验收且无生产执行

#### Scenario: Security - Separate detector calibration and historical audit
- **WHEN** 新执行器runner允许DNS/fixture网络并运行，第八阶段配置API审计也运行
- **THEN** 两者独立runner/策略/证据目录；分别有准确positive control和预期attempt断言，旧配置审计仍零未批准attempt，不能以新网络白名单掩盖配置API出站

#### Scenario: Exception - Planning and missing infrastructure are not passes
- **WHEN** 仅有本次Propose/文档校验、旧归档通过、mock/unit或网络环境缺失
- **THEN** 不能标记新真实网络/PG18/出口测试通过，安全门禁保持未通过，托管CI另按最终实现SHA验收

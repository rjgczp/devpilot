# monitor-scheduling Specification

## Purpose

定义 Monitor 调度意图的持久化休眠边界，确保激活状态不会在缺少生产执行授权、网络隔离和分布式安全协议时转化为周期探测。当前仅交付状态基础，明确重启、积压、未来升级重确认和验收的可观察安全约束。

## Requirements

### Requirement: Dormant scheduling state never creates runnable work
本阶段next_due_at SHALL 始终NULL且有数据库约束，系统 SHALL 不扫描启用意图、不产生run/lease/队列/结果，不提供生产Worker/cron/CLI/HTTP执行入口或可解锁执行的环境开关。API进程 SHALL 不导入或调用执行内核；保存/启用意图不能触发目标网络。

#### Scenario: Security - Large intent populations and restarts
- **WHEN** 大批Monitor保有enabled意图、API重启或配置interval到期
- **THEN** 无任务积压/补偿/每Monitor goroutine，无目标DNS/TCP/HTTP，无运行记录；next_due_at非NULL写入被拒绝

#### Scenario: Exception - Unknown intent commit does not imply a probe
- **WHEN** 激活命令Commit未知、DB暂时不可用或服务崩溃后恢复
- **THEN** 意图可能已保存，但不能据此补发探测，执行入口仍关闭

### Requirement: Production execution requires a separately reviewed deployment boundary
真实生产执行 SHALL 继续关闭，直到另行批准Worker与分布式调度Change并取得对应生产环境的独立出口证明。单元/隔离namespace/历史CI通过 SHALL 不等于生产许可；应用IP过滤 SHALL 不替代默认拒绝的独立网络出口。不得在API进程内回退执行。

#### Scenario: Security - Missing or unverifiable production isolation
- **WHEN** 无可访问生产拓扑、缺IPv4/IPv6/resolver/内部公网/转换路由/最小权限或独立停止证明
- **THEN** 不开放互联网监控、不降低SSRF要求、不增加试运行环境开关，验收标记未验证

#### Scenario: Security - Fixture success has limited scope
- **WHEN** 第九阶段真实DNS/TLS/SSRF及隔离默认deny负向控制通过
- **THEN** 只证明对应测试拓扑，不能自动启用已保存Monitor或宣称生产网络安全

### Requirement: Future worker introduction cannot activate stored intent automatically
本阶段持久化enabled意图 SHALL 不作为未来生产运行的直接授权。引入Worker前 SHALL 清除本阶段enabled意图、推进代次并要求当前Owner重新确认当时的配置版本；迁移/启动/配置保存 SHALL 不自动授权历史Monitor。

#### Scenario: Security - Later deployment with old enabled intent
- **WHEN** 后续独立获批版本准备引入真实周期调度
- **THEN** 旧意图不触发DNS/连接，必须先完成受审查的失效升级和新的Owner版本确认

### Requirement: Acceptance separates planning database network and deployment evidence
本Change验收 SHALL 分开记录文档/单元、真实PG18并发、Race Detector、受控Linux网络及部署出口状态。A的Apply SHALL 保留前九阶段回归，扩展配置零目标审计覆盖意图接口；计划或旧归档 SHALL 不被标为本次实测通过。

#### Scenario: Concurrency - Real database proof for dormant activation
- **WHEN** 后续获授权A实现接受验收
- **THEN** PG18独立连接/屏障/锁观测验证授权失权、两Owner CAS、配置原子失效、未知Commit、共享配额/冷却和Migration0→7/6→7/回滚/隔离Down；normal/race完整回归

#### Scenario: Security - Independent zero-network and kernel regression
- **WHEN** 运行Linux验收
- **THEN** 原配置及意图接口为零目标DNS/TCP/HTTP，旧精确网络白名单和构建来源守护不放宽；独立runner保留第九阶段全部真实DNS/TLS/SSRF/资源测试，不用probe白名单掩盖配置出站

#### Scenario: Exception - Missing infrastructure and planning-only status
- **WHEN** 只有Propose/文档校验或缺PG18/Goose/IPv6/namespace/生产访问
- **THEN** 未执行项明确记录，显式测试job缺依赖失败而非skip后报通过；生产执行门禁保持关闭

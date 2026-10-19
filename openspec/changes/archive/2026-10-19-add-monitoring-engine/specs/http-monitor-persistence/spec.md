# Spec Delta

## MODIFIED Requirements

### Requirement: Future probing requires a separately verified SSRF execution boundary
探测器 MUST 在每个获准新连接前验证完整有界A/AAAA/CNAME观察并绑定已验证IP，拒绝非公网/特殊用途/元数据/转换绕过，保留原域名TLS验证/SNI，禁止代理/重定向跟随，并独立验收出口及资源边界。配置API SHALL 继续零目标网络。本阶段 SHALL 只允许受控内部单次测试，无生产执行入口/调度/激活/结果持久化；第八阶段历史不实现探测的契约与证据 SHALL 保留其历史含义。

#### Scenario: Security - Rebinding TOCTOU and redirect escape
- **WHEN** 域名答案变化、混合公网与私网、AAAA嵌入特殊IPv4或HTTP重定向到内部资源
- **THEN** 执行期拒绝不安全答案且不跟随重定向，不在已校验后让默认客户端重解析host；独立真实测试验证DNS变化、IP绑定、TLS、代理/redirect及出口阻断，静态URL测试或第八阶段零网络审计不能替代这些证明

#### Scenario: Success - Configuration remains dormant without new schema
- **WHEN** 本阶段部署、历史/新Monitor存在、配置被保存或更新
- **THEN** 无自动执行、触发API/Worker、启停/结果表或Migration7，Migration1–6字节与schema6保持；内部执行只用隔离fixture，未来生产激活仍需独立评审

#### Scenario: Concurrency - Internal execution does not extend write transactions
- **WHEN** 系统按完整三元组在同一语句快照读完整配置/version并在释放DB资源后执行，Owner同时更新配置或用户权限变化
- **THEN** 原User→Session→Workspace→Membership→Project→Monitor保护与未知Commit语义不变，网络不持这些事务锁；旧结果明确属旧version而非新URL，未来子表仍需完整三元组和版本谱系

#### Scenario: Security - Internal library is not production egress approval
- **WHEN** 内部单次网络/PG测试通过但缺生产出口隔离/激活评审
- **THEN** 不能公开运行探测服务或声称生产SSRF安全；单元、真实网络、PG18及独立出口证据分开报告，第八阶段配置API审计继续以原精确白名单验证零目标尝试

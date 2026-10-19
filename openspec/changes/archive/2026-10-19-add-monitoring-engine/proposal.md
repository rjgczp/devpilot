# Proposal

## Why

HTTP Monitor 当前只有配置，缺少可验证的执行期 SSRF、超时及资源边界。先建立可在隔离环境真实验收的单次执行能力，避免将配置保存直接变成对用户控制目标的持续网络访问。

## What Changes

- 推荐范围 **A：安全单次探测执行器**。提供内部不可变配置快照、受控 DNS/IP 策略、绑定连接、GET/HTTPS 执行和内存结果；真实执行仅通过受控内部测试，不接入 API 进程、生产启动、定时任务、命令行探测入口或公开手动触发接口。
- DNS 对每个获准的新目标连接重新查询完整、有界的 A/AAAA/CNAME 答案；混合危险答案整体拒绝。按审核后的地址策略拒绝特殊用途、元数据、嵌入/转换地址，再直接拨号已验证 IP，核对远端，不将域名交给默认 Dial。
- HTTPS 保留原始域名 SNI/证书校验；禁止代理、重定向跟随、凭据、连接复用及隐式重试。推荐只判断最终响应头状态，不读取响应体，限制 DNS、TCP、TLS、响应头及整体耗时和总读取字节。
- 引入有界、无排队的内部 admission：进程/Workspace/Project/Monitor 并发、速率和状态表上限，取消后回收；隔离探测测试进程不持有 API Cookie、应用 DB 凭据或业务事务锁。
- 系统读取以完整 `(workspace_id, project_id, monitor_id)` 在同一 PG18 语句快照取完整配置和 version。已开始的旧版本可完成，内存结果明确归属该版本；不成为当前配置健康状态。
- 本轮 **不增加 Migration、数据库结果表、激活字段、调度器、生产 Worker 或新 API**。Migration1–6 不变，下一次确需 schema 变更才使用 Migration7；新旧 Monitor 均保持纯配置，部署后零自动执行。
- 分开规定静态/单元、受控 Linux 真实 DNS/HTTP/TLS、真实 PG18 和独立出口隔离证据；保留第八阶段配置 API 零目标网络审计。应用层检查不替代出口防火墙，未满足出口门禁不得宣称公开服务已具备生产 SSRF 安全性。
- B 同轮激活/周期调度/Worker 的数据模型、并发和故障成本见 design；作为比较与后续评审输入，不列入本轮可执行任务。
- 本次仅 Propose。所有选择是待确认推荐；独立技术评审及用户另行授权 Apply 前，不实现、不迁移、不探测生产目标、不创建 PR、不部署或激活。

## Capabilities

### New Capabilities

- `http-monitor-execution`: 内部单次探测、DNS/连接/TLS 安全协议、结果、版本快照、资源预算、隔离及分层验收。

### Modified Capabilities

- `http-monitor-persistence`: 将既有“未来探测需独立验证”要求衔接为本轮隔离单次执行门禁，保留配置写事务、Migration6 和未来结果版本契约；不改变配置 API 的零目标网络要求。

## Impact

后续经授权 Apply 预计新增 `internal/monitoring/`（与配置包 `internal/monitor/` 分离）、PG18 受控集成夹具、`tests/network/` 下独立执行器测试夹具/runner、CI 验收 job 及操作说明。`cmd/api`、`internal/app` 不导入或启动执行器；API schema/路由保持原样。复用 Go1.24.3、net/http、pgx/v5、已固定的 `golang.org/x/net/dns/dnsmessage`；不新增 Redis、队列或隐式依赖升级。必要的纯配置校验复用仅做窄接口，不重构认证/Workspace。

基线核实：2026-10-19（Asia/Shanghai），初始工作区干净，分支 main；HEAD、本地 origin/main 与 GitHub `GET /repos/rjgczp/devpilot/branches/main` 均为 `252c7d3c000c68de3e08d2fc8de4cece3a2d00e8`。`git ls-remote` 因本机代理不可达失败，已用连接器补核实。八阶段归档齐全，active changes 初始为空；Migration6 为 `00006_create_http_monitors.sql`，SHA256 `26ac26c66da96b0f16076342f0f11258bb6ea92be5f71a6b015c97a3075ee4e2`，只有配置/复合 FK/不可变身份保护，无执行状态。完整发现、权衡、待确认产品决策及来源在 design。

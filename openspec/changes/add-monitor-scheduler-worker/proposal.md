# Proposal

## Why

前十阶段已交付配置、版本激活意图及安全单次执行内核，但用户尚不能观察持续监控闭环。需要把明确、有限期的 Owner 授权转成可恢复的周期任务，同时防止历史 enabled 意图、重复领取、未知网络执行和未验证的生产出口被误当作运行许可。

## What Changes

- **推荐方案 A**：独立 Linux 无上游 fixture 网络、真实 PG18、多 Controller/Worker 的完整周期闭环；生产真实网络执行硬关闭。它交付授权→到期→领取→开始→最小结果→下一周期、暂停及故障恢复，具有独立产品验收价值。
- B 需要实际生产网络、独立出口策略、可撤销执行许可和节点生命周期证据；当前不具备，不选择。C 只交付数据库控制面，安全但不足以验证周期产品行为；仅作为 A 的内部实施步骤，不作为默认交付终点。
- **BREAKING**：规划 Goose Migration8 原子撤销全部旧 enabled 意图、推进 g、清空旧配置授权且保留冷却；新旧 API 混跑时，旧 activation PUT 只能写意图，不能生成执行授权。新增独立有限期授权命令，必须由当前 Owner 重确认 v/g；不自动升级旧意图。
- **BREAKING（第一轮评审修订）**：保留Activation字段形状/类型、权限、Owner意图和强ETag格式，废弃两个旧执行字段，固定返回 `not_reported / execution_status_required`，实际观察以execution-status为唯一权威；不把真实running伪报blocked。Migration8令全部存量Activation含disabled的g恰好+1一次以退役旧强tag，冷却不缩短。旧const客户端须迁移；fixture运行前只允许新投影HTTP实例接流量，旧DB写者不授予执行的保证不变。
- 分离意图、调度、run、lease/fence、执行许可与运维停止；引入共享槽/令牌、公平有界扫描、无积压补偿、不可重发的 started/unknown 语义及有界保留。
- Claim统一为无锁候选→Owner User SHARE→Workspace NO KEY UPDATE SKIP LOCKED→Membership→Project→Monitor→Activation→Schedule→Run→Budgets→Reservation；候选失权/跳过即全回滚重选，绝不持Workspace补锁其他User；全部等待后新SQL授权复验，并规划两个Owner/两Worker/用户PATCH的真实PG18锁图验收。
- 明确采用 Start Commit 线性化的暂停模型作为 A 的评审候选：暂停阻止后续 Start，已开始任务可能在暂停返回后发包；更强物理停止依赖独立出口栅栏。产品未接受该承诺时不得开放生产执行。
- 新增最小授权/运行状态投影；保持原 Monitor DTO/ETag、Cookie/CSRF/Owner 协议和配置/激活 API 零目标网络，不增加历史图表、告警或前端。

## Capabilities

### New Capabilities

- `monitor-runs`：run 谱系、最小持久结果、unknown、权威运行状态与跨接口一致性、有界保留。
- `monitor-worker-security`：控制面/Probe 信任分离、独立出口许可、物理终止证明、共享资源和公共目标保护。

### Modified Capabilities

- `monitor-activation`：区分意图/有限期授权，显式废弃旧执行字段及迁移旧强tag；保持资源形状、权限、ETag格式及Owner命令，明确语义breaking change。
- `monitor-scheduling`：从强制休眠演进为仅隔离环境可执行的周期状态机、父先于子领取、Start/Result 和故障恢复。
- `http-monitor-execution`：受控跨进程调用复用原内核，保留全部 DNS/TLS/SSRF/预算，结果加入 g/run/fence 谱系。
- `http-monitor-persistence`：Migration8、安全升级、独立调度/run 表和触发器兼容保护。
- `http-monitor-management`：保留离线配置行为，明确独立授权与状态子资源例外，避免旧“全系统没有任务”表述与新隔离闭环冲突。

## Impact

基线 `1d3b8266e494061beccc3ccdf47694ba4b0339c5`；本地 HEAD/main/origin/main 与远端 main 一致，初始工作区干净，十个 Change 已归档。[PR #10](https://github.com/rjgczp/devpilot/pull/10) 已合并；基线 [CI 39356475239](https://github.com/rjgczp/devpilot/actions/runs/39356475239) 的 verify、integration、execution-security 均 success。当前 compose 仅 PG18，无生产 Worker 拓扑；历史 76 项受控网络测试只证明 fixture 环境。

后续 Apply 涉及独立 Controller/Worker 控制面、复用 `internal/monitoring/` 的隔离 Probe、Monitor 授权/状态路由、Migration8、PG 最小权限函数、专用 Linux 验收与 CI；不引入 Redis/Kafka。Migration1–7 字节固定。A 预估 20–30 工程日加 5–8 日独立安全/故障验收（单人顺序工作，非承诺工期）；依赖 PG18/Goose3.26.0、Go1.24.3、具 IPv6/NET_ADMIN 的独立 Linux runner、接收端观测和出口监督器。

本次仅 Propose：只写本目录规划工件及 CLI 元数据。不写业务代码或 SQL、不迁移、不启动 Worker、不探测公网、不创建实现 PR、不部署/合并/归档。独立设计审查及产品对范围、暂停语义、网络条件明确批准后才可另行 Apply；生产开放仍需独立部署级授权。

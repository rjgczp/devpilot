# Proposal

## Why

HTTP Monitor 已有经过隔离验收的单次执行内核，但保存配置没有周期探测授权，系统也没有分布式调度或生产出口许可。需要先建立 Owner 对明确版本的可撤销意图，防止历史配置、配置修改或未来部署意外触发网络访问。

## What Changes

- 推荐 **方案 A：激活意图与调度状态基础**。新增独立 activation 资源，Owner 可显式设置 enabled/disabled，Owner/Member 可读；enabled 仅表示针对指定版本的意图，响应固定显示 `execution_state=blocked`、`blocked_reason=scheduler_unavailable`，绝不表示已经运行。
- Monitor 配置 version 与 activation_generation 分离；activation 强 ETag 同时覆盖两者。每次成功激活/暂停（包括同值命令）消费 generation；任何成功配置 PATCH（包括名称或同值 PATCH）原子撤销授权并推进 generation。历史与新 Monitor 默认 disabled。
- 拟议 Goose Migration7 仅增加完整三元组引用的 activation 表、约束/索引、默认初始化及配置变更失效保护。保存最末状态审计与持久化再激活冷却；不保存探测结果/URL 副本/无界事件历史。Migration1–6 字节不变。本轮不创建或执行 SQL migration。
- 激活写复用 Cookie-only Session、精确 Origin/CSRF、数据库最终实时 Owner 门控和现有父先子后锁序。每 Workspace 最多100个 enabled 意图；每 Monitor 接受一次 enabled 后至少60秒才可再次接受 enabled，暂停不受此冷却限制；数值均待产品确认。
- `next_due_at` 在 A 永远 NULL；没有任务、租约或执行队列。A 的 enabled 意图不得在未来引入 Worker 时自动生效，必须升级时清除并由 Owner 重新确认。
- 比较 **方案 B：激活 + 周期调度 + 受控 Worker**，在 design 中研究短事务领取、租约/fencing、最小有界运行记录、分布式预算、暂停竞态及真实网络门禁；这些是后续设计输入，不是本轮实施任务或已批准契约。
- 缺生产网络隔离证明时选择 A。没有生产 Worker、执行环境变量开关、API 内执行回退或公网探测。独立设计评审与新的明确 Apply 授权前，只完成本次规划。

## Capabilities

### New Capabilities

- `monitor-activation`: Owner 的版本授权/暂停、条件并发、默认关闭、配置失效、权限、安全状态响应、意图配额及最末审计。
- `monitor-scheduling`: 本次仅定义调度状态的持久化休眠边界、禁止积压/网络执行、未来执行必须重新授权和验收的门禁；不定义可运行调度器。

### Modified Capabilities

- `http-monitor-management`: 在独立子资源允许启停意图；配置端点维持原 DTO/ETag 和零目标网络，配置 PATCH 原子失效授权。
- `http-monitor-persistence`: 允许 Migration7 的三元组 activation 子表，扩展写锁序和配置版本的原子失效义务，保留历史 Migration6 的验收含义。
- `http-monitor-execution`: 区分意图 API 与实际执行入口；现有内核仍只在隔离测试调用，生产执行继续关闭。

## Impact

未来经授权的 A Apply 涉及 `internal/monitor/`、`internal/app/routes.go`、`docs/api/openapi.yaml`、Migration7、PG18/normal/race/配置零网络验收及说明；不引入 Redis、消息队列或依赖升级，不改造认证与 Workspace。B 将另需独立控制器/无凭据 probe 隔离部署、最小数据库角色与受验证的网络策略，仓库目前不具备这些生产条件。

基线检查（2026-10-19，Asia/Shanghai）：初始工作区干净；HEAD、main、origin/main 及 GitHub ref API 返回均为 `2cfdcd6c0c094e938fc0570b67697346c3da7aee`。本机 Git 代理端口不可达导致 `git ls-remote` 失败，已通过 GitHub 连接器核实远端。九个归档齐全、初始 active changes 为空，15份主规格已检查；历史全库 strict 有四份规格七项长度 WARNING，单列于 design，保持原文。

# Task 7.4 — execution-status 实施边界与权限核对

## 后续正式授权记录（2026-10-28）

产品负责人已在本会话明确接受 7.3 保守范围，并授权仅实施 7.4。
7.3 已按冻结 a1fc65a 原始证据核验关闭，32/47。
以下是授权前方案快照，其“待批准/尚未实施”文字保留历史含义；
实际实施、验收及最终 SHA 以 monitor-scheduler-task74-verification.md 为准。
权限差异仍为零；7.5 未获授权。


2026-10-28，Asia/Shanghai。只读源码基线：
`a1fc65a0dbc9c53259d9b06a411250575f35fa6d`；分支
`codex/add-monitor-scheduler-worker`。本文件是方案，不是实施或验收证据。

## 门禁及范围

- 当前仍为 **31/47**。7.3 A 项已获独立代码审查认可，冻结 CI 通过；
  产品尚未明确接受 canceled 能力收缩，因此 7.3 不勾选。
- 当前恢复协议对缺少可信、可区分、已持久化许可拒绝事件的 started Run
  保守归类 unknown；即使不存在目标许可且已有真实物理释放证明，也不声称
  已确认拒绝。补充可信事件来源需要另行评审，不纳入 7.4。
- 历史 IPv6 路由配置 20 秒超时根因未确定，负面证据保留，不宣称已修复。
- 产品接受上述范围调整后才可将 7.3 标为完成、更新 32/47。
  7.4 仍须单独取得正式实施授权；本方案不构成该授权。

## 已有实现与待核对部分

| 已有文件/入口 | 已存在的能力 | 7.4 拟补齐，尚未实施 |
| --- | --- | --- |
| `internal/monitor/handler.go`、`execution_status_handler.go` | 规范 GET 子资源、Cookie 摘要、禁 query/条件头/body；5s/2s；固定错误及 no-store/nosniff | 针对真实运行状态与实时失权补全 HTTP/PG 契约，不增加探测路径 |
| `internal/monitor/execution_status_store.go` | 一次 QueryRow 调用既有受限 SQL 函数 | 保持一次语句身份/投影快照，避免拆成预查身份再查 Run |
| `internal/monitor/execution_status.go` | 独立授权/调度/attempt DTO，三个最多各一条的观察字段 | 严格字段白名单、空值、UTC/十进制谱系及状态契约验收 |
| `migrations/00008_create_monitor_scheduler.sql` 的 `execution_status` | STABLE SQL SECURITY DEFINER、固定 search_path、同语句实时身份/完整 scope、LIMIT 1 观察、unknown 遮蔽旧 success | 核对 7.2 当前结果指针与历史 completed 检索的分工、7.3 后 unknown/quarantine 读取，不改结果写入或恢复协议 |
| `docs/api/openapi.yaml` | 已有路径与 MonitorExecutionStatus/MonitorAttempt | 逐项对齐实际合同，维持现有正交枚举，不新增总健康状态 |
| 既有 checkpoint1 HTTP/权限/投影测试 | 已有基础身份、意图兼容、静态投影与切换验证 | 复用既有覆盖；旧管理员行构造仅证明投影，不能包装为真实执行/物理释放证据 |

源码核对发现：现有读取按全 scope 的最近完成行构造 `current_observation`，
并比较当前 v/g/epoch；7.2 写入另有受门控的 `latest_result_run_id`。
未来实施须用真实对照验证二者一致性；建议当前观察读取既有受门控指针并再次
核对完整 scope/v/g/epoch，历史 `latest_completed_observation` 仍可保留原谱系。
这是一项待测试的收敛点，不据此宣称已发现或已修复越权漏洞。
旧 checkpoint1 的直接构造样例如需适配指针，须清楚标记为投影 fixture，
保留相同用户可见安全断言，不用管理员 UPDATE 替代 Supervisor 终止证明。

## 逐项 OpenSpec 映射与拟验收

主规格仍保留前阶段已批准历史契约。本次以当前 Change Delta 为新增契约依据，
不提前同步、改写归档或 Archive。

| Task/Requirement/Scenario | 实施边界 | 后续定向真实验收（拟新增，当前未运行） |
| --- | --- | --- |
| 7.4；monitor-runs / Execution status is a separate live authorized projection / Minimal status distinguishes intent and actual execution；Design D11 | 用户意图 desired_state；授权 none/valid/expired、expires_at；调度 disabled/authorized_waiting/due/blocked、next_due/reason；attempt 保留原状态与谱系 | Owner/Member 均可读；enabled 不等于运行；leased 不伪称 running；started 作为实际运行 attempt 显示；不新增扁平总状态字段 |
| 同 Requirement / Current identity and privacy remain mandatory；管理主规格 Monitor reads bind live identity and complete resource scope | 同一语句 active User、有效未撤销 Session、当前 Membership、完整 W/P/M；无单独裸 ID 存在查询 | 真实会话撤销/过期、禁用、退出、移除、重入 Member 与读取的双顺序；无身份 401、不可见或混合 scope 404、合法成员 200 |
| 同 Requirement / Unknown is not masked by an earlier success | latest_attempt 不被旧 completed 替代；unknown 的 HTTP status/success/failure 为 null；current_observation 不冒充旧 success | 同谱系旧 success→新 unknown；旧版本 completed、PATCH、新 g、同 v 重新授权/epoch 变化；旧审计可见而当前观察 null |
| Every run preserves complete immutable execution lineage / Mixed tenant identities cannot create or read a run；Reauthorization cannot relabel an old observation | 每个返回 Run 保留 W/P/M/run/v/g/epoch/fence；不重标历史谱系 | 两 Workspace/两 Project 真实数据混合，外租户 Run 不出现；新旧结果交错仍只投影本 scope |
| Design D11 正交投影；unknown/授权到期与在途观察条款 | in_flight 依据真实 started 与物理释放事实；不能因 Lease 过期、Pause、PATCH、unknown 或 HTTP 完成推断物理停止 | 复用已批准真实 5.4/6.5 回执路径验证 unknown 在释放前后保持 unknown、in_flight true→false；读取不返槽、不写结果、不推进 g/tag |
| Results contain only bounded private observations / Sensitive sentinels never persist；D11 私密响应 | 最近 attempt、最近 completed、current observation 各最多 1 条；只返现有最小结果；无 URL/actor/lease/Worker/IP/hash/原始错误 | 对响应 JSON 递归字段白名单及敏感哨兵，UTC/nullable 字段、固定 failure/stop/blocked reason；不新增分页、聚合、SLA、前端或日志敏感输入 |
| http-monitor-management / Configuration management never performs target networking / Intent endpoints are also offline；worker-security / Production execution stays hard closed | API 只读控制面数据库，不依赖执行内核或 Probe；运行状态变化不改旧 Activation 表示 | 保留既有精确出站白名单/CGO/NSCD 反例，扩充 status 调用覆盖；固定错误 401/404/500/503、no-store/nosniff、5s/2s；普通发布无执行入口 |
| monitor-activation / 旧迁移提示契约；monitor-runs / Missing authority is not evidence of no execution | Activation 固定 not_reported/execution_status_required；status 不支持/404/503 只显示运行状态不可用 | OpenAPI/合同测试确保不回退推断 blocked、healthy 或未运行；不增加真实客户端或前端 |

7.4 内只做状态接口所需的定向快照/鉴权/谱系验收。
7.5 的完整跨接口矩阵、两次读取间 v/g 变化客户端联合验收、
第 8–10 组容量/调度/完整交付不在本批实施范围，不能提前勾选。

## 数据库权限差异：空集

现有签名：

`scheduler_private.execution_status(bytea,uuid,uuid,uuid)`
`RETURNS TABLE(decision text,data jsonb)`。

- 已由 `devpilot_scheduler_api_owner`（非 LOGIN）持有；
  `LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private`。
- 已有 `REVOKE ALL ... FROM PUBLIC`，仅既有 API 运行角色
  `devpilot_scheduler_api` 获得该函数 EXECUTE；不是 Worker-only 系统函数。
- 当前 owner 已有 users(id,status)、sessions(token_hash,user_id,expires_at,revoked_at)、
  Membership、Project、Monitor、Activation、Schedule、Run、Control 所需 SELECT。
  API LOGIN 本身不直接读这些秘密或业务表；Cookie 原文不入库、不向 Worker 传递。
- 拟仅调整既有只读函数体及 Go/合同测试；没有新 SECURITY DEFINER 函数、重载、
  角色、role membership、GRANT、表列 ACL、密码/凭据读取或恢复/返槽能力。
- 不读取 reservations 以便利推导；需要的物理停止与调度事实只使用已批准
  Run/ Schedule 投影。如果证明现有字段或 SELECT 不足，停止并另报精确差异。

因此本次**无需提交新增 EXECUTE 授权申请，拟 GRANT 差异为零**。
这不是对未来权限变化的预批准；任何实际需要的新能力须先单独审核。
历史生产登录角色清单仍未知，不能据此补生产 GRANT 或执行 Migration。

## 后续获授权后的验证与交付边界

1. 冻结测试清单，将以上场景映射到实际测试名；基于既有接口定向实现，
   不重做 1–6 组功能。真实权限/事务并发不能用 Mock 替代。
2. 受 testdb 守护的独立可销毁 PG18 normal/race，真实 Cookie HTTP 与独立连接
   屏障/快照；物理状态采用已有可信终止链，保留原始失败和 SQLSTATE。
3. 必要 Go unit/race/vet、API 零目标审计及普通构建边界；最终同一冻结 SHA
   绑定命令、退出码、源码/日志哈希、Push/PR CI 和 Artifact。不把旧 CI 记为新实现通过。
4. Change strict、git diff --check、Migration1–7 字节及 ACL 零差异复核。
   仅实际验收后申请勾选 7.4，并停止等待独立审查。

本次只读/文档核对未启动容器、PG、Probe 或任何目标网络测试；
刚停止的 Docker 容器保持停止。生产执行硬关闭，PR11 Open；
不合并、Archive、部署或实施 7.4。

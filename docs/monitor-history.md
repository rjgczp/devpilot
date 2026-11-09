# Monitor 检查历史

本地启动仍为 `./scripts/dev-dashboard.sh`，浏览器打开 <http://127.0.0.1:5173>，登录后选择 Workspace / Project，点击监控行“历史”或状态详情“查看历史”。Member 和 Owner 均可只读查看。默认 20 条；筛选后重置游标，“加载更早记录”追加记录，“刷新历史”重新取最新窗口。深浅色与移动布局复用原 Dashboard。

执行环境关闭不会制造结果；真实本地流程展示空历史与环境未开放提示。浏览器有数据用例使用明确的合成响应；PG 集成用例只造最小投影，不声称执行过 GET 或取得物理释放证明。已有 Task9.1 原始真实检查证据原封保留。

## API 契约

`GET /api/v1/workspaces/{workspace_id}/projects/{project_id}/monitors/{monitor_id}/history?limit=20&state=all&after=...`

- Cookie、实时 Workspace 成员关系和完整 W/P/M 链必须有效，每页重新检查；有效 Owner/Member 同等只读。会话撤销为401，越权/错误 scope 为404，不暴露资源是否存在。无请求体、无条件请求；错误固定脱敏。响应 `no-store`、`nosniff`，无 ETag、Set-Cookie。
- `limit`: 默认20、1–50；`state`: all/success/failure/unknown/running/canceled/expired。未知参数、重复参数、无效或跨 scope/filter 游标为400。
- 响应 `{configuration_version,data:[{id,configuration_version,checked_at,finished_at,status,http_status,duration_ms,failure_code}],pagination:{next_after}}`。无历史返回空数组及 null 游标。
- `checked_at` 为真实 started_at，只含已经开始的检查。成功/失败来自 completed 最小 Result；unknown/running 不推断 HTTP/耗时/失败原因。失败只返回既有约束允许的分类，不返回原始错误、URL、IP、凭据、工作节点、Fence、物理证明、网络安全元数据。
- 排序为不可变 `claimed_at DESC, run_id DESC`，时间相同用 UUID 打破平局；不是配置可重置的 Fence。游标携带完整 scope/filter、上一页位置与首请求上界。无需边界行继续存在，不使用 OFFSET；上界以上新检查只在刷新后可见。游标只是位置，不是授权能力；数据库仍逐页鉴权。保留清理和状态变化实时可见，不保证跨请求数据库快照；状态变更可能改变筛选成员。
- 历史页头只用现有权威 execution-status，筛选不能用旧成功覆盖新 unknown。历史配置显示版本标签；不将旧代次结果推断为当前健康。趋势仅绘制已加载完成检查的实际耗时，unknown/缺值断线，不记作0；真实0ms保留。不提供 SLA 或全量聚合。

机器契约见 `docs/api/openapi.yaml` 的 `listMonitorHistory`。

## 最小数据库变化

现有受限 API 角色无法直接读 monitor_runs；故 Migration13 仅增加只读授权入口 `scheduler_private.monitor_history(bytea,uuid,uuid,uuid,integer,text,timestamptz,uuid,timestamptz,uuid)`。函数 STABLE、固定 search_path，复用已存在的非 LOGIN `devpilot_scheduler_api_owner` 与 `execution_status` 实时身份门控；单一 SQL 快照内再次限定完整 W/P/M。仅给既有 `devpilot_scheduler_api` 该函数 EXECUTE，撤销 PUBLIC；无表/列授权、无新角色、无调度/物理执行能力。不修改 Migration1–10/12；不实施 Migration11。Down 仅删除本函数。

复用 monitor_runs 已有完整 scope 前缀索引及既有单监控保留上限，对监控范围内数据排序，不建新索引/表/缓存。查询保留2秒 DB及5秒 HTTP预算。此迁移用于最小功能授权，不是性能扩建；仅在可销毁测试 PG18 与独立本地演示库应用，未执行生产迁移。

## 定向复验

```
go test ./internal/monitor
npm ci --prefix web
npm run --prefix web build
npm run --prefix web test
./scripts/dev-dashboard.sh
# 另一终端，脚本已安装原有固定版本 Goose：
./tests/history/run-pg.sh
npm run --prefix web test:e2e
```

PG脚本自建自毁独立PG18 Linux容器/网络/数据库，不接受调用者 DATABASE_URL；端口仅绑定 loopback，无 Worker/Scheduler 或目标执行能力。测试覆盖受限真实 LOGIN 的20/50上限、并列时间、分页期间新插入、独立游标位置、筛选、unknown/null、Owner/Member、混合scope/跨租户/会话及成员撤销、精确字段和函数ACL、禁止直接读取Run表；分别 normal/race，使用独立 `integration,historyfixture` 标签；因投影保留记录不能走历史全库回退，该测试仅在专用一次性集群中执行。投影行保留原物理安全约束，结束仅回退本轮只读函数后销毁集群，不强行回退既有迁移。本轮直接比较 Migration13 前后原 commit_result 定义、owner及ACL，确认只读迁移不改变已验收执行入口。既有 Migration12 往返用例固定自身版本范围，原断言和已验收 fixture 前置条件不变，本轮不重跑该物理 fixture。浏览器真实API用例检查环境关闭的空历史，模拟数据用例检查筛选/分页/未知优先/趋势/失败清空。

不重新执行完整调度矩阵，不把本轮投影测试当作真实周期、容量或物理证明。Task8.3及9.2–10.5状态不变；生产公网执行继续关闭。

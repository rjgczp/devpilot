# Proposal

## Why

Workspace 已有租户与角色模型，但缺少查看、移除和退出成员的产品闭环。第五个 Change 需要在开放成员写入口前明确用户同意、实时授权和最后 Owner 的并发保护，避免把知道用户 UUID 误当作加入许可。

## What Changes

- 提议成员列表和成员删除接口；Owner 可移除普通 Member，Member 可退出，Owner 仅可在仍有其他 Owner 时退出，不提供移除其他 Owner 或角色授予接口。
- **用户已确认缩小独立交付**：本轮先列表、移除、退出；加入另开需要目标用户同意的 Change。本轮不开放 `POST /api/v1/workspaces/{workspace_id}/members`。
- Design 保留直接添加已注册 UUID 的风险、候选错误/幂等契约和延期验收清单，用于解释为何不采用；它们不属于本轮 delta 或实施任务。后续加入时新增角色只能为 member，不允许客户端授予 owner。
- 成员写采用 READ COMMITTED 短事务：有序 User → Session → Workspace → Membership 锁定；等待结束后重读权限、Owner 数量及数据库时间有效性。读取沿用单语句身份与成员快照。
- 复用 `workspace_members` 和现有 Cookie、CSRF、错误与日志规则；以真实 PG18 屏障与锁观察测试证明并发安全，保留完整注册、登录、Session、Workspace 回归。

本次仅生成规划，等待独立技术评审与产品决策后再显式请求 Apply。不编写业务代码、运行迁移、修改数据库或创建 PR。排除邮件、邀请链接/邮箱验证、Owner 转让 UI、角色晋升/降级 API、复杂 RBAC、Workspace 更新/删除、项目 CRUD、监控、告警、AI Agent、Redis/JWT/OAuth、前端和计费。

## Capabilities

### New Capabilities

- `workspace-members`: 成员列表、无直接加入入口、普通成员移除/退出、Owner 自退限制、输入/响应/隐私和可执行验收。

### Modified Capabilities

- `workspace-access`: 将同源 CSRF 扩展至 DELETE，统一成员写预算与租户安全验收；消除旧契约“无成员管理接口”的阶段性限制，保留原三个 API 的字段与行为。
- `workspace-persistence`: 允许经过授权的成员写入口，规定同 Workspace 串行写、最后 Owner 不变量与无自动重试；复用现有 schema，明确应用事务协议对受信任 SQL 的边界。

`user-authentication` / `session-management` 主契约已明确身份不等于事务授权，故无需 delta；将其既有撤销、禁用、过期和清理规则纳入回归，不改变 Cookie 或认证 API。

## Impact

- 未来实施涉及 `internal/workspace` 的窄成员存储/处理接口、`internal/app/routes.go`、单元与 `tests/integration`、`docs/api/openapi.yaml`、README 和 CI 分支触发配置；不引入依赖。
- 推荐不新增 migration/索引，不改已应用迁移 1/2/3；最后 Owner 由所有受支持成员写的统一事务协议保证，不能宣称现有 CHECK/FK 阻止任意 SQL 删除最后 Owner。若评审要求数据库强制约束，必须先修订本规划并新增版本 4，不能在 Apply 中暗加。用户同意持久化属于后续加入 Change。
- API 为新增；既有 Workspace DTO、认证、Session TTL、清理与 health 保持兼容。新增列表会向同租户成员披露有限 UUID/role/joined_at；这是需要确认的新增可见性。本切片尚不能让新用户加入或退出后自助重加，必须如实描述其交付范围。

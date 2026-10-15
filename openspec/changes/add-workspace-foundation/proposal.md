# Proposal

## Why

DevPilot AI 已有注册和 Cookie Session，但尚无租户归属与资源授权边界。建立 Workspace 和 User 的多对多成员关系，使后续业务能以明确、可验证的租户边界接入，并先验收跨租户拒绝行为。

## What Changes

- 新增创建、当前用户 Workspace 列表、成员可见详情三个 API；身份仅来自现有 Cookie Session，创建者自动成为 owner。
- 新增 `workspaces`、`workspace_members`，使用 PostgreSQL 18 原生 UUIDv7、复合成员主键、角色检查、外键和列表索引；名称可重复，UUID 是唯一资源标识，本轮无 slug。
- 创建 Workspace 与 owner 成员在短事务中原子提交；对会话撤销与账户禁用采用明确行锁及重新验证协议，不以入口认证代替事务授权。
- owner/member 均可读取所属 Workspace；非成员和不存在资源返回相同 404。列表由可信用户身份约束，详情同时约束用户与租户。
- 复用现有 Origin、自定义头 CSRF 协议、Cookie 解析、安全错误与日志规则；不变更登录、注册和 Session 生命周期契约。
- 推荐本轮采用应用层显式授权，暂不启用 RLS；记录边界、缺点与引入 RLS 的触发条件，为后续业务定义租户字段和关联约束原则。
- 补充严格输入、事务失败、未知提交、权限变更竞态、单元/真实 PostgreSQL 18/CI 验收及 API 文档任务。

**已确认范围：** 用户确认成员管理独立 Change，本轮仅创建流程自动写入 owner。多对多模型与成员读取授权可用，member 角色通过隔离测试夹具验收；本轮没有用户加入他人 Workspace 的产品入口，不能宣称已交付完整加入流程。

不实现项目 CRUD、监控、告警、AI Agent、Redis、OAuth/JWT、邮件/邀请、复杂 RBAC、计费或前端；不新增改名、删除、退出、成员列表、添加/移除成员、角色变更或所有权转移接口。

## Capabilities

### New Capabilities

- `workspace-persistence`：租户与成员约束、原子创建、身份状态与事务协调、显式迁移及数据保留。
- `workspace-access`：三个 API 的输入与响应、成员读取授权、两种角色、隔离、CSRF、安全失败及可执行验收。

### Modified Capabilities

- `user-authentication`：将既有“未来 Workspace 事务授权”约束接入本轮明确的业务边界，保持认证仅提供时点身份，不授予 Workspace 权限；原 me/login/logout 行为不变。

## Impact

后续 Apply 预计增加 `internal/workspace` 和 `migrations/00003_create_workspaces.sql`，接线 `internal/app/routes.go`、`cmd/api/main.go`；窄范围提取 `internal/auth` 的 Cookie 名称/CSRF 协议复用，保留现有测试。复用 pgx/v5、Goose v3.26.0、共享连接池、Go 1.24.3 与固定 PostgreSQL 18 镜像，不引入新服务或 ORM。

更新 OpenAPI、README、迁移说明、集成夹具和 CI 分支触发；旧迁移 1/2 不修改，readiness 仍仅检测连接，不在启动自动迁移。规划基线为干净的 `main=origin/main=99f3f516db284ef31b70fa9131d3653dd61cd40a`（2026-10-15，Asia/Shanghai），前三个 Change 已归档。本轮仅创建规划产物，不 Apply、不执行数据库迁移、不创建 PR；完成后等待独立技术评审。


## Review and Apply Authorization

2026-10-15，用户确认Proposal、Design、Tasks和三份Delta Specs全部通过独立技术评审，正式批准在codex/add-workspace-foundation实施，并创建指向main的PR，提供真实验收及CI证据后等待独立代码审查。上文“本轮仅规划”记录提案阶段边界；当前授权允许本Change实施和隔离测试迁移，不允许合并、归档或公开部署，业务范围不变。

# Proposal

## Why

Workspace 已支持成员查询、移除和退出，但没有经目标用户同意的加入途径。现在增加绑定已注册用户的平台内邀请，并修复允许重新加入后旧 DELETE 可能删除新成员关系的协议缺口，形成最小、可独立验收的成员生命周期。

## What Changes

- Owner 使用目标用户 UUID 创建平台内定向邀请；目标以自己的有效 Cookie Session 查看、接受或拒绝，Owner 查看本 Workspace 邀请并撤销。创建邀请绝不创建成员关系；接受只授予固定 `member`。
- 增加 pending、accepted、declined、revoked、expired 状态、固定 7 天有效期、单 Workspace/目标至多一条存储 pending、明确重试与终态语义。无邀请码、邮件或全局用户搜索。
- **BREAKING**：名单增加 `membership_id`；既有成员 DELETE 强制单一强 `If-Match` 条件，缺失返回 428，关系缺失/条件不匹配返回 412，替代旧的缺失目标 204，以保证旧请求不删除重新加入的新关系。用户已允许将此方案列入提案，兼容性变更仍须独立评审批准后才实施。
- 追加 Goose 版本 4：邀请表和成员关系代际 UUID；已应用 Migration 1/2/3 字节不变。接受的授权、邀请迁移与关系插入在同一有界事务中提交。
- 完整规划 PG18 的真实锁屏障、未知 Commit、跨租户和既有 API 回归，以及普通/race 两类 CI；本次仅规划，不执行数据库操作、Apply、PR、部署或归档。

## Capabilities

### New Capabilities

- `workspace-invitations`: 定向邀请的产品策略、最小 API、状态机、接收者隐私、明确同意和并发验收。

### Modified Capabilities

- `workspace-members`: 名单投影、条件删除与重加保护；保留 Owner/Member 删除权限和最后 Owner 规则；邀请接受成为唯一新增加入入口，禁止直接 POST members 或授予 Owner。
- `workspace-persistence`: Migration 4、不可复用的 membership_id、邀请约束与索引、有界事务锁协议、接受原子性和未知提交处理。
- `workspace-access`: 将邀请纳入 Cookie/CSRF/安全响应与真实 PG18 验收，保持现有 Workspace DTO 和租户查询规则。

## Impact

基线经 2026-10-15 只读核验：本地 main、origin/main、GitHub main 均为 `fbafb4a890396293d137889ecc85ea7c706f923e`；第五 Change 已归档到 `2026-10-15-add-workspace-members`，三份 delta 已同步主规格，工作区最初干净。源码依据为 `internal/workspace/member_delete.go`、`members.go`、`store.go`、`internal/session/store.go`、`internal/app/routes.go`、Migration 1/2/3 和现有 integration/CI。

未来实现会影响 workspace/invitation handler、store、显式路由、成员删除调用方、OpenAPI、README、迁移文档及测试；不新增技术依赖、配置项或 Worker。2026-10-15 用户已确认平台内邀请、7 天有效期、发起者失权后继续有效、有限 UUID 可邀请性泄露、终态重复动作409和100条有效 pending 上限，详见 Design D1。条件 DELETE、Migration 和事务协议仍待独立技术评审，不将产品确认视为实施授权。

本轮排除邮件/完整通知、全局用户搜索、OAuth/SSO/JWT、复杂 RBAC、Owner 转让、Workspace 删除、项目 CRUD、HTTP 监控/告警/AI Agent、Redis、前端页面、计费/订阅。公开部署前的网关限流、反滥用和运维准备是后续发布门槛，不扩展本轮业务能力。

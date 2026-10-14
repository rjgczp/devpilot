# Proposal

## Why

DevPilot AI 的 Go 运行基线已经完成，但 SaaS 平台还不能持久化用户或接受注册。现在以一个可独立验收的注册切片建立用户数据、安全密码存储与业务 API 约定，为后续认证提供基础。

## What Changes

- 引入版本化、可重复执行的 SQL migration 工作流，创建具有 UUID 主键、规范化邮箱唯一约束、密码哈希、账户状态与时间戳的 `users` 表。
- 新增 `POST /api/v1/auth/register`，实施严格 JSON/字段校验、Argon2id 哈希、原子插入以及一致的业务响应和安全错误响应。
- 保留 ASCII 子集和全地址小写作为 MVP 邮箱产品策略；数据库唯一约束作为并发一致性的最终保证，不声称其他邮箱格式普遍无效。
- 明确 `active` 仅表示账户未禁用，`email_verified_at=NULL` 表示邮箱所有权尚未验证，后续功能不得仅凭 active 信任邮箱。
- 暂定 Argon2id 64 MiB/t=3/p=1、每进程两个真实执行槽；4096-byte 请求上限和廉价校验先于哈希，取消不能提前释放仍在使用的槽位。性能预算以实测确认，不得擅自降低安全参数。
- 区分邮箱冲突409、连接不可用503、数据库超时503与缺 users 表等内部部署错误500；无法确认 INSERT 结果时不保证一定未创建账户，不暴露原始 PgError；客户端取消/断连不保证也不强制尝试返回503。
- 复用 Goose 原生 CLI、事务与版本机制，迁移必须显式执行；共享/生产环境限制破坏性 Down。数据库仅对 password_hash 约束非空和合理长度，PHC 完整性由密码模块和测试负责。
- 严格检查原始JSON的Unicode转义和代理对，拒绝未配对代理项，禁止只检查解码后的UTF-8；密码保持15–128 Unicode码点、最多512 UTF-8 bytes，禁止裁剪/截断/归一化。
- 补充单元测试、真实 PostgreSQL 18 集成测试、CI 验证、API 契约与迁移/开发说明。
- 本 Change 只覆盖注册；不实现登录、JWT、Session、OAuth、Workspace、RBAC、监控任务、AI Agent、Redis 或邮件发送/邮箱验证流程。
- 两个哈希槽不是完整反滥用限流。本次记录并验收公开部署前置要求：另行实现注册限流与防滥用控制，并完成部署评审；不在本 Change 扩展该系统。

## Capabilities

### New Capabilities

- `user-registration`: 注册请求校验、安全密码处理、账户创建、重复邮箱与安全响应，以及可执行的验证和 API 文档契约。
- `user-persistence`: 用户表约束、规范邮箱唯一性及版本化数据库迁移的执行、失败与回退行为。

### Modified Capabilities

无。沿用 `backend-runtime` 和 `service-health`：环境变量配置、slog 安全日志、HTTP 超时、共享 pgx 池及优雅退出保持现有契约；不增加启动时自动迁移或 schema 就绪检查。

## Impact

- 后续 Apply 预计新增 `migrations/`、`internal/registration/`、`internal/password/`、`internal/user/`、`docs/api/openapi.yaml` 与迁移说明；在 `cmd/api` 组合注册与现有健康路由。迁移使用固定版本 Goose CLI，不增加自定义迁移入口或引擎。
- 复用 Go 1.24.3 模块基线、`net/http`、`log/slog`、pgx/v5 与 PostgreSQL 18。预计增加兼容当前 Go 基线的 `golang.org/x/crypto`、Goose v3；本次不引入 ORM、sqlc 或新 HTTP 框架。
- 现有开发数据库新增 `users` 和迁移版本表；集成测试使用独立测试库，禁止清理开发数据。CI 将增加 PostgreSQL 18 服务和强制集成检查。
- 用户已授权：完成最后规格调整、一致性检查和严格校验后进入Apply，逐项实施、实际测试并创建PR等待独立审查；不得自动合并或归档。

# Proposal

## Why

DevPilot AI 已完成用户注册，但注册不会签发任何认证凭据，当前无法登录、退出或查询当前身份。本 Change 在已合并归档的 Go/PostgreSQL 基线上建立可撤销的服务端会话，为后续业务提供可信的 user ID。

## What Changes

- 新增 `POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/me`，统一安全响应与依赖故障语义。
- 新增 PostgreSQL Session migration：只存高熵随机令牌的安全摘要，支持固定过期、撤销、账户禁用检查及有界过期清理。
- 扩展现有密码模块，严格解析 Argon2id PHC 并在计算前校验参数、编码和长度；不存在邮箱、错误密码及禁用账户使用相同认证失败契约与真实 dummy 验证。
- 注册和登录共享每进程最多两个 Argon2 槽；登录另外限制包括用户读取、密码验证和会话创建的完整流程并发，不把资源上限当作限流。
- 定义 host-only Cookie、HTTPS 默认安全配置、本地 HTTP 显式例外，以及登录/退出的 Origin、自定义请求头 CSRF 防护。
- **BREAKING（启动配置）**：增加必填 `AUTH_ORIGIN`；默认 `AUTH_MODE=production` 只接受 HTTPS Origin。开发 HTTP 必须显式配置 `AUTH_MODE=development`、回环 Origin 和回环监听。更新所有启动测试及示例。
- 扩展单元、真实 PostgreSQL 18 集成测试、CI、OpenAPI 和操作文档，记录公开部署前必须实现并评审的登录限流、防暴力破解和现有注册防滥用要求。

不实现 JWT、OAuth、SSO、邮箱验证、密码找回、Workspace、RBAC、项目管理、Redis、多设备管理 UI、前端页面、AI Agent、通用认证框架或微服务拆分。限流系统作为公开部署前的独立交付，不在本轮实现。

## Capabilities

### New Capabilities

- `user-authentication`：严格登录输入、账户与密码验证、统一失败、当前身份、退出、安全错误及资源预算。
- `session-management`：会话令牌、持久化、过期/撤销、Cookie/环境配置、CSRF 与清理生命周期。

### Modified Capabilities

- `user-registration`：注册/登录共用每进程两个槽，注册原完整合约及回归测试不变；登录专属故障不能泄漏容量或污染注册状态，明确共享容量竞争/共同DB故障的隔离限制。

## Impact

影响 `internal/password`、`internal/registration`、`internal/user`、`internal/config`、`internal/app` 和 `cmd/api`；拟新增 `internal/auth`、`internal/session` 及 `migrations/00002_create_sessions.sql`。复用 pgx、现有连接池、Goose、HTTP 生命周期与测试库隔离规则；不引入新的存储服务或升级 Go 工具链。

CI 当前只监听 main/注册分支的 push，需纳入登录分支；集成夹具对单份 migration、单步 Down 删除 users、失败 migration 版本 2 的假设必须改为完整迁移链验收。文档扩展 `README.md`、`.env.example`、`docs/api/openapi.yaml`、`docs/database-migrations.md`。本 Change 不改变 connectivity-only readiness，不修改已应用的 users migration，不执行共享/生产迁移或部署。

## Final Review Status

最后技术审查完成的规划内容：两份新能力delta明确每项Requirement的成功、异常、安全、并发场景；24h绝对过期不变，无滑动续期/Refresh Token；未来Workspace事务授权与入口身份验证分离。用户正式批准进入Apply并明确本轮实现自动Session清理，保留Worker设计、Requirement和任务6.1/6.2；不创建独立Change。实施后创建PR等待代码审查，不合并、归档或公开部署。

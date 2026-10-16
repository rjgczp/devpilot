# Proposal

## Why

DevPilot AI 已有可靠的 Workspace 成员与邀请边界，但尚无承载未来 Monitor、Incident、Alert 的项目资源。现在建立不可迁移的项目租户归属、当前权限校验和并发更新契约，避免后续资源通过已知 UUID 越权或发生跨租户引用。

## What Changes

- 新增 Workspace 下 Project 创建、列表、详情与条件 PATCH；建议 Owner 可写、Owner/Member 可读，完全继承 Workspace 权限，无项目成员表。
- 建议允许同 Workspace 重名，不引入 slug、status、软删除或归档；本轮不开放 DELETE。所有产品推荐等待用户确认及独立技术评审，文档完成不代表批准 Apply。
- Project 使用数据库 UUIDv7、固定 workspace_id、名称、可空缺但存储为空串的描述、创建者来源、数据库时间及递增 version；提供 ETag/If-Match 防止丢失更新。
- 新增 Migration 5 计划：projects、租户复合唯一约束、不可变字段保护及必要索引。Migration 1/2/3/4 字节不变，不创建未来业务表。
- 查询在数据库快照内同时验证可信 Session、当前成员关系与项目归属；写事务延续 User → Session → Workspace → Membership 顺序，再保护 Project，锁等待后重新门控。
- 增加真实 PostgreSQL 18 隔离、并发、故障、迁移及全历史回归计划，并接入既有 Race Detector/CI。明确响应丢失不代表回滚，不提供创建幂等承诺。
- 本次仅 Propose，生成规划文件；不编辑业务代码、主 Specs、Migration、CI 或 API 文档，不执行数据库操作、不创建 PR。

## Capabilities

### New Capabilities

- `project-management`: 项目 API、字段验证、继承权限、列表、条件更新、安全错误和资源生命周期边界。
- `project-persistence`: 项目持久化、复合租户引用、授权事务锁、提交结果、Migration 5 与真实 PostgreSQL 验收。

### Modified Capabilities

- `workspace-access`: 扩展租户验收边界，允许本次经评审的项目表，与既有仅允许邀请 schema 的限定衔接；保留成员、邀请及认证行为。

## Impact

后续 Apply 预计新增 `internal/project/`、`migrations/00005_create_projects.sql` 与项目测试；小范围接入 `internal/app/routes.go`、`internal/app/server.go`，更新 `docs/api/openapi.yaml`、`docs/database-migrations.md`、README 和 `.github/workflows/go-ci.yml`。既有 Schema 测试需固定历史版本边界，并将临时失败版本移至 6，避免与真实 Migration 5 冲突。不升级 Go/pgx/Goose/PG，不引入 RLS、缓存或新服务。无旧 API 破坏性变更；新增 PATCH 从第一版即要求 If-Match。

基线（2026-10-16，Asia/Shanghai）：本地干净，main=origin/main=GitHub main=`ce7e41c378d2add28697509f85725e0f31d92129`；[PR #6](https://github.com/rjgczp/devpilot/pull/6) 已合并；第六 Change 已归档于 `openspec/changes/archive/2026-10-16-add-workspace-invitations/` 并同步四份主 Specs，启动前无 active change。全库 strict 的四份既有主规格长度警告不属于未完成归档，详见 design；不借本轮重写其历史契约。

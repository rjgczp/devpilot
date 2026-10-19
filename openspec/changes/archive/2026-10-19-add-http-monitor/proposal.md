# Proposal

## Why

DevPilot AI 的 Project 已具备当前成员授权和条件更新边界，但还不能保存 HTTP 监控配置。现在建立最小配置资源、不可变租户归属及清晰的 URL 安全契约，为后续探测引擎提供可信配置来源，避免把字符串校验误称为 SSRF 防护闭环。

## What Changes

- 新增 Project 下 Monitor 的 POST、列表 GET、详情 GET、条件 PATCH；推荐当前 Owner 写、Owner/Member 读，继承 Workspace 权限。
- 推荐必填 name/url，GET 固定方法，interval_seconds 默认60（允许60/300/900/1800/3600），timeout_seconds 默认10（整数1–30且小于interval），expected_status 默认200（单个200–599状态码）。无 enabled/paused/desired_state、删除、归档或结果字段；保存配置不表示已运行，未来引擎不得自动激活全部存量配置。
- 推荐只接收 HTTP(S) DNS 域名、对应默认端口；禁止 IP 字面量、用户信息、query、fragment、自定义 Host/Header。ASCII/punycode、大小写、尾随点、路径编码有确定策略；写入仅纯语法/策略检查，不解析 DNS、不连接目标。
- 定义未来执行期必须完成的 DNS/IP 分类、连接目标绑定、TLS SNI/证书校验、禁止不受控代理/重定向与出口网络控制，明确这些不在本轮实现或验收完成之列。
- 规划 Migration6：http_monitors、复合 Project 外键、不可变身份保护、版本及最小索引/CHECK；Migration1–5字节不变，旧业务数据不变。
- Store 在 SQL 内校验可信 Cookie 身份、当前 Membership、Project/Monitor 全归属；写事务保护后新语句重新校验，ETag/If-Match 防止丢失更新，未知 Commit 不自动重放。
- 规划真实 PG18、Race Detector、无目标网络副作用、迁移与前七阶段回归，保留历史迁移固定边界。
- 本轮仅创建 Propose 规划文件，产品选择是待确认推荐，须独立技术评审后由用户另行授权 Apply；不编辑业务代码、主规格、迁移、CI/API文档，不执行数据库迁移，不创建 PR。

## Capabilities

### New Capabilities

- `http-monitor-management`: 配置模型、API/权限、严格输入、URL规范化、脱敏、分页、条件更新及无探测生命周期。
- `http-monitor-persistence`: 固定租户关系、数据库约束、授权事务/锁、版本与提交结果、Migration6、PG18与回归验收、未来安全读取版本契约。

### Modified Capabilities

- `workspace-access`: 最小扩展已审核租户资源范围以允许 HTTP Monitor 配置及其验收；既有认证、成员、邀请、Project契约不变。Project已有复合子引用要求可直接满足，无需修改 `project-persistence`。

## Impact

后续 Apply 预计新增 `internal/monitor/`、`migrations/00006_create_http_monitors.sql` 与单元/集成测试；接入 `internal/app/routes.go`、`internal/app/server.go`，更新 `docs/api/openapi.yaml`、`docs/database-migrations.md`、README、CI分支触发。复用现有 Go1.24.3/net/http/pgx与Goosev3.26.0/PG18；如验证punycode使用 `golang.org/x/net/idna`，Apply需选定并固定兼容现有工具链的版本，不采用隐式latest或升级Go。失败迁移临时版本从6移至7；历史1–5测试继续使用明确边界。

基线（2026-10-19，Asia/Shanghai）：工作区初始干净；HEAD/main/origin/main/远端refs/heads/main均为 `66dcf69e60c457966ee3390ee2838a6076fae059`（远端只读 `git ls-remote` 确认）；[PR #7](https://github.com/rjgczp/devpilot/pull/7) 合并提交 `61ffbbd`，归档提交 `66dcf69`；第七 Change 位于 `openspec/changes/archive/2026-10-16-add-project-management/`，主规格已存在，初始活动Change为空。不会覆盖既有未提交工作。

# Proposal

## Why

DevPilot AI 目前只有项目简介，缺少可运行、可验证的 Go 后端入口。先建立服务运行和本地 PostgreSQL 开发基础，可以让后续的租户管理、监控数据接入与 Python AI 诊断集成在稳定的工程约定上逐步实现。

## What Changes

- 建立可启动的 Go HTTP 服务基础工程，仅从环境变量加载并校验配置，输出不含敏感信息的结构化日志。
- 提供独立于数据库的进程存活检查，以及检查 PostgreSQL、可在故障恢复后自动恢复的就绪检查；服务开始关闭后不再报告就绪。
- 支持 SIGINT、SIGTERM 触发的 10 秒总预算内优雅退出，停止接收请求并释放数据库连接等资源；超时或异常时记录原因并非零退出。
- 提供仅绑定本机回环地址的 PostgreSQL 18 开发环境、无真实凭据的 `.env.example` 和启动说明；明确 HTTP Server 超时默认值。
- 为启动与退出、非法配置、健康检查、数据库故障与恢复添加自动化测试和本地验收步骤。
- 本次不建立租户表、数据库迁移、业务 API、监控采集、任务调度、Redis、Python AI 服务、完整认证框架或额外安全中间件。

## Capabilities

### New Capabilities

- `backend-runtime`: Go 服务从经过校验的环境变量配置启动，输出安全的结构化日志，并在信号触发后有界停止。
- `service-health`: 分别报告进程存活和 PostgreSQL 就绪状态，涵盖数据库故障恢复与服务关闭阶段。

### Modified Capabilities

无。

## Impact

- 新增 Go 模块、HTTP 服务入口及内部配置、数据库连接和健康检查代码。
- 新增 `GET /healthz` 与 `GET /readyz` 两个运维接口；尚无既有 API 兼容负担。
- 新增 Docker Compose PostgreSQL 服务、环境变量示例和本地运行文档。
- 引入 PostgreSQL Go 驱动；本地开发需要 Go 与 Docker Compose。

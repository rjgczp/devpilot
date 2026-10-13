# service-health Specification

## Purpose

定义 Go 服务对外提供的存活与就绪信号，使本地开发和未来的部署系统能分别判断进程是否响应、PostgreSQL 依赖是否可用。

## Requirements

### Requirement: Liveness endpoint
The service SHALL expose `GET /healthz` and return HTTP 200 while its HTTP process is running and the endpoint can be served. The liveness result SHALL reflect only the Go process and SHALL NOT depend on PostgreSQL availability.

#### Scenario: Service is running
- **WHEN** a client requests `GET /healthz` while the HTTP process is running
- **THEN** the response status is HTTP 200

#### Scenario: PostgreSQL is unavailable
- **WHEN** PostgreSQL is unavailable and a client requests `GET /healthz`
- **THEN** the response status remains HTTP 200

### Requirement: Database readiness endpoint
The service SHALL expose `GET /readyz` and check PostgreSQL connectivity within a bounded time for each request while it is accepting work. It SHALL mark itself not ready upon receiving SIGINT or SIGTERM, before HTTP shutdown begins. It SHALL return HTTP 200 when PostgreSQL is reachable and the service is not shutting down, and HTTP 503 when the check fails, times out, or shutdown has begun.

#### Scenario: PostgreSQL is reachable
- **WHEN** a client requests `GET /readyz` and PostgreSQL responds within the check timeout
- **THEN** the response status is HTTP 200

#### Scenario: PostgreSQL is unavailable
- **WHEN** a client requests `GET /readyz` and PostgreSQL is unavailable or the check times out
- **THEN** the response status is HTTP 503 and the HTTP process remains available

#### Scenario: PostgreSQL recovers
- **WHEN** PostgreSQL becomes reachable after a failed readiness check and the service has not begun shutting down
- **THEN** a subsequent `GET /readyz` returns HTTP 200 without restarting the service

#### Scenario: Shutdown has begun
- **WHEN** the service has begun shutdown and `GET /readyz` can still be served
- **THEN** the response status is HTTP 503 even if PostgreSQL is reachable

### Requirement: Health response does not expose secrets
Health responses SHALL contain only a minimal status indication and SHALL NOT contain connection strings, credentials, or internal database error details.

#### Scenario: Readiness check fails
- **WHEN** a client requests `GET /readyz` while PostgreSQL is unavailable
- **THEN** the response reveals the readiness state without exposing connection settings or database error details

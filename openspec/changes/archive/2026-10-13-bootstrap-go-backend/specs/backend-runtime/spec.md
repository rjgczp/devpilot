# Spec Delta

## Purpose

定义 Go 后端的基础运行行为，使开发者能够用明确的配置启动服务、通过结构化日志观察运行状态，并在停止进程时可靠地释放资源。

## ADDED Requirements

### Requirement: Configuration controls startup
The Go service SHALL obtain runtime configuration only from environment variables. It SHALL validate the listen address, PostgreSQL connection settings, log level, and positive readiness and shutdown timeouts before listening. The shutdown timeout SHALL default to 10 seconds and remain configurable. Invalid configuration SHALL cause a non-zero exit with an actionable error that does not expose secrets.

#### Scenario: Valid configuration
- **WHEN** the required configuration is valid
- **THEN** the service starts listening at the configured address

#### Scenario: Invalid configuration
- **WHEN** a required value is missing or a configured value is malformed
- **THEN** the service exits before listening and identifies the invalid setting without exposing credentials

#### Scenario: Configuration file is not imported automatically
- **WHEN** a `.env` file exists but a required variable is absent from the process environment
- **THEN** the service rejects the missing variable rather than reading the file implicitly

#### Scenario: Default and invalid shutdown timeouts
- **WHEN** `SHUTDOWN_TIMEOUT` is absent or set to a non-positive or malformed value
- **THEN** the service uses 10 seconds when absent and rejects the invalid value before listening

### Requirement: Safe sample configuration
The repository SHALL provide `.env.example` with the required variable names and local-development examples, without real credentials. The real `.env` file SHALL remain excluded from version control.

#### Scenario: Developer prepares local configuration
- **WHEN** a developer copies `.env.example` for local use
- **THEN** the example contains no production or other real credentials, and the resulting `.env` is ignored by Git

### Requirement: Local PostgreSQL environment
The repository SHALL provide a PostgreSQL 18 development environment whose published database port is accessible only through the local loopback address. The database password SHALL be supplied through an environment variable and SHALL NOT be hardcoded or committed as a real credential.

#### Scenario: Local database is started
- **WHEN** a developer starts the documented database environment with a password in the process environment
- **THEN** PostgreSQL 18 becomes available on `127.0.0.1` and its port is not published on external interfaces

#### Scenario: Password is absent
- **WHEN** the required database password environment variable is missing
- **THEN** the local database environment does not silently start with a hardcoded password

### Requirement: Structured operational logs
The Go service SHALL emit machine-readable logs for startup, shutdown, and operational errors at the configured level. Logs SHALL NOT include `DATABASE_URL`, database passwords, or other secret configuration values.

#### Scenario: Service starts
- **WHEN** the service begins listening
- **THEN** it emits a structured startup log entry

#### Scenario: Database operation fails
- **WHEN** a database connectivity check fails
- **THEN** the service emits a structured error entry without `DATABASE_URL` or the connection password

### Requirement: Graceful termination
The Go service SHALL handle both SIGINT and SIGTERM with one total shutdown deadline, defaulting to 10 seconds. It SHALL stop accepting new requests, allow in-flight requests to finish within that deadline, cancel managed work through context, and close its database connection pool and other managed resources before a successful exit.

#### Scenario: SIGINT with in-flight request
- **WHEN** the running service receives SIGINT while an in-flight request finishes within the shutdown timeout
- **THEN** it stops accepting new requests, allows that request to finish, closes owned resources, and exits successfully

#### Scenario: SIGTERM with in-flight request
- **WHEN** the running service receives SIGTERM while an in-flight request finishes within the shutdown timeout
- **THEN** it follows the same graceful shutdown behavior and exits successfully

#### Scenario: Shutdown deadline expires
- **WHEN** an in-flight request exceeds the shutdown limit
- **THEN** the service records the timeout, force-closes remaining HTTP connections, attempts managed-resource cleanup without extending the deadline, and exits non-zero; unfinished requests or uncooperative background work are not guaranteed to complete

#### Scenario: Shutdown fails before the deadline
- **WHEN** HTTP shutdown or managed-resource cleanup returns an error before the deadline
- **THEN** the service records the reason without secrets and exits non-zero

### Requirement: Bounded HTTP connections
The HTTP service SHALL default to a 5-second read-header timeout, 10-second read timeout, 10-second write timeout, and 60-second idle timeout.

#### Scenario: Client does not finish sending headers
- **WHEN** a client holds an incomplete request header past the configured header timeout
- **THEN** the server closes that connection without allowing it to remain open indefinitely

#### Scenario: HTTP timeout defaults
- **WHEN** the server starts with its default HTTP configuration
- **THEN** it applies the documented 5-second, 10-second, 10-second, and 60-second values to the respective HTTP timeouts

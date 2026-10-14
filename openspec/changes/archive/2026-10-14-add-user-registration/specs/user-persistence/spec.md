# Spec Delta

## Purpose

定义 DevPilot AI 用户数据的持久化完整性与版本化数据库迁移行为，保证用户身份、邮箱唯一性及密码字段在并发与失败情况下仍可靠，并使开发者能够安全执行和验证 schema 变更。

## ADDED Requirements

### Requirement: Complete user records
The users table SHALL require a database-generated UUIDv7 primary key, canonical email, password_hash, status, created_at and updated_at, with nullable email_verified_at. Timestamps SHALL default to database timestamptz values. Status SHALL default to active and allow only active or disabled. Required fields SHALL be NOT NULL. The database SHALL constrain password_hash only to 1–1024 bytes, without algorithm, PHC-format or parameter checks; the password module SHALL validate PHC integrity.

#### Scenario: Complete new user
- **WHEN** registration persists a valid new account
- **THEN** the database generates a non-null unique UUIDv7, equal initial created_at and updated_at timestamps, active status and null email_verified_at, storing a complete PHC hash rather than plaintext

#### Scenario: Invalid direct write
- **WHEN** SQL attempts null required fields, an unknown status, an empty password_hash or a password_hash longer than 1024 bytes
- **THEN** database constraints reject the write without retaining a partial row

#### Scenario: Password parameter evolution
- **WHEN** an explicitly reviewed password-module upgrade produces a valid PHC string with different parameters within the storage bound
- **THEN** the schema accepts that value without requiring a parameter-specific migration, and module tests rather than database regex checks establish its PHC validity

#### Scenario: PHC integrity is an application responsibility
- **WHEN** a direct SQL value is non-empty and within the length bound but is not a valid PHC string
- **THEN** the database does not claim to validate its format or prevent plaintext on that basis, while registration always supplies only password-module output and module/registration tests detect malformed output or plaintext persistence regressions

#### Scenario: Enabled account remains unverified
- **WHEN** a new row has status active and email_verified_at null
- **THEN** its state means only not disabled, and consumers requiring trusted email ownership cannot use status alone as evidence

#### Scenario: Client cannot choose identity
- **WHEN** registration is invoked by an untrusted client
- **THEN** the stored primary key, status and timestamps are assigned by the server/database rather than client payload, and no hash is exposed through the returned user projection

### Requirement: Canonical email uniqueness is enforced by PostgreSQL
The users table SHALL store ASCII canonical email and enforce lowercase, trimmed, non-empty and at-most-254-byte form with database checks. A named UNIQUE constraint SHALL be the final concurrency guarantee. Registration SHALL apply the MVP syntax policy, without claiming unsupported mailbox forms are universally invalid; direct noncanonical writes SHALL be rejected. SQL values SHALL be parameterized.

#### Scenario: Distinct canonical emails
- **WHEN** two valid users have distinct canonical email strings
- **THEN** both can be persisted and independently identified by their UUIDs

#### Scenario: Concurrent unique violation
- **WHEN** concurrent statements insert the same canonical email
- **THEN** only one insertion commits and the other fails on the named email unique constraint

#### Scenario: Application normalization bypass
- **WHEN** direct SQL attempts uppercase, surrounding whitespace, non-ASCII, empty or overlength email, or duplicates an existing canonical email
- **THEN** database checks or the unique constraint reject it, and SQL metacharacters passed as values cannot execute statements

### Requirement: Registration writes are atomic and non-destructive
User creation SHALL atomically insert one complete record or fail. A duplicate email SHALL NOT update existing rows. Only the named email unique violation SHALL be classified as an email conflict; other constraint errors SHALL NOT be misreported as duplicate email. Cancellation or connection loss after a possible commit SHALL NOT cause automatic retry or credential replacement.

#### Scenario: Atomic insert
- **WHEN** all user constraints are satisfied and insertion completes
- **THEN** one complete user row is committed and a safe public projection is returned

#### Scenario: Constraint failure classification
- **WHEN** insertion fails on a primary key or check constraint rather than email uniqueness
- **THEN** it does not change any existing user and is classified as an internal error rather than email_conflict

#### Scenario: Cancellation cannot promise rollback
- **WHEN** client cancellation arrives after INSERT has been submitted
- **THEN** database cancellation is propagated without assuming rollback, no automatic retry occurs, and callers do not promise a 503 response or a definitely uncreated account

#### Scenario: Retry after ambiguous commit
- **WHEN** the first connection fails after the database might have committed and a client retries
- **THEN** the database contains at most one row for the email, and a committed first attempt results in a duplicate response without changing its credentials

### Requirement: Versioned explicit user schema migration
The repository SHALL provide versioned SQL migrations and documented explicit up/status operations using the migration tool's native transaction and version mechanisms. Repeated up SHALL make no schema change. Each migration and version update SHALL be atomic. Shared deployment procedures SHALL serialize migration execution. The API SHALL NOT auto-migrate or require schema access to start; no custom lock or migration engine SHALL be required.

#### Scenario: Fresh schema and repeat application
- **WHEN** migration up runs against a fresh PostgreSQL 18 database and runs a second time
- **THEN** the first run creates the users schema and migration history, while the second changes neither schema nor existing data

#### Scenario: Failed migration
- **WHEN** migration SQL fails within the tool's native transaction
- **THEN** it exits non-zero, does not mark that migration successful and leaves no partially applied user schema

#### Scenario: Shared environment migration serialization
- **WHEN** two releases need to migrate the same shared database
- **THEN** the documented deployment procedure runs one migration operation at a time before the API rollout and does not assume unconfigured CLI locking or API startup serialization

#### Scenario: Startup without migrated schema
- **WHEN** the API starts with valid configuration against a database without the users table
- **THEN** the API can start, /healthz responds, /readyz continues to check connectivity only, and registration returns safe 500 internal_error until migration is explicitly applied

#### Scenario: Migration diagnostic access
- **WHEN** an operator handles migration connection or SQL failure
- **THEN** documented commands use environment credentials rather than DSN arguments, raw native-tool diagnostics stay in restricted operator output and are not exposed through HTTP, and only redacted summaries enter shared documentation or logs

### Requirement: Destructive migration rollback is operationally restricted
Documentation SHALL restrict native one-step down to explicitly identified disposable development/test databases. Shared and production release procedures SHALL allow only status/up and SHALL NOT run down, down-to, redo or reset as automatic rollback. Exceptional destructive recovery SHALL require separate approval, a verified backup and a reviewed recovery plan. API rollback SHALL preserve user data; schema correction SHALL prefer forward migrations.

#### Scenario: Disposable database round trip
- **WHEN** an operator confirms a database is disposable and runs native up, down, and up there
- **THEN** the users table is created, removed with its rows, and recreated with the expected constraints and current migration status

#### Scenario: Shared environment destructive rollback request
- **WHEN** a shared or production release proposes down, down-to, redo or reset as routine rollback
- **THEN** the release review/procedure rejects it and retains user data; documentation does not claim the native CLI itself enforces an environment check or custom confirmation flag

#### Scenario: Exceptional destructive recovery
- **WHEN** a shared or production database requires exceptional destructive recovery
- **THEN** it remains outside routine release commands and requires separate authorization, verified backup and recovery review before execution

#### Scenario: Reverting application code
- **WHEN** an operator reverts to the preceding API binary after the additive users migration
- **THEN** user data remains intact and the earlier health/runtime behavior can operate with the additional table present

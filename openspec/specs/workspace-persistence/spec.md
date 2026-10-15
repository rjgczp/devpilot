# workspace-persistence Specification

## Purpose

定义 Workspace 及用户成员关系的持久化完整性、原子创建和身份状态并发边界，使多租户授权有可靠的数据基础，并确保显式迁移、故障及未知提交情况下不损坏已有用户与会话数据。

## Requirements

### Requirement: Workspaces have database identities and bounded names
Workspaces SHALL have a database-generated UUIDv7 primary key, non-null name, created_at and updated_at. Database timestamps SHALL initially be equal. Names SHALL have 1–100 Unicode code points and at most 400 UTF-8 bytes; the database SHALL reject empty, overlong, ASCII-control-containing or ASCII-edge-whitespace values. Names SHALL NOT be unique or serve as authorization identifiers; no slug is defined.

#### Scenario: Success - Independent same-name workspaces
- **WHEN** one or different users create workspaces with the same valid name
- **THEN** distinct UUIDv7 rows with equal initial timestamps are stored, without a name conflict or shared ownership

#### Scenario: Exception - Invalid direct workspace write
- **WHEN** SQL supplies null required fields, an empty or overlong name, ASCII controls or leading/trailing ASCII whitespace
- **THEN** database constraints reject the row without a partially stored workspace

#### Scenario: Security - Identity is not a secret
- **WHEN** a client learns a workspace UUID or estimates its creation time
- **THEN** that knowledge grants no access; all access still requires current identity and membership

#### Scenario: Concurrency - Concurrent name reuse
- **WHEN** independently authenticated creation requests concurrently use identical names
- **THEN** each confirmed request creates its own distinct workspace and no name-level uniqueness conflict is invented

### Requirement: Membership represents a constrained many-to-many relationship
Membership SHALL require workspace_id, user_id, role and database-created created_at. The pair (workspace_id,user_id) SHALL be the primary key; role SHALL be only owner or member. Workspace deletion SHALL cascade memberships; user deletion SHALL be restricted while any membership references the user. An index starting with user_id then workspace_id SHALL support user-scoped enumeration.

#### Scenario: Success - Multiple memberships and roles
- **WHEN** a guarded fixture associates user A with workspaces X and Y, and user B with X
- **THEN** A has independent roles in X/Y, B shares X without owning Y, and no global user role determines either membership

#### Scenario: Exception - Referential or role violation
- **WHEN** direct SQL inserts an unknown user/workspace, null required value, duplicate pair or a role other than owner/member
- **THEN** the named primary-key, foreign-key, not-null or role constraints reject the invalid relation

#### Scenario: Security - Deletion does not silently orphan ownership
- **WHEN** SQL deletes a user with any workspace membership or deletes an isolated test workspace
- **THEN** user deletion is rejected without changing its sessions/memberships, while workspace deletion removes only that workspace's members and preserves users and other workspaces

#### Scenario: Concurrency - Duplicate relationship race
- **WHEN** two isolated test transactions insert the same membership pair
- **THEN** at most one commits and the other cannot overwrite the existing role; a user may still have different workspace pairs

### Requirement: Workspace and initial owner commit atomically
Creating a workspace SHALL commit exactly one workspace and one owner membership for the authenticated user in one transaction, or commit neither on a confirmed pre-commit failure. Clients SHALL NOT choose owner identity, role, UUID or timestamps. Success SHALL require confirmed commit. The product SHALL expose no alternate membership writer in this change.

#### Scenario: Success - Owner is present before publication
- **WHEN** an active authenticated user creates a valid workspace and commit is confirmed
- **THEN** exactly one workspace and its owner relation are visible together, and the returned role is owner

#### Scenario: Exception - Second insert or commit fails definitively
- **WHEN** owner insertion fails or the database explicitly rolls the transaction back
- **THEN** neither row persists and no successful creation response is returned

#### Scenario: Security - Client ownership override
- **WHEN** a request includes user_id, owner_id, role, id or timestamps
- **THEN** input validation rejects it without writing; SQL never trusts those fields for identity or authority

#### Scenario: Concurrency - Other connections observe atomic state
- **WHEN** a second connection reads while creation is paused between workspace insertion and owner insertion/commit
- **THEN** it cannot observe the uncommitted workspace, and after commit it sees the workspace with its owner relation

### Requirement: Creation coordinates with current identity state
Creation SHALL revalidate and protect the current user's active status and current session's unrevoked state until transaction end. A revocation or disable committed before the corresponding protected validation SHALL deny creation. Expiry SHALL be rechecked using database statement time at the workspace insertion gate; expiry after that gate SHALL NOT imply automatic rollback.

#### Scenario: Success - Live identity at insertion gate
- **WHEN** protected user/session validation succeeds and expires_at is strictly after the insertion statement time
- **THEN** the transaction can create the workspace and owner, without changing or renewing the session

#### Scenario: Exception - Cannot establish protected identity
- **WHEN** the database cannot complete protected validation or a bounded lock wait expires
- **THEN** creation does not proceed on an earlier identity result and returns its safe dependency/internal error if the client remains connected

#### Scenario: Security - Revoked disabled missing or expired identity
- **WHEN** revocation, disable, expiry or session deletion is effective before the applicable protected check
- **THEN** the request returns 401 unauthenticated and no workspace is created, even if an entry identity check previously passed

#### Scenario: Concurrency - Commit ordering is defined
- **WHEN** creation wins its protecting locks before revoke/disable, or revoke/disable wins first
- **THEN** in the first ordering creation commits or rolls back before the conflicting change proceeds, and in the second ordering creation observes the invalid state and is denied; no claim is made to cancel a completed or previously authorized operation

#### Scenario: Concurrency - Expiry while waiting or committing
- **WHEN** a session expires while creation waits for locks or expires after the insertion gate but before commit
- **THEN** the post-wait insertion gate denies the former, while the latter may commit; locks never claim to freeze time

### Requirement: Unknown creation outcomes are not retried automatically
Timeout, cancellation or connection loss after submission SHALL NOT be treated as proof of rollback. The service SHALL NOT automatically retry creation or return 201 without confirmed commit. A later explicit client retry SHALL be a new creation and can create a second same-name workspace. This version SHALL NOT promise idempotency or exactly-once creation.

#### Scenario: Success - Confirmed result is recoverable by listing
- **WHEN** creation committed but the client did not receive its response
- **THEN** the authenticated user's later list can contain the committed workspace and owner role

#### Scenario: Exception - Ambiguous commit
- **WHEN** commit may have reached PostgreSQL but its result is lost
- **THEN** no success is issued; a connected client receives a fixed safe error without any assertion of rollback and the server performs no second creation attempt

#### Scenario: Security - Canceled connection leaks no outcome detail
- **WHEN** the original request is canceled or disconnected during creation
- **THEN** cancellation propagates, no further business work or attempted response starts, and neither logs nor responses expose tokens, digests, SQL or raw driver errors

#### Scenario: Concurrency - Explicit duplicate submissions
- **WHEN** a client submits two valid creates concurrently or retries an uncertain result
- **THEN** distinct committed workspace/owner pairs are allowed, each pair stays atomic, and no deduplication by name or existing memberships occurs

### Requirement: Workspace migrations are additive explicit and recoverable
A new versioned transaction migration SHALL add both workspace tables and constraints without editing migrations 1/2 or existing data. Repeated up SHALL be a no-op. Startup/readiness SHALL retain their current no-auto-migration/connectivity-only contracts. Shared releases SHALL serialize status/up; destructive rollback SHALL remain restricted to guarded disposable databases.

#### Scenario: Success - Fresh and populated upgrades
- **WHEN** an empty schema or a populated version-2 users/sessions schema upgrades to version 3 and repeats up
- **THEN** the full schema is correct, old rows are preserved and repeat up changes neither schema nor rows

#### Scenario: Exception - Failed migration and missing schema
- **WHEN** a disposable test migration fails or the API runs before workspace migration
- **THEN** failed DDL/version changes roll back atomically, while startup/health remain available and valid authenticated workspace requests receive safe 500 internal_error for missing schema

#### Scenario: Security - Guarded one-step down
- **WHEN** one-step version-3 down runs in an explicitly guarded disposable test database
- **THEN** only workspace tables are removed and users/sessions remain; shared/production procedures do not use down/down-to/redo/reset for normal rollback

#### Scenario: Concurrency - Schema operation serialization
- **WHEN** migration tests or shared deployment operations run
- **THEN** schema mutation suites are serialized per database and release procedures use one controlled migration step without claiming unconfigured tool locking

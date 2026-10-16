# Spec Delta

## Purpose

定义Project的数据库身份、固定Workspace归属和租户一致引用，以及在Session失效、成员变化、并发编辑、迁移和提交结果不确定时必须保持的完整性；所有数据库验收使用真实且受保护的PostgreSQL 18环境。

## ADDED Requirements

### Requirement: Projects have constrained database identities and immutable provenance
Projects SHALL store generated UUIDv7 id, non-null workspace_id,name,description,created_by_user_id,created_at,updated_at and positive bigint version default1. Description SHALL default empty and initial timestamps SHALL be equal. Id,workspace_id,creator and created_at SHALL be immutable, including direct ordinary SQL updates. Project text and timestamp constraints SHALL reject invalid stored values within the documented database validation subset.

#### Scenario: Success - Defaults and minimally stored resource
- **WHEN** authorized creation supplies valid name and optional description
- **THEN** PostgreSQL generates UUIDv7/times/version1, absent description becomes empty, creator comes from protected Session and no slug/lifecycle/project-membership columns are required

#### Scenario: Exception - Invalid persistence boundaries
- **WHEN** guarded SQL attempts required-field nulls, nonpositive version, overlong text, invalid ASCII-control/edge name, forbidden ASCII description controls or updated_at before created_at
- **THEN** database constraints reject the row; NUL cannot be stored, and application-only Unicode classifications are not claimed as full SQL CHECK enforcement

#### Scenario: Security - Tenant and provenance cannot be moved
- **WHEN** ordinary SQL or a supported writer attempts to change id,workspace_id,created_by_user_id or created_at, including transfer to another existing Workspace
- **THEN** database immutability protection rejects actual changes without moving the project, and equal assignments do not alter provenance

#### Scenario: Concurrency - Independent defaults and protected version changes
- **WHEN** multiple creates or version-checked updates execute
- **THEN** each created resource has its own UUID, supported updates change only permitted text/version/updated_at, and timestamps cannot substitute for the monotonic version conflict boundary

### Requirement: Project references preserve tenant ownership and independent lifetime
Projects SHALL reference Workspace and creator User with delete RESTRICT, without a membership foreign key. They SHALL have UNIQUE(workspace_id,id), a Workspace-leading list access path and creator-reference index. Future child references SHALL include workspace_id with project_id in a composite FK. Creator status or membership loss SHALL NOT delete or reassign projects or grant continuing access.

#### Scenario: Success - Creator leaves and current members continue
- **WHEN** the creator leaves while another Owner remains, later becomes disabled or has Sessions revoked
- **THEN** project identity/provenance persist and other current Owner/Member access follows current Workspace roles without consulting creator authority

#### Scenario: Exception - Missing parent or referenced deletion
- **WHEN** guarded SQL uses an unknown Workspace/creator or deletes a referenced Workspace/User
- **THEN** FK constraints reject it without orphaning/deleting Project or partially changing existing dependent data; no public delete API is implied

#### Scenario: Security - Composite child reference rejects a tenant mix
- **WHEN** a guarded temporary child table references projects(workspace_id,id) and combines Workspace A with Project B
- **THEN** real PostgreSQL rejects the tuple although each individual UUID exists, while the matching pair succeeds; no production Monitor/Incident/Alert table is added

#### Scenario: Concurrency - Concurrent reference checks cannot create an orphan
- **WHEN** guarded parent deletion and project/temporary-child insertion are ordered on independent connections
- **THEN** FK locking and constraints preserve a valid reference or reject one operation, never commit an orphan or cross-tenant tuple

### Requirement: Project writes protect current identity and authority through completion
Every project create/update SHALL use one bounded transaction and coordinate User then Session then Workspace then Membership protection, followed by scoped Project protection for updates. Conflicting supported identity/member writes SHALL serialize. A fresh statement after protective waits SHALL revalidate active identity, strict Session expiry, current Owner membership and exact resource scope before the permitted mutation.

#### Scenario: Success - Protected Owner gate
- **WHEN** current active actor, valid Session and current Owner authority remain valid after all protective waits
- **THEN** creation/update uses that protected identity/scope and conflicting supported authority changes cannot overtake it before transaction end

#### Scenario: Exception - Current protection is unavailable
- **WHEN** identity is revoked/disabled/missing, membership is removed, Owner becomes Member, or required scope disappears before the final boundary
- **THEN** no project mutation is allowed from earlier checks; business failure is401/404/403 as appropriate and budget/database failure uses safe503/500

#### Scenario: Security - All actual DML carries tenant authority
- **WHEN** a caller replaces tenant/project/actor claims or a writer is invoked without a prior HTTP permission lookup
- **THEN** the actual database read/INSERT/UPDATE requires trusted digest-derived identity and current membership with workspace ownership, never an id-only or caller-asserted-role write

#### Scenario: Concurrency - Revoke disable remove exit and role orderings
- **WHEN** Project write wins its protective locks first or the conflicting revoke/disable/conditional removal/self-exit/coordinated role change wins first
- **THEN** the former can finish before the change proceeds, while the latter is observed and denied by fresh protected state; rejoin as Member does not restore a delayed old Owner write

#### Scenario: Concurrency - Expiry after every wait and after the gate
- **WHEN** Session expires during User/Session/Workspace/Membership protection or update Project lock wait, or after the final gate
- **THEN** every pre-gate expiry is denied401 using a new database statement time, while post-gate expiry can commit; no lock is claimed to freeze time

#### Scenario: Concurrency - Compatibility and independent tenants
- **WHEN** Project writes overlap invitation/member writes, cleanup or writes in another Workspace
- **THEN** lock order remains compatible, cleanup-first can deny401 and Session-protection-first causes cleanup to skip, same-Workspace writes serialize and independent Workspaces need no global resource lock

### Requirement: Project versions make concurrent edits atomic
A successful PATCH SHALL atomically require the expected version with workspace_id and project_id, change supplied mutable fields, increment version and return updated state. Two writes using one version SHALL NOT both succeed. Equal-value edits SHALL consume a version; stale tags SHALL remain invalid after text returns to an earlier value. Version overflow SHALL fail safely without wrapping or partial change.

#### Scenario: Success - Partial edit persists once
- **WHEN** a current Owner updates description with the current condition
- **THEN** name/provenance remain intact, version increments once and updated_at is database-derived and nondecreasing only after confirmed commit

#### Scenario: Exception - Stale condition and overflow do not mutate
- **WHEN** a condition is stale or version cannot be incremented safely
- **THEN** the former is412, the latter500, and a confirmed rollback leaves all fields/version unchanged without refreshing the condition

#### Scenario: Security - Version is not resource authority
- **WHEN** another tenant's valid id/version is supplied with a permitted Workspace path
- **THEN** resource scope still denies404 and no id-only version update touches the foreign project

#### Scenario: Concurrency - Distinct Owners and no lost update
- **WHEN** two different Owners concurrently edit the same version, for equal or disjoint text fields, and one later replays its original tag
- **THEN** at most one first update commits, the other/replay gets412, both attempts use actual protected state and no stale write silently overwrites or merges

### Requirement: Project outcomes require confirmed commit without automatic replay
Project writes SHALL report success only after confirmed Commit. Timeout, cancellation or lost acknowledgement SHALL NOT prove rollback or trigger automatic replay, compensation or another Commit. POST SHALL make no idempotency promise; explicit repeats can create duplicates. PATCH retries SHALL retain the original precondition and reauthorize, with no automatic condition refresh.

#### Scenario: Success - Authorized observation after response loss
- **WHEN** a Project transaction commits but its HTTP response is lost
- **THEN** later authorized reads can observe its actual committed state, without claiming name uniquely identifies the uncertain create

#### Scenario: Exception - Confirmed rollback differs from uncertain Commit
- **WHEN** a real transaction rolls back before an injected error or real Commit succeeds before acknowledgement is lost
- **THEN** the former preserves old state and the latter can persist new state, neither returns unconfirmed success and connected clients get documented safe503/500

#### Scenario: Security - Cancellation cannot become a recovery writer
- **WHEN** the client disconnects or cancellation arrives during submission/Commit
- **THEN** it propagates, no further business write/response starts, connections are released or discarded safely and no background compensation claims to restore prior state

#### Scenario: Concurrency - Uncertain creation and version replay
- **WHEN** explicit duplicate creates overlap an uncertain request, or PATCH committed with a lost receipt is retried with the old tag
- **THEN** duplicate creates remain permitted independent resources, PATCH gets412 if that version committed, and each original request submits Commit at most once

### Requirement: Migration five is additive explicit and transactionally recoverable
Goose version5 SHALL add only reviewed project schema without modifying migrations1–4 or old data. Up SHALL support empty and populated version4 databases and repeat safely. A failed real version5 SHALL roll back its schema and version record. Startup SHALL NOT migrate and readiness SHALL remain connectivity-only. Shared releases SHALL serialize status/up; destructive rollback SHALL remain guarded-disposable-only.

#### Scenario: Success - Fresh upgrade and repeated up
- **WHEN** an empty database reaches5 or populated4 with users/Sessions/Workspaces/member generations/invitation history upgrades and repeats up
- **THEN** projects/constraints/indexes/immutability protection exist, old fields/relationships remain byte-equivalent in value and no default projects or new membership generations are created

#### Scenario: Exception - Real migration failure and missing schema
- **WHEN** failure is injected after realv5 schema/trigger creation before Commit, or API runs on schema4
- **THEN** failed migration leaves no projects/indexes/trigger function/appliedv5 record and retains old data, while authenticated project operations fail safe500 without auto-DDL

#### Scenario: Security - Native one-step down is isolated
- **WHEN** explicitly guarded disposablePG18 executes native down from5 to4
- **THEN** only project data/table/trigger/function are removed, old users/Sessions/Workspace/member/invitation data remain, and shared release/rollback procedures retain schema/data and use forward repair

#### Scenario: Concurrency - Migration ownership and old tests are preserved
- **WHEN** release or full normal/race integration suites run after5 is added
- **THEN** schema changes serialize per database, v4 tests explicitly test4 boundaries, intentional next-version failure moves to6, versions1–4 retain native migration coverage and hashes, and no unconfigured CLI migration lock is assumed

### Requirement: Real PostgreSQL project evidence and complete regression are mandatory
Project acceptance SHALL include unit tests, real guarded PostgreSQL18 integration, Race Detector, matching API documentation and existing feature regression. Missing/wrong/unreachable test DB, non18 server or missing pinned Goose SHALL fail rather than skip or fall back. Concurrency evidence SHALL use independent connections and controlled ordering. Planned cases SHALL NOT be reported as executed verification.

#### Scenario: Success - API and database acceptance agree
- **WHEN** full acceptance exercises registration/login, Workspace/invitation-backed joins, project creation/reads/conditional updates and schema constraints
- **THEN** exact OpenAPI/HTTP/DB outcomes and every added requirement map to actual tests, with local results distinguished from hosted CI

#### Scenario: Exception - Guarded dependencies and historical suites
- **WHEN** integration lacks required dependencies, points at the application DB, or a former auth/Workspace/invitation test regresses
- **THEN** the suite fails visibly, never skips or drops old checks, and ordinary integration/race schema suites do not overlap on one database

#### Scenario: Security - Required isolation and privacy matrix
- **WHEN** Owner/Member/outsider/multi-Workspace actors substitute IDs/cursors, retain stale Cookies after loss or submit sensitive sentinels
- **THEN** real tests assert exact401/403/404/412, zero unauthorized reads/writes, creator lifetime and no leaked logs/Set-Cookie/success metadata

#### Scenario: Concurrency - Required locking version and fault evidence
- **WHEN** tests order identity/member changes and Project writes in both directions, expire Sessions at each wait, race distinct Owners or inject real transaction outcome faults
- **THEN** database wait observation/barriers establish intended orders, no sleep-only or mock-only proof substitutes for PG18 behavior, and full integration-race plus regression of all six earlier changes remain required

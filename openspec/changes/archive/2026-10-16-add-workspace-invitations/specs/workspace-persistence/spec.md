# Spec Delta

## MODIFIED Requirements

### Requirement: Membership represents a constrained many-to-many relationship
Membership SHALL require workspace_id, user_id, role, database-created created_at and a unique non-null database-generated membership_id that SHALL remain immutable for that relation and never be reused by supported writers after deletion. The pair (workspace_id,user_id) SHALL be the primary key; role SHALL be only owner or member. Workspace deletion SHALL cascade memberships; user deletion SHALL be restricted while any membership references the user. An index starting with user_id then workspace_id SHALL support user-scoped enumeration.

#### Scenario: Success - Multiple memberships and roles
- **WHEN** a guarded fixture associates user A with workspaces X and Y, and user B with X
- **THEN** A has independent roles and membership_id values in X/Y, B shares X without owning Y, and no global user role determines either membership

#### Scenario: Exception - Referential or role violation
- **WHEN** direct SQL inserts an unknown user/workspace, null required value, duplicate pair or a role other than owner/member, null membership_id or duplicate membership_id
- **THEN** the named primary-key, foreign-key, not-null or role constraints reject the invalid relation

#### Scenario: Security - Deletion does not silently orphan ownership
- **WHEN** SQL deletes a user with any workspace membership or deletes an isolated test workspace
- **THEN** user deletion is rejected without changing its sessions/memberships, while workspace deletion removes only that workspace's members and preserves users and other workspaces

#### Scenario: Concurrency - Duplicate relationship race
- **WHEN** two isolated test transactions insert the same membership pair
- **THEN** at most one commits and the other cannot overwrite the existing role; a user may still have different workspace pairs

#### Scenario: Concurrency - Rejoin has a different generation
- **WHEN** a relationship is removed and the user joins again through a new accepted invitation
- **THEN** the new relation has a different membership_id while preserving composite-key uniqueness, so any stale conditional delete cannot match the new generation

### Requirement: Workspace and initial owner commit atomically
Creating a workspace SHALL commit exactly one workspace and one owner membership for the authenticated user in one transaction, or commit neither on a confirmed pre-commit failure. Clients SHALL NOT choose owner identity, role, UUID or timestamps. Success SHALL require confirmed commit. Other public member writes SHALL be limited to conditional deletion/exit in workspace-members and fixed-member invitation acceptance in workspace-invitations; direct add and role changes SHALL remain unavailable.

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

### Requirement: Member writes protect current identity and authority
Every member deletion or invitation mutation SHALL run in one bounded transaction that revalidates and protects active actor, unrevoked session, current actor authority and scoped resource/target conditions through completion. Expiry SHALL be checked against database statement time after all potentially blocking authorization locks. Earlier middleware or transaction reads SHALL NOT authorize a later write. Expiry after the final gate SHALL NOT require rollback.

#### Scenario: Success - Protected current actor and target
- **WHEN** the current Session/user and Workspace role permit the specific conditional deletion or invitation action after all protective waits finish
- **THEN** only the authorized relationship/invitation can change and conflicting supported identity/permission changes wait until transaction completion

#### Scenario: Exception - Authorization cannot be established
- **WHEN** a required row disappears, a lock/pool budget expires or a database check fails
- **THEN** stale entry identity is not used; current invalid identity is401, lost Workspace membership404, reduced-role forbidden action403 or scoped invitation404, and dependency/internal failure uses safe503/500 without deletion

#### Scenario: Security - Removal cannot escape its tenant or identity
- **WHEN** target UUID exists in several Workspaces or an attacker supplies another actor UUID
- **THEN** the write remains bound to the Cookie-derived actor and authorized Workspace plus target composite identity, never deleting by user_id alone or reading unscoped user existence

#### Scenario: Concurrency - Revoke disable role and expiry orderings
- **WHEN** revocation, disable, actor removal/role change or cleanup wins before the relevant protected check, or the member transaction wins first
- **THEN** the former is observed and denied as appropriate and the latter conflicting change waits; expiry during any lock wait is denied by the final fresh gate while expiry after that gate permits completion

#### Scenario: Concurrency - All mutation locks precede the final time boundary
- **WHEN** a supported invitation mutation waits on protected User, Session, Workspace, Membership or Invitation state
- **THEN** it rechecks live actor, Session, current resource authority and strict Session/invitation expiry in a fresh post-wait boundary before its permitted writes; acceptance does not require an already existing target membership

### Requirement: Member outcomes require confirmed commit without automatic replay
Member deletion and invitation mutations SHALL return their documented201/200/204 only after confirmed commit; failed delete conditions SHALL not become successful no-ops. Timeout, cancellation or connection loss SHALL NOT prove rollback or trigger automatic replay. A connected client SHALL receive a safe503/500 on unconfirmed outcomes; canceled clients SHALL receive no attempted response. A later explicit request SHALL use current authorization, not cached success, idempotency state or an old membership grant.

#### Scenario: Success - Explicit retry observes actual state
- **WHEN** deletion committed but its response was lost and the client makes a fresh permitted request
- **THEN** an Owner may observe absence/receive412 with the old condition, while a removed self-exiting caller receives404 with a still-valid identity, and no previous grant is reused

#### Scenario: Exception - Definite rollback and uncertain Commit differ
- **WHEN** deletion fails before commit and rollback is confirmed, or a submitted Commit acknowledgement is lost
- **THEN** the former leaves the relation intact, the latter may have removed it, neither returns success without confirmation, and neither is automatically retried or compensated by adding a member

#### Scenario: Security - Failure and cancellation cannot leak authority
- **WHEN** a member transaction fails with sensitive SQL/driver/session/user data or the client disconnects
- **THEN** only fixed safe errors/categories can leave the boundary, no further business/response work begins after cancellation and cleanup releases or discards the transaction connection within bounded resource handling

#### Scenario: Concurrency - Response loss does not restore membership
- **WHEN** one deletion commits with a lost response while other reads/deletions proceed
- **THEN** committed state controls subsequent authorization, no automatic retry can duplicate effects and no rollback assertion or background repair recreates access

#### Scenario: Concurrency - Accepted consent and join share an uncertain outcome
- **WHEN** actual PostgreSQL acceptance commits both rows but the driver acknowledgement is lost, or the real transaction is rolled back before a fault is returned
- **THEN** the former can persist both invitation accepted and one member, the latter persists neither transition, each submits Commit at most once and no false success or automatic compensation follows

### Requirement: Member lifecycle reuses the established schema
Existing membership keys, roles, foreign keys, timestamps and indexes SHALL remain intact. A separately reviewed additive Goose version4 SHALL add membership_id and workspace_invitations without editing migrations1/2/3. No owner counters, runtime DDL, idempotency tables or RLS claims SHALL be added. Startup/readiness SHALL retain their no-auto-migration/connectivity-only behavior.

#### Scenario: Success - Existing version-three schema remains sufficient
- **WHEN** a fresh schema reaches version4 or populated version3 upgrades through the new migration and repeats up
- **THEN** existing users/sessions/Workspaces/roles/created_at/keys remain intact, every old member receives one stable unique generation, repeat up preserves it and version4 is applied only by explicit migration

#### Scenario: Exception - Missing schema and future migration need
- **WHEN** a required table/column is missing or version4 fails midway
- **THEN** requests fail safely500 without auto-DDL, failed version4 rolls back its DDL/data/version together, and future fixes append a migration rather than editing an applied one

#### Scenario: Security - Existing deletion and role constraints survive
- **WHEN** guarded fixtures test unknown references, duplicate pairs, invalid roles, user deletion and Workspace cascade
- **THEN** the existing database constraints still reject invalid rows/restrict referenced user deletion and preserve tenant-scoped cascade semantics; no public Workspace deletion or role-grant endpoint is inferred

#### Scenario: Concurrency - Migration and data test isolation
- **WHEN** full PG18 regression and member transaction suites run
- **THEN** schema-mutating suites stay serialized in the guarded disposable database, membership concurrency uses independent connections in a stable schema, and migration1/2/3 byte hashes stay unchanged

#### Scenario: Security - One step down is disposable only
- **WHEN** an explicitly guarded disposable version4 database executes one-step down to3
- **THEN** invitation history and generation columns are removed while v3 member/user/session/Workspace data and constraints remain; shared releases never use down/reset as rollback, and old unconditional-delete binaries are not reopened after rejoining

## ADDED Requirements

### Requirement: Invitations have scoped referential and pending uniqueness constraints
Invitations SHALL have generated UUID id, immutable workspace_id,invitee_user_id,created_by_user_id, allowed status and non-null created_at,updated_at,expires_at. Workspace deletion SHALL cascade; referenced user deletion SHALL restrict. One stored pending per Workspace/recipient SHALL be enforced independently of current time, allowing multiple terminal history rows.

#### Scenario: Success - Scoped history and bounded indexes
- **WHEN** migrations create the invitation model and supported creation stores pending then terminal records
- **THEN** the table supports Workspace/id and recipient/id lists, pending queries and Workspace expiry counts; creator and recipient differ, timestamps are ordered, and expiry is enforced at runtime without a time-dependent partial-index predicate

#### Scenario: Exception - Invalid direct persistence
- **WHEN** guarded SQL supplies missing references, null required values, self invitation, unknown status, expiry not after creation, updated_at outside creation/expiry, or a second stored pending pair
- **THEN** named FK/not-null/CHECK/unique constraints reject the write without corrupting existing rows, while terminal history can coexist with a later pending

#### Scenario: Security - History does not regain authority
- **WHEN** the accepted membership is removed or the creator loses its role/account availability
- **THEN** accepted history is not cascaded through a membership FK or reopened, creator loss alone does not invalidate pending, and supported operations never repurpose resource UUIDs as bearer credentials

#### Scenario: Concurrency - Expired pending replacement is atomic
- **WHEN** two creates encounter an expired stored pending for the same Workspace/recipient
- **THEN** supported writes atomically expire the old row and create at most one new pending under Workspace protection, and failure leaves no half replacement or duplicate pending

### Requirement: Invitation acceptance protects consent and relation generation atomically
Acceptance SHALL revalidate live recipient identity, bound invitation, current Workspace integrity, pending status, strict expiry and absent membership in one bounded transaction. The accepted transition and new fixed-member relationship with fresh membership_id SHALL commit atomically. Protected supported writers SHALL share a consistent deadlock-avoiding order and SHALL never overwrite an existing relation.

#### Scenario: Success - Protected first membership
- **WHEN** all current protected conditions allow acceptance
- **THEN** accepted and one new member publish together only after confirmed commit, current recipient account/Session changes cannot overtake protection, and no client role or inviter Session is used as recipient consent

#### Scenario: Exception - Insert failure existing relation or absent scope
- **WHEN** relation insertion fails, an existing relation is found, identity/expiry becomes invalid before the boundary, or invitation/Workspace scope is unavailable
- **THEN** no partial accepted state persists, existing relation role/id/time is never overwritten and the documented safe failure is returned without automatic replay

#### Scenario: Security - Tenant binding and owner integrity
- **WHEN** a foreign recipient/Workspace is substituted or a surviving Workspace has no Owner due to administrative corruption
- **THEN** invitation binding denies foreign access404, ownerless writes fail safe500, and neither SQL role injection nor a known UUID bypasses tenant/identity checks

#### Scenario: Concurrency - Consistent protection includes missing memberships
- **WHEN** accept competes with conditional delete/rejoin, decline/revoke, identity invalidation or supported maintenance role changes
- **THEN** all use coordinated User→Session→Workspace→Membership→Invitation protection where applicable, same-class rows use deterministic UUID order, the Workspace protects absent relationships, and opposite ordered tests verify documented results without deadlocks

#### Scenario: Concurrency - Recipient disable and Session revoke orderings
- **WHEN** recipient disable/revocation wins before protected checks or acceptance holds its protective locks first
- **THEN** the former denies401 with no join, while the latter can commit before the invalidating change; fresh later calls use current identity and no previously accepted grant is replayed

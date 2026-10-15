# Spec Delta

## MODIFIED Requirements

### Requirement: Workspace and initial owner commit atomically
Creating a workspace SHALL commit exactly one workspace and one owner membership for the authenticated user in one transaction, or commit neither on a confirmed pre-commit failure. Clients SHALL NOT choose owner identity, role, UUID or timestamps. Success SHALL require confirmed commit. Additional public membership writes SHALL be limited to the deletion/exit contract in workspace-members; adding members or changing roles SHALL remain unavailable.

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

## ADDED Requirements

### Requirement: Member writes protect current identity and authority
Every member deletion or authorized no-op SHALL run in one bounded transaction that revalidates and protects active actor, unrevoked session, current actor role and scoped target conditions through completion. Expiry SHALL be checked against database statement time after all potentially blocking authorization locks. Earlier middleware or transaction reads SHALL NOT authorize a later write. Expiry after the final gate SHALL NOT require rollback.

#### Scenario: Success - Protected current actor and target
- **WHEN** the current Session/user and Workspace role permit a deletion after all protective waits finish
- **THEN** only the authorized relationship can change and conflicting supported identity/permission changes wait until transaction completion

#### Scenario: Exception - Authorization cannot be established
- **WHEN** a required row disappears, a lock/pool budget expires or a database check fails
- **THEN** stale entry identity is not used; current invalid identity is401, lost Workspace membership404, reduced-role forbidden action403, and dependency/internal failure uses safe503/500 without deletion

#### Scenario: Security - Removal cannot escape its tenant or identity
- **WHEN** target UUID exists in several Workspaces or an attacker supplies another actor UUID
- **THEN** the write remains bound to the Cookie-derived actor and authorized Workspace plus target composite identity, never deleting by user_id alone or reading unscoped user existence

#### Scenario: Concurrency - Revoke disable role and expiry orderings
- **WHEN** revocation, disable, actor removal/role change or cleanup wins before the relevant protected check, or the member transaction wins first
- **THEN** the former is observed and denied as appropriate and the latter conflicting change waits; expiry during any lock wait is denied by the final fresh gate while expiry after that gate permits completion

### Requirement: Supported member writes preserve an owner relationship
All supported member mutations for one Workspace SHALL be serialized and SHALL preserve at least one owner relationship for that surviving Workspace. Owner retention SHALL be evaluated using current protected state after waiting, never an earlier unprotected count. The guarantee SHALL cover coordinated application/maintenance writers, not arbitrary SQL, privileged schema changes or active-account availability.

#### Scenario: Success - Retained owner is stored atomically
- **WHEN** an ordinary Member is removed or one of several Owners exits under the supported protocol
- **THEN** readers observe committed membership changes atomically and a surviving Workspace retains at least one role=owner relationship

#### Scenario: Exception - Would remove final owner or invalid initial state
- **WHEN** an Owner self-exit would leave zero Owners, or an operation on administratively corrupted ownerless state cannot establish the invariant
- **THEN** final-owner exit is409 without write; corrupted state never becomes an authorization shortcut or silently repaired ownership and is treated as a safe internal integrity fault if encountered by a write

#### Scenario: Security - Direct SQL limits are accurately stated
- **WHEN** database guarantees or a maintenance membership change are reviewed
- **THEN** ordinary PK/FK/role CHECK are not represented as last-owner enforcement, supported maintenance follows the same serialization/count protocol, and no unreviewed production SQL joining/role-grant workflow is offered

#### Scenario: Concurrency - Two-owner write skew is prevented
- **WHEN** two Owner self-exits start with two Owners, in either acquisition order
- **THEN** only one commits removal, the other observes one Owner and is409, or failures leave more Owners; independent tenants do not require a global Workspace write lock

### Requirement: Member outcomes require confirmed commit without automatic replay
Member mutations SHALL return204 only after confirmed commit, including authorized no-op deletion. Timeout, cancellation or connection loss SHALL NOT prove rollback or trigger automatic replay. A connected client SHALL receive a safe503/500 on unconfirmed outcomes; canceled clients SHALL receive no attempted response. A later explicit request SHALL use current authorization, not cached success, idempotency state or an old membership grant.

#### Scenario: Success - Explicit retry observes actual state
- **WHEN** deletion committed but its response was lost and the client makes a fresh permitted request
- **THEN** an Owner may observe absence/repeat204, while a removed self-exiting caller receives404 with a still-valid identity, and no previous grant is reused

#### Scenario: Exception - Definite rollback and uncertain Commit differ
- **WHEN** deletion fails before commit and rollback is confirmed, or a submitted Commit acknowledgement is lost
- **THEN** the former leaves the relation intact, the latter may have removed it, neither returns204 without confirmation, and neither is automatically retried or compensated by adding a member

#### Scenario: Security - Failure and cancellation cannot leak authority
- **WHEN** a member transaction fails with sensitive SQL/driver/session/user data or the client disconnects
- **THEN** only fixed safe errors/categories can leave the boundary, no further business/response work begins after cancellation and cleanup releases or discards the transaction connection within bounded resource handling

#### Scenario: Concurrency - Response loss does not restore membership
- **WHEN** one deletion commits with a lost response while other reads/deletions proceed
- **THEN** committed state controls subsequent authorization, no automatic retry can duplicate effects and no rollback assertion or background repair recreates access

### Requirement: Member lifecycle reuses the established schema
This member-list/removal/exit delivery SHALL reuse existing membership keys, role constraints, foreign keys, timestamps and indexes without new migration or changes to migrations1/2/3. It SHALL NOT add owner counters, invitation/consent records, idempotency tables, runtime DDL or RLS claims. Database-enforced owner retention or joining persistence SHALL require a separately reviewed additive migration plan.

#### Scenario: Success - Existing version-three schema remains sufficient
- **WHEN** a fresh or populated version3 schema runs the new APIs and repeated existing migration up
- **THEN** existing rows/constraints/indexes remain compatible, roster ordering and scoped deletion use existing keys, and no schema version advance or startup migration occurs

#### Scenario: Exception - Missing schema and future migration need
- **WHEN** a required existing table is missing or reviewers require stronger database owner enforcement
- **THEN** requests fail safely500 without auto-DDL, and stronger enforcement is planned as a new additive version rather than editing an applied migration

#### Scenario: Security - Existing deletion and role constraints survive
- **WHEN** guarded fixtures test unknown references, duplicate pairs, invalid roles, user deletion and Workspace cascade
- **THEN** the existing database constraints still reject invalid rows/restrict referenced user deletion and preserve tenant-scoped cascade semantics; no public Workspace deletion or role-grant endpoint is inferred

#### Scenario: Concurrency - Migration and data test isolation
- **WHEN** full PG18 regression and member transaction suites run
- **THEN** schema-mutating suites stay serialized in the guarded disposable database, membership concurrency uses independent connections in a stable schema, and migration1/2/3 byte hashes stay unchanged

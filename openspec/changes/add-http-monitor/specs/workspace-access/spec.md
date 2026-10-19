# Workspace Access Delta

## MODIFIED Requirements

### Requirement: Tenant boundaries and acceptance remain verifiable
The repository SHALL verify workspace behavior with unit tests, mandatory real PostgreSQL 18 integration/race tests and matching API documentation. Each tenant resource query SHALL use trusted identity plus membership and resource ownership. Future tenant-owned rows SHALL require workspace_id and tenant-consistent relationships. Public membership and invitation APIs SHALL follow workspace-members and workspace-invitations, including narrowly recipient-scoped consent preview. Only the reviewed invitation, project and HTTP Monitor configuration schema additions SHALL be introduced; no unrelated business tables or RLS enforcement SHALL be claimed.

#### Scenario: Success - End-to-end contract and many-to-many fixtures
- **WHEN** guarded PG18 tests register/login users, create workspaces and exercise invitation-backed member joins and roster/conditional removal/exit, using multi-owner or role-change SQL only as guarded fixtures
- **THEN** API/DB contracts, multi-workspace lists, per-workspace roles and atomic creation are verified; real consent-backed joining is verified, fixture membership insertion is never presented as consent and no direct POST member endpoint is exposed

#### Scenario: Exception - Required test dependencies absent
- **WHEN** explicit integration runs lack the guarded URL, PG18, reachable DB or pinned Goose
- **THEN** they fail rather than skip or fall back to the application database; schema suites remain serialized

#### Scenario: Security - Required tenant isolation matrix
- **WHEN** tests substitute other-tenant path IDs/cursors, forge identities/roles, or exercise outsider/missing/disabled/revoked states
- **THEN** exact 401/403/404 behavior, zero unauthorized data/writes and safe errors/logs are asserted against real tenant fixtures, without timing-based claims of complete existence secrecy

#### Scenario: Concurrency - Deterministic authorization evidence
- **WHEN** controlled connections/barriers order create/revoke/disable, reads/member removal/role change and duplicate writes
- **THEN** both documented orderings, post-wait expiry, no automatic retries and database constraints are verified without treating sleep-based timing or planned tests as completed evidence

#### Scenario: Security - Immediate tenant access loss
- **WHEN** removal or self-exit has committed and the removed user makes fresh roster, detail, workspace-list or deletion requests with its old valid Cookie
- **THEN** it receives404 for that Workspace's protected endpoints, the Workspace is absent from its list, other tenants are unchanged and auth/me may still succeed

#### Scenario: Concurrency - Membership verification is required evidence
- **WHEN** real PG18 tests order owner exits, remove/exit races, role changes, revocation/disable/cleanup and lock-wait expiry with controlled connections
- **THEN** both documented orderings and all resource/identity gates are verified; missing dependencies fail and real invite acceptance/rejoin, old DELETE generation protection, acceptance/revocation/decline and quota races are verified in both orderings rather than replaced by fixture-only inserts

#### Scenario: Concurrency - Required deterministic delayed request evidence
- **WHEN** a DELETE request begins before removal/rejoin but is paused before obtaining its Workspace lock
- **THEN** PostgreSQL18 tests demonstrate the old composite-key-only protocol's failure and the conditional protocol's rejection of M1 after M2 joins; a distinct test proves that already holding the Workspace lock blocks rejoin

#### Scenario: Exception - CI and legacy fixtures follow the reviewed migration
- **WHEN** full normal and race integration suites run after version4 is added
- **THEN** they preserve explicit version3 migration coverage, move the intentionally failed next-version fixture beyond version4, update precise roster/DELETE contracts, fail on missing guarded dependencies, and serialize schema-changing suites rather than masking failures or skipping old APIs

#### Scenario: Success - Projects extend the reviewed tenant foundation
- **WHEN** the reviewed project-management and project-persistence capabilities add Project creation, reading and conditional updating
- **THEN** only the four scoped Project operations and Migration5 are introduced, while existing Workspace/member/invitation DTOs, methods, roles and Cookie behavior remain unchanged

#### Scenario: Exception - Project scope does not silently add future resources
- **WHEN** a request or implementation attempts Monitor, Incident, Alert, project transfer, archive or deletion in this Change
- **THEN** those operations remain unavailable and no unrelated schema or project-role table is introduced

#### Scenario: Security - Project identifiers require current tenant authority
- **WHEN** users substitute a known foreign Project UUID, combine an authorized Workspace with a foreign Project, or retain a Cookie after membership loss
- **THEN** fresh Project reads/writes use live identity, membership and resource ownership together and deny unauthorized data/changes under the project contracts

#### Scenario: Concurrency - Project protection composes with existing member operations
- **WHEN** Project create/PATCH races with invitation acceptance, conditional removal, self-exit, revocation or coordinated role changes
- **THEN** the existing User→Session→Workspace→Membership order remains intact, Project protection follows it and deterministic PG18/race tests cover both orderings without weakening old member-generation protection


#### Scenario: Success - Reviewed HTTP Monitor configuration extension
- **WHEN** the eighth Change adds reviewed http-monitor-management and http-monitor-persistence behavior
- **THEN** only the four Project-scoped configuration operations and Migration6 are added; no target DNS/HTTP/connection, worker, result, alert, independent role or mutation of existing API contracts occurs; historical Project-only scope scenarios retain their seventh-Change test boundary

#### Scenario: Security - Monitor authority inherits the complete tenant chain
- **WHEN** a user substitutes Workspace/Project/Monitor UUIDs or loses current identity or membership
- **THEN** actual Monitor SQL gates active Cookie identity, current membership and matching complete ownership with no id-only access or relaxed existing input/CSRF protocols

#### Scenario: Concurrency - Monitor protection and historical acceptance compose
- **WHEN** Monitor writes race with Project/member/invitation/session/account changes and full normal/race suites run at schema6
- **THEN** compatible User→Session→Workspace→Membership→Project→Monitor protection and post-wait time checks are verified on real guarded PG18; historical version1–5 boundaries persist and the current next-version failure fixture moves from6 to7

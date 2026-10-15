# Spec Delta

## MODIFIED Requirements

### Requirement: Workspace success responses have a minimal projection
Confirmed creation SHALL return 201 with Location: /api/v1/workspaces/{id}. Detail SHALL return 200. Creation and detail SHALL use data containing only id,name,role,created_at,updated_at, with role specific to the requesting membership and UTC RFC3339 timestamps. These responses SHALL NOT include other users, owner identity, email, credentials or sessions. Workspace endpoints SHALL NOT issue, clear or renew Cookies.

#### Scenario: Success - Owner and member projections
- **WHEN** creation commits or owner/member reads a permitted detail
- **THEN** the exact projection matches stored workspace state and the caller's role; only creation returns Location and its role is owner

#### Scenario: Exception - Unconfirmed creation
- **WHEN** persistence fails or its commit outcome is unknown
- **THEN** no 201 or success Location is returned, and the error does not assert that creation was rolled back

#### Scenario: Security - No expanded identity projection
- **WHEN** existing Workspace creation/list/detail responses and headers are captured
- **THEN** no owner/member roster, other-user identifier, email, token, digest, password/PHC or Set-Cookie appears

#### Scenario: Concurrency - Reads cannot overwrite login state
- **WHEN** a workspace response overlaps a new login
- **THEN** the workspace response sends no Set-Cookie and cannot delete or replace the new browser credential

#### Scenario: Success - Separate member roster projection
- **WHEN** the separate member roster endpoint returns an authorized list
- **THEN** its user_id/role/joined_at projection is governed by workspace-members without expanding the existing Workspace DTO or changing Cookie behavior

### Requirement: Workspace mutations reuse the same-origin CSRF protocol
Supported Workspace POST and member DELETE SHALL require exactly one valid non-null Origin matching AUTH_ORIGIN and exactly one X-DevPilot-CSRF: 1 before identity/database work. Present Sec-Fetch-Site SHALL be a single same-origin value. Rejection SHALL be 403 csrf_rejected with no writes or Cookie effects. GET SHALL remain read-only and need no CSRF header. No CORS grant or Referer/Host fallback SHALL be added.

#### Scenario: Success - Same-origin client and CLI
- **WHEN** a browser or CLI sends valid proof and its session Cookie for a valid create or member delete
- **THEN** it may proceed; list/detail use Cookie without requiring the custom header or changing state

#### Scenario: Exception - Missing or ambiguous proof
- **WHEN** Origin/proof is missing, null, duplicated, malformed or a mismatching tuple, or Fetch Metadata is present with anything except one same-origin
- **THEN** POST/DELETE return fixed 403 before session lookup, transaction or Cookie changes

#### Scenario: Security - Spoofed host and cross-origin preflight
- **WHEN** hostile requests use a suffix/subdomain/scheme/port mismatch, spoof Host/Forwarded, provide only Referer or issue OPTIONS
- **THEN** proof does not pass and OPTIONS returns 405 without CORS permissions

#### Scenario: Concurrency - Proof cannot cross requests
- **WHEN** valid and forged POST/DELETE requests execute concurrently
- **THEN** each checks its own immutable policy/proof and the forged request performs no database work

#### Scenario: Security - Deletion proof rejects before database work
- **WHEN** a member deletion has missing/null/duplicate/mismatching Origin or custom proof, invalid Fetch Metadata or Referer/Host-only evidence
- **THEN** fixed403 occurs before identity lookup, transaction, deletion or Cookie effects, including self-exit and a target that is already absent

### Requirement: Workspace responses and processing fail safely
Workspace endpoints SHALL use JSON data/error except empty204, no-store, nosniff and fixed safe messages. DB unavailability SHALL be 503 service_unavailable, confirmed timeout 503 database_timeout and schema/unexpected faults 500 internal_error. Requests SHALL have a 5s processing budget and each read/whole mutation transaction at most 2s within it. Logs SHALL omit secrets, raw SQL/errors, headers, bodies, names and member IDs. Cancellation SHALL stop further work and attempted responses.

#### Scenario: Success - Documented headers and recovery
- **WHEN** a workspace request succeeds, or dependencies recover after failure
- **THEN** the exact documented safe response is returned and later requests can succeed without restart or global failure state

#### Scenario: Exception - Error classes methods and panic
- **WHEN** a connection fails, a confirmed deadline expires, a table is missing, an unexplained cancellation/internal defect occurs, or a dependency panics
- **THEN** safe 503/503/500/500 responses result if writable; pre-write panic is fixed 500 and post-write panic aborts the connection without exposing its value

#### Scenario: Security - Sensitive diagnostic sentinels
- **WHEN** failures or successes include recognizable token, digest, PHC, DSN, SQL, email or name sentinels internally
- **THEN** logs disclose none, and response data contains only the permitted name/fields with errors never echoing supplied values

#### Scenario: Concurrency - Bounded waits and method isolation
- **WHEN** lock/pool waits or simultaneous failing/successful requests occur
- **THEN** deadlines bound DB waiting, requests do not share mutable identity/result state or acquire Argon slots; wrong methods return 405 with Allow: GET, POST on the workspace collection, GET on workspace detail/member collection or DELETE on member item and HEAD has no body

#### Scenario: Success - Confirmed empty member deletion response
- **WHEN** an authorized member deletion or no-op decision commits
- **THEN** empty204 has no JSON body, JSON Content-Type, Location or Set-Cookie, while no-store/nosniff remain present

#### Scenario: Exception - Member mutation failure categories
- **WHEN** a member transaction suffers unknown Commit, pool/lock deadline, cancellation, missing schema, deadlock or unexplained database fault
- **THEN** no unconfirmed204 is issued, connected clients receive the safe503/500 category, canceled clients receive no attempted response and no automatic business retry occurs

### Requirement: Tenant boundaries and acceptance remain verifiable
The repository SHALL verify workspace behavior with unit tests, mandatory real PostgreSQL 18 integration/race tests and matching API documentation. Each tenant resource query SHALL use trusted identity plus membership and resource ownership. Future tenant-owned rows SHALL require workspace_id and tenant-consistent relationships. Public member APIs SHALL be limited to workspace-members. No future business tables or RLS enforcement SHALL be introduced.

#### Scenario: Success - End-to-end contract and many-to-many fixtures
- **WHEN** guarded PG18 tests register/login users, create workspaces and seed extra member/owner relations only in fixtures and exercise roster/removal/exit APIs
- **THEN** API/DB contracts, multi-workspace lists, per-workspace roles and atomic creation are verified; fixture membership insertion is not presented as a shipped join API and no POST member endpoint is exposed

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
- **THEN** both documented orderings and all resource/identity gates are verified; missing dependencies fail and fixture-only add/rejoin cases are explicitly deferred rather than reported as product success

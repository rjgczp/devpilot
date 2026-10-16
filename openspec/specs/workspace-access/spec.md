# workspace-access Specification

## Purpose

定义 DevPilot AI 第一版 Workspace API 的输入、响应、成员读取权限和租户隔离规则，并明确 Cookie Session、同源 CSRF、安全错误与并发权限变化的验收边界，防止客户端替换资源标识造成越权。

## Requirements

### Requirement: Workspace creation accepts only a bounded name
POST /api/v1/workspaces SHALL accept exactly one JSON object containing only a string name, with a 4096-byte body limit, application/json with optional UTF-8 charset and no Content-Encoding. Malformed UTF-8, invalid raw Unicode escapes, duplicate/unknown/missing fields and multiple values SHALL be rejected. Structural, size and representation failures SHALL be 400 invalid_request, 413 request_too_large and 415 unsupported_media_type respectively.

#### Scenario: Success - Strict name input
- **WHEN** a CSRF-valid authenticated request supplies exactly one valid name string in the supported representation
- **THEN** it can proceed to name validation and atomic creation without accepting account or role fields

#### Scenario: Exception - Representation and parser boundaries
- **WHEN** JSON is null, an array, empty, multiple values, has duplicate/unknown/missing/non-string fields, invalid UTF-8 or unpaired/malformed raw surrogate escapes, or representation/body bounds fail
- **THEN** it receives the documented 400/413/415 without workspace persistence; chunked data and trailing whitespace count toward the 4096-byte limit

#### Scenario: Security - Identity and role injection
- **WHEN** a request injects user_id, owner_id, workspace_id, role, id or timestamps
- **THEN** 400 invalid_request is returned, no values are echoed, and no supplied field can establish the creator or privilege

#### Scenario: Concurrency - Parser state isolation
- **WHEN** valid and malicious requests overlap
- **THEN** each request's name, identity and errors remain independent and rejected input cannot influence another transaction

### Requirement: Display names have a deterministic Unicode policy
Creation SHALL trim only surrounding ASCII SP/HT/CR/LF, then require 1–100 Unicode code points and at most 400 UTF-8 bytes. Remaining leading/trailing Unicode whitespace and any Unicode Cc control SHALL be rejected. Interior ordinary spaces, case and valid Unicode SHALL be preserved without normalization. Violations SHALL return 422 validation_failed with name and fixed required/invalid_length/invalid_format reasons.

#### Scenario: Success - Display names preserve meaning
- **WHEN** a valid name contains Chinese, supplementary scalars, interior spaces or surrounding ASCII whitespace
- **THEN** only the specified edge characters are removed, the stored/returned value is otherwise unchanged, and identical names can belong to independent workspaces

#### Scenario: Exception - Name boundaries
- **WHEN** trimming leaves no characters, the result exceeds 100 code points/400 bytes, contains Cc controls or has remaining Unicode edge whitespace
- **THEN** 422 reports name with required for empty, otherwise invalid_length before invalid_format, without persistence or submitted values

#### Scenario: Security - SQL and markup are values
- **WHEN** an otherwise valid name contains quotes, SQL-like text or markup
- **THEN** it is stored only as a parameterized string and returned as JSON data, never executed or treated as a trusted identifier or HTML

#### Scenario: Concurrency - Names do not serialize unrelated tenants
- **WHEN** several creators concurrently use case variants or Unicode-equivalent names
- **THEN** no global name lock, normalization merge or uniqueness rule combines their independent workspaces

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
- **THEN** its membership_id/user_id/role/joined_at projection is governed by workspace-members without expanding the existing Workspace DTO or changing Cookie behavior

#### Scenario: Security - Consent preview does not expand Workspace access
- **WHEN** a nonmember reads its own invitation inbox
- **THEN** only the separate workspace-invitations projection may disclose the invited Workspace name/id, existing Workspace detail/list/roster DTOs are not expanded, and the invitation is not itself membership authority

### Requirement: Workspace lists are bounded and user scoped
GET /api/v1/workspaces SHALL return only the caller's memberships, ordered by workspace UUID ascending. It SHALL accept only optional single limit (canonical decimal 1–100, default 50) and after (canonical lowercase hyphenated UUID), fetching at most limit+1 candidates. Success SHALL use data as an array of the detail projection and pagination.next_after as the last returned UUID only when another row exists, otherwise null.

#### Scenario: Success - Empty multiple and paged memberships
- **WHEN** an authenticated user has zero, one or several memberships and follows returned cursors
- **THEN** it receives 200 with []/null for empty results, or bounded pages of only its workspaces and roles; pages over a stable dataset neither duplicate nor omit records

#### Scenario: Exception - Invalid list selectors
- **WHEN** limit/after is repeated, unknown keys occur, limit is zero, over 100, signed, padded or non-decimal, after is malformed, or query percent-encoding is invalid
- **THEN** 400 invalid_request is returned without unbounded querying or reflection of submitted values

#### Scenario: Security - Cursor is never authority
- **WHEN** after contains another tenant's UUID or a valid nonexistent UUID
- **THEN** it is only an ordering boundary, causes no resource existence check, and cannot expose another tenant's rows, role, count or metadata

#### Scenario: Concurrency - Pagination uses fresh snapshots
- **WHEN** membership/creation changes between page requests
- **THEN** each page applies current identity and membership checks, with no whole-traversal snapshot/total-count guarantee; removed memberships are not authorized by an older cursor

### Requirement: Workspace detail is authorized without existence disclosure
GET /api/v1/workspaces/{workspace_id} SHALL require membership for the trusted authenticated user. Both owner and member SHALL read their workspace. An authenticated non-member, unknown workspace or malformed/noncanonical UUID SHALL receive identical 404 workspace_not_found with message Workspace not found. The endpoint SHALL NOT query unscoped existence to distinguish those cases or accept query selectors.

#### Scenario: Success - Same workspace different roles
- **WHEN** owner and member independently request their shared workspace
- **THEN** both receive its same basic fields and only their own respective role

#### Scenario: Exception - Unknown malformed or inaccessible resource
- **WHEN** a valid identity requests an unknown UUID, malformed/noncanonical UUID or a workspace without its membership
- **THEN** the same 404 code/message/envelope/headers are returned without an owner or existence hint; unsupported query parameters return 400 invalid_request

#### Scenario: Security - Cross-tenant ID replacement
- **WHEN** user A belongs only to X, user B belongs only to Y, and A replaces X's path ID with Y's
- **THEN** A receives the same 404 as an unknown resource, learns no Y metadata and cannot change any membership by reading

#### Scenario: Concurrency - Membership changes around a read
- **WHEN** membership removal or role change commits before the authorized read snapshot, or after that snapshot
- **THEN** the former uses the changed membership and denies removal with 404, while an earlier authorized snapshot may finish; later fresh reads cannot reuse cached membership

### Requirement: Workspace reads use live identity and tenant checks together
Workspace reads SHALL validate the selected environment Cookie using the existing canonical-secret protocol and database predicates for active user, unrevoked session and expiry strictly after statement time. Resource reads SHALL evaluate identity and membership in one statement snapshot. Invalid identity SHALL return 401 unauthenticated / Authentication required., distinct from authenticated missing membership. No middleware identity or membership cache SHALL bypass this decision.

#### Scenario: Success - Active unverified identity is sufficient for this feature
- **WHEN** an active user with unverified email has a current valid session and membership
- **THEN** workspace access is allowed by that membership without claiming verified email ownership or extending session lifetime

#### Scenario: Exception - Missing invalid or expired Cookie
- **WHEN** a well-formed endpoint request has missing, duplicate, whitespace-altered, malformed, unknown, expired, revoked or disabled-user Cookie
- **THEN** identical 401 is returned without data or Set-Cookie; query/JSON/Authorization tokens and client user_id cannot replace Cookie authentication

#### Scenario: Security - No fallback on database faults
- **WHEN** live identity/tenant validation cannot complete because of database failure
- **THEN** a safe 503/500 is returned rather than 401/404 or access based on stale identity

#### Scenario: Concurrency - Read snapshot includes revocation and account state
- **WHEN** logout/disable commits before a resource-read snapshot or while a previously authorized snapshot completes
- **THEN** the former is 401 and the latter may finish; a valid entry check does not override changed state at the actual resource-read snapshot

### Requirement: Workspace mutations reuse the same-origin CSRF protocol
Supported Workspace POST, member DELETE and invitation create/accept/decline/revoke mutations SHALL require exactly one valid non-null Origin matching AUTH_ORIGIN and exactly one X-DevPilot-CSRF: 1 before identity/database work. Present Sec-Fetch-Site SHALL be a single same-origin value. Rejection SHALL be 403 csrf_rejected with no writes or Cookie effects. GET SHALL remain read-only and need no CSRF header. No CORS grant or Referer/Host fallback SHALL be added.

#### Scenario: Success - Same-origin client and CLI
- **WHEN** a browser or CLI sends valid proof and its session Cookie for a valid Workspace/member/invitation mutation
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
Workspace and invitation endpoints SHALL use JSON data/error except empty204, no-store, nosniff and fixed safe messages. DB unavailability SHALL be 503 service_unavailable, confirmed timeout 503 database_timeout and schema/unexpected faults 500 internal_error. Requests SHALL have a 5s processing budget and each read/whole mutation transaction at most 2s within it. Logs SHALL omit secrets, raw SQL/errors, headers, bodies, names and user/member/invitation IDs. Cancellation SHALL stop further work and attempted responses.

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
- **WHEN** an authorized conditional member deletion with a matching current validator commits
- **THEN** empty204 has no JSON body, JSON Content-Type, Location or Set-Cookie, while no-store/nosniff remain present

#### Scenario: Exception - Member mutation failure categories
- **WHEN** a member transaction suffers unknown Commit, pool/lock deadline, cancellation, missing schema, deadlock or unexplained database fault
- **THEN** no unconfirmed204 is issued, connected clients receive the safe503/500 category, canceled clients receive no attempted response and no automatic business retry occurs

#### Scenario: Exception - Invitation methods and parsing remain bounded
- **WHEN** invitation namespaces receive unsupported methods, ambiguous encoded/cleaned paths, malformed create JSON or a nonempty/slow accept/decline/revoke body
- **THEN** exact endpoint Allow headers follow workspace-invitations, mutation aliases never redirect into writes, bounded parser/body deadlines apply, HEAD has no body and OPTIONS grants no CORS

### Requirement: Tenant boundaries and acceptance remain verifiable
The repository SHALL verify workspace behavior with unit tests, mandatory real PostgreSQL 18 integration/race tests and matching API documentation. Each tenant resource query SHALL use trusted identity plus membership and resource ownership. Future tenant-owned rows SHALL require workspace_id and tenant-consistent relationships. Public membership and invitation APIs SHALL follow workspace-members and workspace-invitations, including narrowly recipient-scoped consent preview. Only the reviewed invitation and project schema additions SHALL be introduced; no unrelated business tables or RLS enforcement SHALL be claimed.

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

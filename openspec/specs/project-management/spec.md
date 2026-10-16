# project-management Specification

## Purpose

定义Workspace下Project管理的可观察API、成员继承权限、租户隔离及并发编辑契约，为未来监控资源建立稳定归属；同时限制输入、响应和生命周期，防止已知UUID被用来绕过当前成员授权。

## Requirements

### Requirement: Projects inherit current workspace permissions without an independent lifecycle
Projects SHALL belong to one immutable Workspace. Current Owners SHALL create and modify projects; current Owners and Members SHALL read them. Creator identity SHALL NOT grant access. No project roles, transfer, status, delete, archive or restore operations SHALL exist. Same-Workspace names SHALL allow duplicates and SHALL NOT be slugs or idempotency keys.

#### Scenario: Success - Workspace authority and independent project lifetime
- **WHEN** an Owner creates two same-name projects and a current Member reads them
- **THEN** both projects have different database-generated identities and are readable; no project membership must be created

#### Scenario: Exception - Unsupported operations
- **WHEN** a canonical collection/item receives DELETE, PUT, HEAD, OPTIONS or an unsupported action
- **THEN** canonical collection/item methods return405 with Allow GET, POST or GET, PATCH respectively, HEAD has no body and no lifecycle state changes; unsupported action paths return404

#### Scenario: Security - Current members and creator are distinct
- **WHEN** a creator exits, is removed or disabled while another Owner remains, or an ordinary Member tries to write
- **THEN** the project survives, other current members retain their permitted access, the former creator cannot retain authority and Member writes to readable scope return403 project_forbidden

#### Scenario: Concurrency - Duplicate names and lost creation responses
- **WHEN** valid creates overlap or a client explicitly repeats a create after response loss
- **THEN** distinct same-name projects can commit, no name-based deduplication occurs and the service makes no exactly-once promise

### Requirement: Project mutation representations are strictly bounded
POST SHALL require name and optionally description; PATCH SHALL accept at least one of those fields. Supplied fields SHALL be strings in a single JSON object. Unknown/duplicate keys, null, invalid UTF-8 or surrogate escapes, and extra JSON SHALL be400 invalid_request. Bodies SHALL be at most16384 raw bytes. Representation SHALL be single application/json with optional UTF-8 charset and no Content-Encoding.

#### Scenario: Success - Partial update and empty description
- **WHEN** POST omits description or PATCH supplies only description as an empty string
- **THEN** creation stores description as empty, PATCH clears it while retaining name, and missing PATCH fields remain unchanged

#### Scenario: Exception - Structural media and byte boundaries
- **WHEN** a request is an array/null, lacks POST name, is an empty PATCH, contains non-string/duplicate/unknown keys or trailing values, exceeds16384 bytes, or violates the media contract
- **THEN** it returns400,413 request_too_large or415 unsupported_media_type respectively, with no persistence; escaped strings, chunked bytes and trailing whitespace count toward the raw limit

#### Scenario: Security - Ownership and version injection
- **WHEN** JSON supplies id,workspace_id,user_id,created_by_user_id,role,status,version or timestamps, including escaped duplicate keys or malformed Unicode
- **THEN** it returns400 without accepting actor/tenant/version claims or echoing the values, and alternate credentials in JSON do not authenticate

#### Scenario: Concurrency - Request parsing and slow input remain bounded
- **WHEN** valid and malformed mutations overlap, or a chunked body withholds data/end-of-body
- **THEN** parser state cannot cross requests, body reading and early-rejection draining remain within the5s processing budget, cancellation stops work and no slow-input path begins an unauthorized database write

### Requirement: Project text has deterministic Unicode validation
Name SHALL trim only ASCII SP/HT/CR/LF edges, then require1–100 code points and at most400 UTF-8 bytes, no remaining Unicode edge whitespace or Cc. Description SHALL preserve text without trimming, permit empty/HT/LF, reject other Cc and allow at most2000 code points/8000 bytes. No Unicode normalization SHALL occur. Invalid fields SHALL return422 validation_failed with fixed field reasons.

#### Scenario: Success - Multilingual and multiline values
- **WHEN** name includes Chinese, supplementary scalars and interior spaces, and description includes HT/LF or empty/ordinary whitespace
- **THEN** only specified name edges are trimmed, supplementary scalars count once and description/case/combining characters are preserved as data

#### Scenario: Exception - Field boundaries and fixed reasons
- **WHEN** trimmed name is empty, either field exceeds its code-point/byte bound, or text violates its control/edge policy
- **THEN** fields contains required for empty name, otherwise invalid_length before invalid_format per field; description CR/NUL/DEL/C1 controls and name Cc are rejected without persistence

#### Scenario: Security - Text is never executable or reflected in failures
- **WHEN** an otherwise valid field contains SQL-like text, quotes, markup or recognizable private sentinels
- **THEN** it is treated as a parameterized display value, errors/logs omit it and no HTML trust or SQL identifier interpretation occurs

#### Scenario: Concurrency - Equivalent names do not merge projects
- **WHEN** Owners submit equal, case-different or canonically equivalent Unicode names concurrently
- **THEN** each project retains its own identity and submitted normalized-by-policy text, without uniqueness conflicts or shared parser buffers

### Requirement: Project reads bind live identity membership and tenant ownership
Every project read SHALL evaluate active User, unrevoked unexpired Session, current Workspace membership and scoped project ownership in one database snapshot. Missing live identity SHALL be401 unauthenticated. Nonmembership, unknown/malformed Workspace or Project, and mismatched Workspace/Project SHALL uniformly be404 project_not_found. No unscoped existence lookup or cached authority SHALL bypass this decision.

#### Scenario: Success - Both roles and an empty workspace
- **WHEN** a current Owner or Member with live Cookie identity reads a matching project or an empty Workspace project list
- **THEN** detail is200, authorized empty list is200 with data:[], and the membership gate remains distinct from absence of projects

#### Scenario: Exception - Invalid identity has no empty-success fallback
- **WHEN** a structurally valid request uses missing, expired, revoked, cleaned-up Session or a disabled account
- **THEN** it returns401 rather than200/empty data, irrespective of whether supplied resource selectors match any project

#### Scenario: Security - Known UUID and tenant substitution cannot grant access
- **WHEN** an outsider substitutes wid/pid, a user belonging to both Workspaces combines A's wid with B's pid, or a pending invitee supplies the invited Workspace UUID
- **THEN** all inaccessible/mismatched resources receive identical404 code/message without name, creator, ETag or existence disclosure; invite preview grants no project authority

#### Scenario: Concurrency - Fresh reads lose removed authority
- **WHEN** removal, exit, revocation or disable commits before a new read snapshot, or after an already valid read snapshot
- **THEN** fresh reads deny404 for lost membership or401 for invalid identity, while the earlier authorized read can finish; other Workspaces remain unaffected and reads make no writes

### Requirement: Project lists use bounded workspace keyset pagination
GET projects SHALL accept only single optional limit and after. Limit SHALL be canonical decimal1–100, default50; after SHALL be a canonical lowercase hyphenated UUID. Rows SHALL be scoped to current Workspace membership and ordered by UUID ascending with at most limit+1 candidates. Response SHALL contain data and pagination.next_after, null unless another row exists. No aggregate all-Workspace endpoint SHALL be added.

#### Scenario: Success - Stable keys and final pages
- **WHEN** authorized projects span multiple pages, no projects exist or after is beyond the last row
- **THEN** pages follow id ASC, next_after is the last returned UUID only with an extra candidate, empty results are[] with null cursor and no total is exposed

#### Scenario: Exception - Selector grammar
- **WHEN** query contains duplicates, unknown or empty selectors, noncanonical limit/UUID, or any query on POST/PATCH/detail
- **THEN** it receives400 invalid_request without resource data or mutation; no offset/sort/filter option is accepted

#### Scenario: Security - Cursor is only a lower bound
- **WHEN** after equals a nonexistent UUID or an ID from another Workspace
- **THEN** it can affect the authorized range but never membership or tenant filtering, and the service does not look up or reveal that cursor resource

#### Scenario: Concurrency - Pages are fresh snapshots
- **WHEN** projects are created/renamed or membership changes between pages, including late commit of an ID below the cursor
- **THEN** names do not change ordering keys, each page reauthorizes and can reflect new state, and traversal does not promise a snapshot or coverage of every concurrent commit

### Requirement: Project success responses expose a stable minimal representation
Single-resource responses SHALL use data containing only id,workspace_id,name,description,version,created_at,updated_at. Version SHALL be a positive decimal string; timestamps SHALL be UTC RFC3339Nano. Confirmed POST SHALL be201 with canonical Location; detail/PATCH SHALL be200. Single-resource successes SHALL include strong ETag "project:<id>:<version>". List elements SHALL use the same fields without a resource ETag header.

#### Scenario: Success - Exact response and validator
- **WHEN** creation or detail succeeds at version1, or PATCH succeeds at version2
- **THEN** JSON contains exactly the prescribed fields with version as"1"/"2", description always a string, only POST has Location and ETag contains the exact returned id/version

#### Scenario: Exception - No unconfirmed success metadata
- **WHEN** storage or Commit cannot be confirmed or an operation returns an authorization/precondition failure
- **THEN** no success data, Location or ETag is returned and no public response asserts rollback merely from an error

#### Scenario: Security - Projection and Cookie privacy
- **WHEN** any project response or log is captured
- **THEN** it includes no creator/other-user identity, role/member roster, credentials or Session data; every outcome forbids Set-Cookie, and only permitted success text can appear in JSON

#### Scenario: Concurrency - Stable representations cannot overwrite login state
- **WHEN** Owner and Member read the same unchanged project while a login response overlaps
- **THEN** the resource representation and ETag are role-independent and stable for that version, and project responses cannot issue, renew or clear the browser credential

### Requirement: Project updates require a current strong precondition
PATCH SHALL require one strong If-Match project tag. Malformed, multiple, weak, wildcard or oversized tags SHALL be400. After current identity, scope and Owner checks, absence SHALL be428 precondition_required and a different project/version SHALL be412 precondition_failed. Successful updates SHALL change only name/description, increment version once and advance nondecreasing updated_at, even for equal values.

#### Scenario: Success - Current conditional partial edit
- **WHEN** an Owner PATCHes a matching scoped project with its current strong tag
- **THEN** exactly one update commits, omitted text fields persist, immutable fields stay unchanged and200 returns the new version/tag

#### Scenario: Exception - Header grammar and precondition outcomes
- **WHEN** If-Match is repeated, has multiple tags, weak/wildcard/empty/noncanonical/out-of-bigint content or exceeds96 bytes, is absent for an authorized existing project, or has a stale valid tag
- **THEN** it returns400/428/412 respectively; If-Match on GET/POST or any If-None-Match/If-Modified-Since/If-Unmodified-Since/If-Range returns400 without conditional-read semantics

#### Scenario: Security - Preconditions never grant or reveal authority
- **WHEN** a nonmember, mismatched project, invalid identity or Member write supplies a correct/absent tag
- **THEN** business evaluation denies401/404/403 before428/412, no current tag is leaked and a syntactically valid tag from another project gives412 only after authorization of the actual resource

#### Scenario: Concurrency - Stale concurrent intent cannot overwrite a new edit
- **WHEN** two distinct Owners PATCH with the same original tag, even for different fields, or retry after one response is lost
- **THEN** at most one commits that version, the later attempt gets412, equal-value writes still consume a version and the server neither merges nor refreshes the client's condition automatically

#### Scenario: Concurrency - ABA and exhaustion remain safe
- **WHEN** text changes A to B back to A, an old tag is replayed, or version reaches bigint maximum
- **THEN** old intent remains412 despite matching text, and overflow fails500 with unchanged state rather than recycling a version

### Requirement: Project routes reuse Cookie and same origin security protocols
POST/PATCH SHALL require the established exact Origin and X-DevPilot-CSRF:1 proof, with single same-origin Sec-Fetch-Site if present, before identity/database work. GET SHALL be read-only. Only the configured canonical Session Cookie SHALL authenticate. Ambiguous encoded/cleaned paths SHALL be rejected without redirecting mutations. No CORS or alternate credential transport SHALL be introduced.

#### Scenario: Success - Same-origin writes and Cookie-only reads
- **WHEN** a browser or CLI supplies the selected environment Cookie and required proof for POST/PATCH, or Cookie for GET
- **THEN** protocol validation can proceed without changing Cookie settings or requiring password work

#### Scenario: Exception - Method and path boundaries
- **WHEN** a request uses a canonical path with an unsupported method, or an extra/trailing segment, duplicate slash, dot segment or RawPath alias
- **THEN** canonical method failure is405 with exactAllow, ambiguous paths are404 with no redirect/write, HEAD has no body and OPTIONS grants no CORS

#### Scenario: Security - Forged origins and actor claims
- **WHEN** proof is missing/null/repeated/mismatched, Fetch Metadata is invalid, only Referer/Host is supplied or credentials appear only in Authorization/body/query
- **THEN** write proof failure is403 csrf_rejected before Session/DB work, absent valid Cookie is401 and no supplied user_id/role can authorize access

#### Scenario: Concurrency - Proof and routing stay request local
- **WHEN** valid writes overlap forged-origin or path-alias requests
- **THEN** the latter create no transaction/side effect, cannot borrow proof and cannot redirect into another request's legitimate resource scope

### Requirement: Project failures budgets and diagnostics are safe
Project requests SHALL have a5s processing budget and read/whole-transaction DB budget at most2s within it. Failures SHALL use fixed JSON error code/message, no-store/nosniff and no success headers or Cookies. DB unavailable/timeout SHALL be503 service_unavailable/database_timeout; schema or unexpected faults SHALL be500 internal_error. Cancellation SHALL stop further work and attempted responses. Logs SHALL contain only safe fixed categories.

#### Scenario: Success - Dependency recovery and fixed headers
- **WHEN** an ordinary request succeeds or dependencies recover after a prior failure
- **THEN** responses match the design's exact data/error and English messages with JSON/no-store/nosniff, and later requests work without global failure state

#### Scenario: Exception - Bounded reads and transactions
- **WHEN** pool/row-lock/query/Commit or body budget expires, schema is missing, or panic occurs
- **THEN** timeout/fault uses the documented503/500 if writable; processing-body timeout uses503 service_unavailable, pre-write panic is safe500 and post-write panic aborts without false success

#### Scenario: Security - Sensitive fault sentinels
- **WHEN** failures include SQL, raw driver errors, DSN, headers/body, email/PHC, Cookie/digest, names/descriptions, identifiers or If-Match sentinels
- **THEN** logs/error JSON contain none,422 contains only fixed field reasons and project responses never Set-Cookie

#### Scenario: Concurrency - Canceled clients and transport draining
- **WHEN** an HTTP client disconnects or a deadline cancels a slow/early-rejected request while other requests succeed
- **THEN** cancellation propagates and resources are bounded/released, no background mutation or synthesized200 is claimed, and independent requests retain their own identity/result state

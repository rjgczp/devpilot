# workspace-members Specification

## Purpose

定义 DevPilot AI 的 Workspace 成员名单、移除与主动退出契约，使成员权限撤销可被实时授权和真实数据库并发测试验证，并明确用户同意未实现之前不得开放直接加入接口的产品边界。

## Requirements

### Requirement: Member roster is live authorized and minimally projected
GET /api/v1/workspaces/{workspace_id}/members SHALL allow current owner/member identities only. One statement snapshot SHALL validate active user, valid session and workspace membership before projecting data[].membership_id,user_id,role,joined_at only. joined_at SHALL be the membership creation time in UTC RFC3339. Missing membership, unknown or noncanonical workspace UUID SHALL share 404 workspace_not_found; invalid live identity SHALL be 401 unauthenticated.

#### Scenario: Success - Both roles read a minimal roster
- **WHEN** an active Owner or Member with a valid Cookie reads its Workspace roster
- **THEN** 200 returns only membership UUIDs, member UUIDs, roles and joined_at, including existing disabled-user relationships without exposing account status, email, names, credentials or sessions

#### Scenario: Exception - Missing identity resource or membership
- **WHEN** a well-formed request has expired/revoked/unknown/disabled identity, or a valid identity has an unknown/noncanonical Workspace or no membership
- **THEN** the former is fixed 401 and the latter identical 404 workspace_not_found / Workspace not found.; dependency failures are safe 503/500 rather than false absence

#### Scenario: Security - Replaced tenant and identity claims
- **WHEN** a user substitutes another tenant's Workspace UUID or supplies another user's UUID, role, email or Authorization token as identity
- **THEN** no foreign roster is returned and no unscoped Workspace/user existence check or identity override is performed

#### Scenario: Concurrency - Roster snapshots and permission removal
- **WHEN** removal/revocation/disable commits before the roster statement snapshot or after that snapshot is authorized
- **THEN** the former applies immediately as 404/401 and the latter snapshot may finish; later requests and pages cannot reuse its authority

#### Scenario: Success - Relation validator is available without changing pagination
- **WHEN** a current member reads the roster after upgrade or a later rejoin
- **THEN** each row includes the current nonreused membership_id; the documented item validator is "membership:<membership_id>:<role>", while ordering/cursors remain user_id and existing Workspace projections are unchanged

### Requirement: Member pagination has strict bounded selectors
Roster GET SHALL accept only single optional limit and after selectors, order by user_id ascending, and fetch at most limit+1 candidates. limit SHALL be canonical decimal 1–100 with default 50; after SHALL be a lowercase hyphenated UUID without requiring a particular UUID version. Success SHALL return data and pagination.next_after, set to the last returned UUID only when more rows exist, otherwise null. Invalid selectors SHALL return 400 invalid_request.

#### Scenario: Success - Stable pages and empty suffix
- **WHEN** authorized callers page through a stable roster using default, minimum or maximum limit and then request an empty suffix
- **THEN** each 200 is bounded, stable traversal neither duplicates nor omits relationships, and an empty page has data:[] with next_after:null and no total count

#### Scenario: Exception - Selector syntax boundaries
- **WHEN** selectors are unknown/repeated, percent encoding is invalid, limit is empty/zero/over100/signed/padded/nondecimal or after is empty/uppercase/malformed
- **THEN** 400 invalid_request occurs without unbounded querying or reflected input

#### Scenario: Security - Cursor grants no membership
- **WHEN** after is a valid unknown UUID or another tenant's user UUID
- **THEN** it serves only as a sort boundary with no existence lookup and cannot expose another Workspace, global user record or permission

#### Scenario: Concurrency - Fresh pages without traversal snapshot
- **WHEN** relationships or permissions change between page requests
- **THEN** each page uses fresh identity/membership authorization and current rows, without a whole-traversal snapshot or total-count guarantee

### Requirement: Member deletion has a narrow permission matrix
DELETE /api/v1/workspaces/{workspace_id}/members/{user_id} SHALL derive its actor only from the valid Cookie. A member SHALL delete only itself; an owner SHALL delete ordinary members or itself subject to Owner retention, never another owner. Other-user deletion by a member and other-owner deletion SHALL return 403 membership_forbidden. A non-member SHALL receive 404 workspace_not_found before target lookup. Confirmed authorized conditional deletion SHALL return empty 204; missing or mismatched conditions SHALL follow the mandatory membership precondition contract.

#### Scenario: Success - Owner removal and member exit
- **WHEN** an Owner removes an ordinary Member, including a disabled target, or a Member deletes its own relationship
- **THEN** a matching membership condition and confirmed commit return 204 without body, JSON Content-Type or Location, and only the exact target relationship in that Workspace is removed

#### Scenario: Exception - Forbidden target role or operation
- **WHEN** a Member attempts another-user deletion or an Owner attempts to delete another Owner
- **THEN** fixed 403 membership_forbidden / Membership operation is not permitted. is returned with no write; a Member's other-user denial is independent of target existence/status

#### Scenario: Security - Actor cannot be a path or body claim
- **WHEN** an attacker substitutes target/Workspace IDs or sends actor_id, user_id or role in query/body to impersonate an Owner
- **THEN** client data never establishes the actor, non-member access stays fixed404 and rejected query/body performs no member mutation

#### Scenario: Concurrency - Target and actor changes while waiting
- **WHEN** another supported write removes or changes the actor/target before this delete gains protection
- **THEN** current actor membership/role, target relationship and supplied condition determine 404/403/428/412/204/409; stale entry authorization cannot perform a write

### Requirement: Delete selectors and representations are unambiguous
DELETE SHALL accept no query selectors and no nonempty body, and SHALL require the single strong If-Match defined by the membership precondition contract. Noncanonical workspace UUID SHALL yield the same authenticated 404 as an inaccessible Workspace. Noncanonical target UUID SHALL yield 404 member_not_found only after current Workspace membership is established, without querying users. UUIDs SHALL use canonical lowercase hyphenated form. Input rejection SHALL never select an arbitrary identity, tenant or member.

#### Scenario: Success - Canonical target and empty representation
- **WHEN** a CSRF-valid authenticated delete has canonical path UUIDs, a valid single strong If-Match, no query and no body
- **THEN** it reaches transaction authorization without requiring a JSON Content-Type; the role inside its validator is a comparison condition, never an authority or role assignment

#### Scenario: Exception - Query body and path boundaries
- **WHEN** DELETE has any query or nonempty body, or an authorized Workspace request has uppercase/compact/malformed target UUID
- **THEN** query/body returns fixed400 with bounded body inspection before transaction, while target format returns fixed404 member_not_found / Member not found.; invalid Workspace remains workspace_not_found

#### Scenario: Security - Parsing cannot become an existence oracle
- **WHEN** an outsider sends malformed or canonical target IDs under an inaccessible Workspace
- **THEN** the Workspace denial precedes target lookup/format response and neither users nor another tenant's membership is inspected for target disclosure

#### Scenario: Concurrency - Malformed and valid requests stay independent
- **WHEN** a valid DELETE overlaps malformed paths, extra segments, query/body injection or a canceled body read
- **THEN** rejected requests neither redirect into mutation nor share parser/actor state; cancellation prevents subsequent business work or attempted response

### Requirement: Last owner can leave only when another owner remains
An owner SHALL be allowed to delete itself only if another owner relationship remains in the same Workspace after the transaction. Removing the last owner SHALL return 409 last_owner_required with message Workspace must retain an owner. The invariant SHALL count owner relationships, not active accounts. No public role-change or owner-transfer endpoint SHALL be introduced.

#### Scenario: Success - Owner exit retains another owner
- **WHEN** a valid Owner exits a Workspace that has another Owner in a protected multi-owner fixture
- **THEN** confirmed204 removes only the caller and at least one owner relationship remains

#### Scenario: Exception - Last owner exit
- **WHEN** the only remaining Owner attempts self-deletion
- **THEN** fixed409 is returned, the relationship is preserved and no downgrade or automatic replacement is performed

#### Scenario: Security - Owner and account-state limits are explicit
- **WHEN** clients attempt role injection/owner removal or review a Workspace whose last Owner account is disabled
- **THEN** no owner promotion/removal shortcut exists; account disable does not delete membership and this contract makes no guarantee of an active Owner or account recovery interface

#### Scenario: Concurrency - Two owners exit or remove each other
- **WHEN** two Owners concurrently self-delete, or target one another
- **THEN** self-exit serializes so only one deletion can commit and the remaining-owner exit gets409; mutual other-owner removal gets403/403 and retains both, regardless of lock acquisition order

### Requirement: Delete is state idempotent under current authorization
Conditional DELETE SHALL change at most the matching current relationship once. An authorized Owner with an absent or mismatched target SHALL receive412 precondition_failed without global user lookup. A removed caller SHALL receive404 and invalid identity401. Retries SHALL NOT refresh an old condition into a later relationship's condition or reuse cached authority.

#### Scenario: Success - Owner repeats a removal
- **WHEN** an authorized Owner repeats DELETE for an absent relation, including a canonical UUID with no global user
- **THEN** each request with a stale condition returns412 precondition_failed and no other member, user, session or Workspace changes

#### Scenario: Exception - Self-exit replay loses authority
- **WHEN** a Member or nonfinal Owner retries its successful self-exit with a still-valid Cookie
- **THEN** the fresh request returns404 workspace_not_found; if the Cookie later becomes invalid it returns401, without clearing Cookie or replaying old204

#### Scenario: Security - Other-tenant target existence stays hidden
- **WHEN** an Owner supplies the UUID of a user belonging only to another Workspace, or a Member probes absent targets
- **THEN** the Owner gets the same412 as any absent/mismatched relationship after satisfying the required precondition syntax and the Member gets the same forbidden response as other-user deletion, without unscoped user queries

#### Scenario: Concurrency - Overlapping remove and exit
- **WHEN** two Owners remove one Member, or an Owner removal races that Member's self-exit
- **THEN** duplicate authorized Owner deletes with one relation condition are204/412; Owner-first removal makes later self-exit404, while self-exit-first makes later Owner delete412, with one relationship removal and no stale privilege reuse

### Requirement: Joining remains a separate consent-based capability
Joining/rejoining SHALL be available only through the bound-recipient invitation acceptance in workspace-invitations, granting member. Direct POST /members and public role-grant SHALL remain unavailable. Unsupported member collection/item methods SHALL return405 with Allow: GET/DELETE respectively; HEAD SHALL have no body and OPTIONS SHALL grant no CORS. Fixtures SHALL NOT substitute for consent.

#### Scenario: Success - Smaller delivery is accurately described
- **WHEN** API documentation and acceptance evidence are reviewed
- **THEN** listing/removal/conditional exit plus the separate invitation acceptance are documented, and only the target Session can consent to joining/rejoining, with no production SQL or seed endpoint substituting for them

#### Scenario: Exception - Direct add or role mutation request
- **WHEN** any caller POSTs a registered/unknown/disabled user UUID to /members or uses unsupported PUT/PATCH/HEAD/OPTIONS methods
- **THEN** method rejection is fixed405 with the exact Allow header before database work, regardless of body, target existence or Workspace access, and HEAD has no body

#### Scenario: Security - UUID and owner role are not consent
- **WHEN** an Owner knows a target UUID or submits role:owner with an add request
- **THEN** no membership is created and no account existence is disclosed; the separate invitation acceptance requires explicit consent under the target's valid identity and cannot grant owner from client input

#### Scenario: Concurrency - Rejected joins cannot race into existence
- **WHEN** unsupported joins or role updates overlap roster reads and allowed removals
- **THEN** rejected requests perform zero DB operations and cannot restore a removed member, change its role or manufacture a success result

### Requirement: Membership deletion requires a current relation precondition
DELETE SHALL require exactly one strong If-Match: "membership:<canonical membership_id>:<role>". Missing SHALL be428 precondition_required after live identity/scope/action checks; absent, changed role or different generation SHALL be412 precondition_failed. Invalid syntax SHALL be400. Only matching protected deletion with confirmed commit SHALL be204. No unconditional fallback SHALL exist.

#### Scenario: Success - Current relation can be removed
- **WHEN** an authorized Owner removes a member, or a member/eligible Owner exits using its current roster-derived validator
- **THEN** exactly the specified Workspace/user/membership_id/role is removed, with last-Owner409 still enforced, and204 follows confirmed commit only

#### Scenario: Exception - Missing wildcard weak or stale condition
- **WHEN** a request omits If-Match, uses duplicate/list/wildcard/weak/noncanonical/empty tags, or supplies a well-formed stale/absent relation validator
- **THEN** after the applicable documented parsing/authorization precedence it receives428,400 or412 respectively without deletion; forbidden target roles remain403 and lost actor scope404

#### Scenario: Security - Generation condition does not grant permission
- **WHEN** an outsider or Member supplies a known valid other-user validator, a role in a tag, or a membership_id from another Workspace
- **THEN** existing scope/role checks still apply, no target account lookup occurs, and the condition cannot select another tenant or assign a role

#### Scenario: Concurrency - Delayed old delete cannot delete rejoined membership
- **WHEN** D_old for M1 pauses before its Workspace lock, another removal commits M1 deletion, and invitation acceptance commits M2 before D_old resumes
- **THEN** D_old's M1 validator receives412, M2 remains and its membership_id differs; if D_old already holds the Workspace lock, rejoin waits and any later M2 still survives that M1 deletion

#### Scenario: Concurrency - Delayed self exit and explicit replay
- **WHEN** a delayed self-exit or client replay carries M1's condition after the caller rejoins as M2
- **THEN** a still-valid current actor cannot delete M2 through M1's condition, and no automatic retry or validator refresh turns the old request into a newly authorized M2 removal

#### Scenario: Exception - Legacy clients and mixed releases
- **WHEN** a legacy client sends no condition, or deployment/rollback would coexist with an unconditional-delete binary
- **THEN** the new API rejects the request428, and the release plan requires draining/stopping old writers before inviting/rejoining; no compatibility mode silently permits unconditional deletes

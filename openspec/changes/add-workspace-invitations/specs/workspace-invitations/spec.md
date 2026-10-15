# Spec Delta

## Purpose

定义 DevPilot AI 的 Workspace 平台内定向邀请，使目标用户通过自己的有效 Session 明确接受后才获得成员资格，并统一邀请发现、拒绝、撤销、过期、重复操作、身份隔离与并发安全的可验证产品契约。

## ADDED Requirements

### Requirement: Invitations require explicit consent from the bound recipient
The system SHALL support invitations bound to an existing user UUID. Creation SHALL NOT create membership. Only the bound active user with its own valid Cookie Session SHALL accept or decline. Acceptance SHALL grant only member. No invitation secret, email delivery, user search, application approval or direct member-add API SHALL be introduced.

#### Scenario: Success - Creation and consent are separate
- **WHEN** an Owner creates an invitation and the active target later explicitly accepts with its own valid Session
- **THEN** before acceptance no membership exists and Workspace detail/roster stay404 for the target; confirmed acceptance creates one member relationship and fresh existing Workspace endpoints immediately use it

#### Scenario: Exception - Target cannot act without live identity
- **WHEN** the target has an unknown, expired, revoked, deleted or disabled-account Session at the final authorization boundary
- **THEN** the action returns401 unauthenticated with no consent or membership change, even if earlier identity validation succeeded

#### Scenario: Security - Owner knowledge and role claims are not consent
- **WHEN** an Owner knows an invitation/target UUID or submits actor_id, role:owner, token or another user's Cookie claims
- **THEN** it cannot accept for that user, client identity/role fields are rejected, no owner relationship is granted and UUID knowledge alone confers no authority

#### Scenario: Concurrency - Consent and membership publish together
- **WHEN** another connection reads while acceptance is paused after either mutation but before commit
- **THEN** it cannot observe committed accepted without its new membership or the new membership without committed accepted; confirmed rollback exposes neither

### Requirement: Owners create strictly bounded directed invitations
POST /api/v1/workspaces/{workspace_id}/invitations SHALL accept only invitee_user_id as one JSON string and require a current Owner. Valid creation SHALL return201 with the Owner invitation projection. Unknown, disabled, self or already-member targets SHALL share422 invitee_unavailable after Owner authorization. Valid duplicate pending SHALL return409 invitation_pending.

#### Scenario: Success - Owner creates a seven day invitation
- **WHEN** an authorized Owner supplies another active nonmember's canonical UUID
- **THEN** 201 data contains exactly id,workspace_id,invitee_user_id,created_by_user_id,status,created_at,updated_at,expires_at; status is pending, creator is the Cookie actor, initial timestamps match and expiry is604800 seconds after creation, without Location or membership insertion

#### Scenario: Exception - Representation and target rejection
- **WHEN** body exceeds4096 bytes, Content-Type/encoding is unsupported, JSON is malformed/duplicate/unknown/missing/nonstring/multivalue/invalid Unicode, or the target UUID is noncanonical/unavailable
- **THEN** the applicable413 request_too_large,415 unsupported_media_type,400 invalid_request or422 invitee_unavailable occurs without new invitation; UUID semantic/unavailability reasons have one fixed message and no account fields

#### Scenario: Security - Workspace permission precedes target probing
- **WHEN** a current Member or outsider attempts creation using active, disabled, unknown or malformed targets
- **THEN** Member receives403 invitation_forbidden and outsider/unknown Workspace/noncanonical Workspace receives identical404 workspace_not_found; neither can query global target existence through this operation

#### Scenario: Concurrency - Duplicate creation is bounded
- **WHEN** multiple Owners create for the same Workspace/target in either order
- **THEN** at most one pending invitation commits and the other gets409; an expired old pending can be expired and replaced atomically with a new invitation UUID, without reviving an old record

### Requirement: Invitations have a fixed lifecycle and exact expiry boundary
Each invitation SHALL be pending, accepted, declined, revoked or expired. Only pending SHALL transition to one terminal state. Pending SHALL become effectively expired when expires_at is at or before the database authorization time. Terminal records SHALL never reopen or regain membership. GET SHALL NOT persist expiry, and expiry enforcement SHALL NOT depend on cleanup.

#### Scenario: Success - Four terminal outcomes
- **WHEN** the bound user accepts/declines, a current Owner revokes, or database time reaches expires_at
- **THEN** the invitation's only allowed outcome is respectively accepted/declined/revoked/expired; only accepted creates a member, and dates are UTC RFC3339

#### Scenario: Exception - Repeat and expired actions
- **WHEN** an authorized actor repeats any action on accepted/declined/revoked or acts on expired
- **THEN** it receives409 invitation_not_pending for the first three, or409 invitation_expired for expired, without changing membership or terminal timestamps; accepted stays accepted after its original expiry

#### Scenario: Security - Expiry cannot be client extended
- **WHEN** a client supplies expires_at, TTL, state or timestamps, or views a pending record whose expiry has elapsed
- **THEN** mutation input is rejected; read status is expired with updated_at=expires_at, and effective expiry prevents acceptance even if storage still says pending and no Worker has run

#### Scenario: Concurrency - Time advances during protected work
- **WHEN** invitation or Session expiry occurs during a protective wait, or only after the final post-wait authorization boundary
- **THEN** expiry at/before that boundary denies acceptance, while later expiry can allow the already-authorized transaction to commit; locks never imply frozen time

### Requirement: Invitation ownership belongs to the workspace
An invitation SHALL remain valid independently of its creator's subsequent membership, role, Session or account status. Creation SHALL require live Owner authority at its own boundary. Any current Owner of its Workspace SHALL list or revoke it. Recipient actions SHALL still require current recipient identity, invitation validity and surviving Workspace owner integrity.

#### Scenario: Success - A different Owner manages the invitation
- **WHEN** the creator exits, loses Owner role, has its Session revoked or is disabled after creation and another Owner remains
- **THEN** the pending invitation can still be accepted by its active recipient, and the remaining Owner can list or revoke it

#### Scenario: Exception - Management authority is lost
- **WHEN** removal or role demotion takes effect before create or revoke gains protected current authority
- **THEN** creation uses404 workspace_not_found or403 invitation_forbidden as applicable, and item revoke uses404 invitation_not_found, without stale Owner permission

#### Scenario: Security - Provenance does not delegate authority
- **WHEN** a creator who is no longer an Owner retains an invitation ID or another Owner inspects its provenance
- **THEN** creator identity alone cannot list/revoke/accept and no creator Session or account-state information is disclosed

#### Scenario: Concurrency - Creator changes and explicit revocation differ
- **WHEN** creator removal/disable overlaps acceptance, or a current Owner explicitly revokes while acceptance proceeds
- **THEN** creator changes alone do not invalidate the invitation, while explicit revoke follows the documented mutually exclusive accept/revoke outcomes

### Requirement: Invitation lists are scoped bounded and minimally projected
Owner GET /api/v1/workspaces/{workspace_id}/invitations and personal GET /api/v1/invitations SHALL return data arrays and pagination.next_after. Only optional single limit,after,status SHALL be accepted. limit SHALL be1–100/default50, after a canonical UUID, status pending/default or all. Results SHALL order by invitation id ascending and fetch at most limit+1.

#### Scenario: Success - Minimal role specific views and history
- **WHEN** an Owner lists its Workspace or an authenticated user lists its own inbox, including empty results, pending/default and all
- **THEN** Owner rows have exactly id,workspace_id,invitee_user_id,created_by_user_id,status,created_at,updated_at,expires_at; recipient rows exactly id,workspace_id,workspace_name,status,created_at,updated_at,expires_at, with workspace_name current only for effective pending and null for all terminal history; empty is[]/null and only a further row produces a next_after cursor

#### Scenario: Exception - Selector and authority failures
- **WHEN** selectors repeat/are unknown, limit is noncanonical/outside bounds, status is not pending/all, after is noncanonical or encoding invalid, or live identity/Owner scope fails
- **THEN** syntax fails400; invalid identity401, Workspace Member403 invitation_forbidden and outsider/unknown Workspace404 workspace_not_found, without unbounded queries or reflected input

#### Scenario: Security - Inbox is a narrow consent preview
- **WHEN** a nonmember receives an invitation or substitutes another target, Workspace, invitation or cursor UUID
- **THEN** only its own scoped invitation history and valid pending consent previews are visible, with no other recipients/creator/email/account fields; terminal history cannot disclose new Workspace names, Workspace detail/roster remain protected and cursor grants no authority

#### Scenario: Concurrency - Fresh snapshot and effective expiry
- **WHEN** status, membership, identity or expiry changes between pages or after an authorized statement snapshot
- **THEN** each new page uses fresh live authorization; default hides terminal/effectively expired rows, all reports terminal/effective state, and an earlier authorized snapshot can finish without a traversal snapshot or GET writes

### Requirement: Recipient acceptance is a one time bounded operation
POST /api/v1/invitations/{invitation_id}/accept SHALL take an empty body and no query. Bound-recipient confirmed acceptance SHALL return200 data with exactly invitation_id,workspace_id,membership_id,role,joined_at. Nonpending/expired SHALL use409; an existing membership SHALL use409 already_member without role overwrite. Reacceptance SHALL never recreate a removed relationship.

#### Scenario: Success - First acceptance and new invitation for rejoin
- **WHEN** an active recipient accepts a valid invitation, later leaves, then accepts a newly created invitation
- **THEN** each confirmed join uses member and a different membership_id; the original invitation remains accepted and grants no subsequent relationship

#### Scenario: Exception - Terminal existing or corrupt state
- **WHEN** an authorized accept has an existing relationship, a nonpending/expired invitation, or administratively ownerless surviving Workspace
- **THEN** respectively409 already_member,409 invitation_not_pending/invitation_expired, or safe500 internal_error occurs, with no promotion, overwritten relation, ownership repair or partial acceptance

#### Scenario: Security - Foreign target invitation is absent
- **WHEN** a well-formed active user accepts another user's invitation, unknown ID or noncanonical ID
- **THEN** identical404 invitation_not_found / Invitation not found. reveals no invitation or Workspace fields and creates no relationship, including when that caller is the Workspace Owner

#### Scenario: Concurrency - Parallel and repeated acceptance
- **WHEN** two valid accepts race for one invitation in either lock order, or the first confirmed acceptance's response was lost
- **THEN** at most one relationship is created, subsequent accepted actions return409 without replay, and authorized all-history/member queries can observe actual committed state

### Requirement: Recipients decline and current owners revoke pending invitations
POST /api/v1/invitations/{invitation_id}/decline SHALL allow only the bound recipient; DELETE /api/v1/invitations/{invitation_id} SHALL allow only current Owners of its Workspace. Both SHALL require an empty body/no query and return empty204 only after confirmed pending-to-terminal commit. Decline SHALL store declined and revoke SHALL store revoked, never delete history or membership.

#### Scenario: Success - Explicit refusal and withdrawal
- **WHEN** a recipient declines or any current scoped Owner revokes a live pending invitation
- **THEN** confirmed204 has no body/JSON Content-Type/Location, the respective terminal state is queryable via all, and no membership is created or deleted

#### Scenario: Exception - No pending transition is available
- **WHEN** either operation targets terminal or effective expired state
- **THEN** fixed409 invitation_not_pending or invitation_expired occurs; no repeated transition or new membership is produced and commit failure never masquerades as confirmed204

#### Scenario: Security - Revoke existence is Owner scoped
- **WHEN** a Member, outsider or removed/demoted creator revokes an item, or another recipient attempts decline
- **THEN** unknown, noncanonical and inaccessible items share404 invitation_not_found without distinguishing target/Workspace existence; malformed/extra body or query fails400 without mutation

#### Scenario: Concurrency - Accept competes with refusal or withdrawal
- **WHEN** accept races decline or revoke with either action taking protection first
- **THEN** accept-first can return200 and other action409; decline/revoke-first returns204 and accept409 with no membership; a failed first transaction leaves only confirmed committed state for the second

### Requirement: Invitation outcomes require confirmed commit and no automatic replay
Invitation mutations SHALL return success only for confirmed commits and SHALL NOT automatically retry or compensate after failure. Unknown Commit SHALL use safe503/500 when writable and SHALL NOT assert rollback. Canceled requests SHALL stop further business/response work. State and membership SHALL remain atomic even when commit acknowledgement is lost.

#### Scenario: Success - Observation after lost response
- **WHEN** an invitation mutation commits but its acknowledgement or HTTP response is lost
- **THEN** fresh authorized all-history and membership reads can reveal committed state, and an accepted invitation cannot restore a subsequently removed relationship through retry

#### Scenario: Exception - Timeout definite rollback and unknown commit
- **WHEN** pool/lock/statement budget expires, rollback is confirmed, or Commit has an uncertain outcome
- **THEN** timeout uses503 database_timeout and other faults safe503/500, no false201/200/204 appears, and only a confirmed rollback proves unchanged state

#### Scenario: Security - Failure details are fixed
- **WHEN** failures carry driver, SQL, Cookie, hash, DSN, user/invitation/member ID or name sentinels
- **THEN** neither public errors nor logs disclose those values, and no success Cookie/Location or background repair bypasses fresh authority

#### Scenario: Concurrency - Failure does not create a retry writer
- **WHEN** an ambiguous mutation overlaps acceptance, revocation, removal or rejoin
- **THEN** the server submits no automatic second mutation/Commit, subsequent requests reevaluate current state, and no compensation deletes a later valid membership

### Requirement: MVP invitation abuse boundaries are explicit
The service SHALL limit each Workspace to100 effective pending invitations and one stored pending per Workspace/recipient. Owner-visible eligible UUID success SHALL be documented as limited invitability disclosure, with unified unavailable failures. Public deployment SHALL require separate rate, recipient-frequency and abuse monitoring readiness; these caps SHALL NOT be claimed as full abuse prevention.

#### Scenario: Success - Bounded outstanding invitations
- **WHEN** creation would add the100th or101st effective pending invitation
- **THEN** the100th can succeed and101st gets409 invitation_limit_reached; terminal/effectively expired history does not consume this quota

#### Scenario: Exception - No delivery or retry shortcut
- **WHEN** a caller asks for token, email, resend, extension, bulk invitation or membership role grant
- **THEN** no such operation is exposed and no unsupported request creates or renews invitation state

#### Scenario: Security - Privacy limits are truthful
- **WHEN** an authorized Owner probes UUIDs or a deployment is reviewed
- **THEN** no email/status/search endpoint is exposed, eligibility disclosure and cross-Workspace bombing/reinvite risks are documented, and public release requires gateway rate limits, recipient policies and sanitized monitoring without claiming they are shipped here

#### Scenario: Concurrency - Quota cannot be oversubscribed
- **WHEN** different Owners concurrently create at99 effective pending invitations
- **THEN** at most one new pending commits before the quota is full and the other receives409, with no global lock combining independent Workspaces

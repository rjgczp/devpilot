# user-authentication Specification

## Purpose

定义 DevPilot AI 的登录、当前身份和退出行为，使客户端能够通过严格输入、安全失败响应及有界资源处理建立身份，同时保证账户禁用、数据库故障、取消和并发情况下不会错误授予认证。

场景分类：Success（成功）、Exception（异常）、Security（安全）、Concurrency（并发）。操作/发布场景以文档与评审验收；未来 Workspace 场景仅约束后续设计，不代表本轮实现。

## Requirements

### Requirement: Strict login request
The service SHALL accept only POST /api/v1/auth/login with one application/json object containing exactly string email and password fields, optional UTF-8 charset, no Content-Encoding, and at most 4096 body bytes. It SHALL reject duplicate, unknown, missing or non-string fields, multiple values, malformed UTF-8 and invalid raw Unicode escapes before credential lookup or Argon computation.

#### Scenario: Exception - Ambiguous JSON and invalid raw Unicode
- **WHEN** the request is empty, null, an array, multiple values, has duplicate/unknown/missing keys, non-string fields, invalid UTF-8, or isolated, reversed, truncated or malformed surrogate escapes
- **THEN** it returns 400 invalid_request without looking up credentials, hashing or creating a session

#### Scenario: Exception - Media and body boundaries
- **WHEN** a CSRF-valid POST has unsupported or missing media, unexpected media parameters or any Content-Encoding, or its JSON body exceeds 4096 bytes including chunked data and trailing whitespace
- **THEN** media rejection returns 415 unsupported_media_type and oversize JSON returns 413 request_too_large without credential or password work

#### Scenario: Success - Exact decoded password
- **WHEN** valid JSON encodes supplementary scalars as raw UTF-8 or paired escapes, or contains a literal U+FFFD
- **THEN** the service accepts them within field bounds and preserves the decoded password bytes without normalization

#### Scenario: Security - Credential and privilege injection
- **WHEN** a CSRF-valid body injects status, user_id, session fields or raw invalid Unicode
- **THEN** 400 is returned before account lookup or resource acquisition, with no echoed input

#### Scenario: Concurrency - Independent parsers
- **WHEN** valid and malformed logins arrive concurrently
- **THEN** each request uses only its own bounded input and the malformed request cannot alter another request or its credential selection

### Requirement: Login field policy is independent of account existence
Login SHALL normalize email with the registration ASCII subset, trim SP/HT/CR/LF and lowercase the whole address. Login passwords SHALL contain 1–128 Unicode code points and at most 512 UTF-8 bytes, without trimming or normalization. Input-policy violations SHALL return 422 validation_failed with fixed field reason codes before account lookup; login SHALL NOT reapply the registration minimum of 15 code points.

#### Scenario: Success - Email compatibility and normalization
- **WHEN** email is a supported ASCII address with case and surrounding ASCII whitespace
- **THEN** lookup uses the same canonical value as registration, preserving dots and plus tags

#### Scenario: Exception - Field bounds and unsupported mailbox form
- **WHEN** email violates the registration subset/lengths, password is empty, or password exceeds 128 code points or 512 bytes
- **THEN** 422 includes only affected field names and required/invalid_length/invalid_format codes, never submitted values; unsupported email forms are documented as an MVP limit

#### Scenario: Success - Short incorrect password is an authentication attempt
- **WHEN** a CSRF-valid login has valid email and a 1–14-code-point password
- **THEN** it reaches bounded credential verification and receives 401 on mismatch rather than a registration-policy rejection

#### Scenario: Security - Validation cannot disclose account state
- **WHEN** the same invalid fields are submitted for an existing and nonexistent email
- **THEN** the same input-derived 422 is returned without account lookup or sensitive logging

#### Scenario: Concurrency - Canonical lookup consistency
- **WHEN** case/whitespace variants of a supported email are verified concurrently
- **THEN** each uses the same canonical account key while password bytes and validation errors stay request-local

### Requirement: Credential failures share one contract and bounded cost
Unknown email, wrong password, disabled account and invalid stored PHC SHALL produce identical 401 invalid_credentials with message Invalid email or password. and no Set-Cookie. Each such rejection SHALL perform exactly one approved-cost Argon verification: valid hashes use real verification regardless of status; missing or invalid hashes use a dummy that cannot authenticate. Capacity and dependency failures SHALL use their separate contracts.

#### Scenario: Exception - No account and invalid hash
- **WHEN** canonical email has no user, or the stored PHC is malformed or outside the approved policy
- **THEN** one dummy verification runs, the common 401 is returned, and no attacker-supplied PHC parameters reach Argon

#### Scenario: Exception - Wrong password or disabled account
- **WHEN** a user has valid PHC but its password does not match, or its status is disabled even with the correct password
- **THEN** one real verification runs, the identical 401 is returned, and no session is created or Cookie changed

#### Scenario: Security - Dummy password happens to match
- **WHEN** a missing-account attempt supplies the dummy's matching password
- **THEN** it still returns the common 401 and cannot create a session

#### Scenario: Security - Enumeration claims are scoped
- **WHEN** API security documentation is reviewed
- **THEN** it describes mitigation of login response/cost differences without promising constant network time or whole-system enumeration resistance, because registration retains its explicit 409 occupancy disclosure

#### Scenario: Success - Real credentials proceed
- **WHEN** an admitted request verifies the approved PHC of an active account with its correct password
- **THEN** one real verification permits the separately specified current-state check and session creation

#### Scenario: Concurrency - No identity reuse between attempts
- **WHEN** real, dummy and disabled-account attempts overlap within capacity
- **THEN** results remain tied to their own lookup and verification, with no cached last-user or dummy result authenticating another request

### Requirement: Password verification rejects unsafe PHC before computation
Verification SHALL accept only the existing canonical Argon2id v19 PHC format with m=65536 KiB,t=3,p=1, 16-byte salt and 32-byte output. It SHALL enforce the 1024-byte PHC bound, strict unpadded standard Base64 and exact structure before Argon computation. Zero, overflow, altered or over-budget parameters SHALL be rejected without expensive work. Derived-key comparison SHALL be constant-time.

#### Scenario: Success - Stored registration hash verifies
- **WHEN** Hash output is verified using the exact original password or a different password
- **THEN** the original matches and the different value does not, with no PHC or password disclosure

#### Scenario: Security - Structural and numeric probes
- **WHEN** PHC has wrong algorithm/version, extra segments, duplicate/unknown/reordered parameters, signed/zero/leading-zero/overflow numbers, memory above 65536, iterations above 3, parallelism above 1, or any tuple other than the existing one
- **THEN** parsing returns a safe invalid-hash outcome without invoking Argon, allocating requested memory or panicking

#### Scenario: Exception - Encoded payload probes
- **WHEN** PHC exceeds 1024 bytes or its salt/key has wrong length, padding, URL alphabet, whitespace, newlines or noncanonical trailing bits
- **THEN** parsing rejects before Argon computation, including fuzzed inputs

#### Scenario: Security - Reviewed parameter evolution
- **WHEN** a future password policy introduces another PHC tuple
- **THEN** acceptance requires an explicit reviewed verifier/budget update, while the database retains its existing parameter-independent storage constraint

#### Scenario: Concurrency - Parser and key isolation
- **WHEN** valid and malicious PHC values are parsed or verified concurrently
- **THEN** invalid values do not reach Argon and salts, derived keys and results remain request-local under the shared budget

### Requirement: Confirmed login creates an independent session
An active account with matching credentials SHALL receive 200 with only data.id,email,status,created_at and a fresh session Cookie after confirmed session commit. Creation SHALL recheck current status and the verified credential snapshot under a lock that serializes status/password updates. Login SHALL NOT alter user state or reuse a supplied token, and SHALL NOT revoke other sessions.

#### Scenario: Success - Registration then login
- **WHEN** an unverified active registered user supplies correct credentials
- **THEN** a session commits and the response uses the existing safe user projection without password_hash or a claim of verified email ownership

#### Scenario: Concurrency - State changes before session creation
- **WHEN** status or password_hash changes after verification but before the creation transaction obtains its user lock
- **THEN** the service returns the common 401 and creates no session; a concurrent change is serialized with the short creation transaction, not with Argon computation

#### Scenario: Concurrency - Repeated or concurrent logins
- **WHEN** two successful logins occur for the same user within capacity
- **THEN** each creates a distinct random session, old tokens are not reused, and neither automatically revokes the other or any previously issued session

#### Scenario: Exception - Unknown commit or failed randomness
- **WHEN** token randomness fails or session commit is not confirmed due to a database failure
- **THEN** no success or new Cookie is issued and no automatic retry occurs; an unconfirmed committed session may remain until expiry

#### Scenario: Security - Fresh token resists fixation
- **WHEN** a successful login arrives with a preselected or previously issued Cookie
- **THEN** a fresh server-generated token is issued only after commit; supplied tokens cannot determine the new secret or change user state

### Requirement: Login resource work is bounded across endpoints
The process SHALL share at most two concurrent Argon computations across registration and real/dummy login verification, without a waiting queue. Login SHALL additionally allow at most two full workflows from before user lookup through session creation. Saturated login capacity SHALL return 503 authentication_busy with Retry-After: 1. Processing SHALL use a 5s context with each DB operation limited to 2s within it.

#### Scenario: Concurrency - Mixed workload exhaustion
- **WHEN** two registration/login Argon computations occupy the shared budget and another valid login needs verification
- **THEN** it returns authentication_busy without starting another Argon computation or session insert; inexpensive invalid requests still receive their own 4xx

#### Scenario: Concurrency - Session creation stalls
- **WHEN** two admitted login workflows are awaiting credential reads or session creation, even after their Argon calls finished
- **THEN** a third valid login returns authentication_busy before user lookup and no workflow queue is created

#### Scenario: Exception - Cancellation and actual release
- **WHEN** clients cancel or server processing time expires during real/dummy verification
- **THEN** running computation retains its shared slot until return, admitted workflow capacity is eventually released, and no new DB work or Cookie write begins after cancellation; faults and panics also release acquired capacity

#### Scenario: Success - Budget measurements
- **WHEN** real Hash/Verify mixed benchmarks run at concurrency one and two
- **THEN** verification records hardware, Go version, sample count, latency, allocations and peak process memory, without equating two buffers with a process RSS cap or silently lowering parameters

#### Scenario: Security - Cheap abuse consumes no expensive slot
- **WHEN** invalid or CSRF-rejected requests arrive while password capacity is free or full
- **THEN** their own 4xx is returned without Argon work, slot acquisition or session creation; no endpoint bypasses the shared budget

### Requirement: Current identity depends on live database state
GET /api/v1/auth/me SHALL return 200 with the safe user projection only for a current valid session belonging to an active user. Missing, invalid, ambiguous, unknown, expired, revoked or disabled-user sessions SHALL return identical 401 unauthenticated with message Authentication required. The endpoint SHALL not renew sessions, modify cookies, trust cached status or infer verified email or Workspace authorization.

#### Scenario: Success - Valid identity and no renewal
- **WHEN** a current session is queried repeatedly
- **THEN** data identifies the same user, stored timestamps/expiry remain unchanged, and no Set-Cookie is sent

#### Scenario: Exception - Invalid identity cases
- **WHEN** Cookie is absent, duplicated, malformed, unknown, expired, revoked or belongs to a disabled user
- **THEN** the same 401 is returned with no user projection and no Set-Cookie, even if the account was active when login occurred

#### Scenario: Security - Future tenant authorization
- **WHEN** a future Workspace operation consumes authenticated identity
- **THEN** it must separately authorize membership/roles using the trusted user UUID and cannot derive access from email, active status or client user_id; this change implements no Workspace authorization

#### Scenario: Concurrency - Old response cannot clear new login
- **WHEN** a stale or invalid me request overlaps with a successful login
- **THEN** me neither renews nor deletes Cookie and cannot remove the newly issued browser credential; its identity decision remains only a query-time snapshot

### Requirement: Logout is protected and idempotent
POST /api/v1/auth/logout SHALL apply CSRF protection before revocation or Cookie changes. A parseable current token SHALL be revoked regardless of user status or prior expiry. Confirmed revocation, no matching session or unusable/missing Cookie SHALL return 204 with an empty body and deletion of only the current environment's Cookie. A revocation database failure SHALL preserve Cookie and return a safe error.

#### Scenario: Success - Revoke one session and replay
- **WHEN** logout confirms revocation and the old token is replayed against me
- **THEN** logout is 204 with deletion Cookie, replay is 401, and other sessions for that user remain valid

#### Scenario: Success - Repeated logout and missing identity
- **WHEN** logout is repeated, the session is expired/disabled, or Cookie is absent, malformed or ambiguous
- **THEN** CSRF-valid requests return 204 and delete the current environment Cookie; unusable Cookie does not select an arbitrary token for DB revocation

#### Scenario: Exception - Database failure or uncertain revoke commit
- **WHEN** a token-bearing revoke encounters unavailable DB, timeout or internal error
- **THEN** it returns the corresponding 503/500 without deletion Cookie or an assurance of completed logout; later retry is idempotent even if the earlier revoke committed

#### Scenario: Security - Forged logout cannot change state
- **WHEN** a logout fails Origin or custom-header proof even when it carries a valid token
- **THEN** 403 is returned before revoke or Cookie deletion

#### Scenario: Concurrency - Repeated revoke and response ordering
- **WHEN** two valid logouts revoke the same token, or login and logout overlap
- **THEN** confirmed logouts are idempotent; unrelated sessions persist, while documentation does not promise deterministic browser Cookie ordering for overlapping authentication mutations

### Requirement: Authentication responses distinguish dependency failures safely
New endpoints SHALL use no-store and nosniff, safe JSON data/error envelopes except empty 204, and fixed messages. DB unavailability SHALL be 503 service_unavailable, confirmed timeout 503 database_timeout, and schema/internal defects 500 internal_error, never 401 or authenticated fallback. HTTP logs SHALL omit credentials, email, tokens, digests, headers, raw errors, SQL and traces.

#### Scenario: Exception - Route methods
- **WHEN** login/logout use a method other than POST or me uses a method other than GET
- **THEN** it returns 405 method_not_allowed with the respective Allow header before work; HEAD has no body and OPTIONS does not grant CORS

#### Scenario: Exception - Database error classes and recovery
- **WHEN** DB is unavailable, a child deadline or confirmed statement timeout occurs, a required table is missing, or unexplained SQLSTATE 57014 occurs
- **THEN** safe documented 503/503/500/500 responses result with no authentication fallback; after dependency recovery valid requests can succeed without restart

#### Scenario: Security - Cancellation and panic privacy
- **WHEN** the original client context is canceled, or a dependency panics with recognizable sensitive content
- **THEN** canceled requests get no attempted response, pre-write panics return safe 500, post-write panics abort the connection, and logs reveal only fixed categories without the sensitive content

#### Scenario: Success - Public responses match their contract
- **WHEN** login or me succeeds or logout completes
- **THEN** the exact documented projection or empty 204 and safe headers are returned without credential fields

#### Scenario: Concurrency - Per-request failure handling
- **WHEN** one request fails with a sensitive dependency error while another succeeds
- **THEN** the failure cannot contaminate the other response or request state, and no process-global authentication failure latch is set

### Requirement: Public authentication requires separate abuse controls
Documentation SHALL require implemented and reviewed login rate limiting and brute-force/credential-stuffing defenses before public deployment, in addition to existing registration abuse controls. It SHALL specify source and account budgets, cross-instance consistency, trusted proxy handling, lockout denial-of-service risks and executable acceptance criteria; two process slots SHALL not satisfy these release prerequisites.

#### Scenario: Exception - Only slots are present
- **WHEN** public deployment is reviewed with only concurrency/time budgets implemented
- **THEN** the release prerequisites remain unmet until rate limiting and abuse defenses are implemented and reviewed; this change records rather than implements them

#### Scenario: Success - Explicit release checklist
- **WHEN** planning and implementation documentation are reviewed
- **THEN** the required source/account budgets, recovery criteria and evidence owners are specified, without representing future controls as delivered

#### Scenario: Security - Lockout and enumeration review
- **WHEN** future login abuse defenses are designed
- **THEN** the review includes nonexistent-account keys, credential stuffing and attacker-induced account lockout, while preserving safe error behavior

#### Scenario: Concurrency - Cross-instance enforcement
- **WHEN** public deployment has several API instances
- **THEN** abuse-control review requires consistent rate/failure budgets across instances rather than multiplying independent process slots into a global defense

### Requirement: Authentication has executable verification
Unit tests, real PostgreSQL 18 integration tests and CI SHALL verify authentication/session contracts and existing registration/runtime regressions. Explicit integration runs SHALL fail without the guarded test database or pinned migration tooling. API and operational documentation SHALL agree with error, Cookie, CSRF, concurrency and uncertain-commit behavior, and evidence SHALL distinguish measured results from planned controls.

#### Scenario: Success - End-to-end and isolated verification
- **WHEN** CI or a developer runs the documented integration tag with the isolated test database
- **THEN** real registration/login/me/logout, PHC, persistence, expiry, disabled state, races and DB failures are exercised without development data mutation

#### Scenario: Exception - Missing dependency
- **WHEN** explicit integration runs lack the required URL, PostgreSQL version, reachable DB or Goose
- **THEN** they fail safely rather than skip or fall back to DATABASE_URL

#### Scenario: Security - Test target and evidence protection
- **WHEN** a test URL targets the application database or evidence contains raw secrets
- **THEN** the unsafe target is rejected before mutation and only redacted evidence is published

#### Scenario: Concurrency - Race and schema-suite separation
- **WHEN** mixed endpoint/race tests and migration tests are run
- **THEN** concurrent request paths are exercised but independent destructive suites do not mutate the same schema simultaneously

#### Scenario: Success - Absolute lifetime contract matches documentation
- **WHEN** the default-policy API documentation and real PostgreSQL tests are checked
- **THEN** both establish expires_at-created_at=24h, repeated me never changes expiry or emits Cookie, expiry returns 401 even with retained rows, and no refresh endpoint or Refresh Token is defined


### Requirement: Authentication does not grant transaction-level Workspace authority
Session authentication SHALL establish only a query-time user identity, not Workspace permissions or authority for an entire business operation. Protected writes SHALL define transaction-level resource authorization and coordination with concurrent session, account or membership changes. A prior middleware check or an ordinary read inside a transaction SHALL NOT alone be claimed to provide that atomic guarantee. Authentication endpoints SHALL grant no Workspace permissions; Workspace capabilities SHALL own their membership and transaction authorization contracts.

#### Scenario: Success - Identity is a limited input to future authorization
- **WHEN** a Workspace or future service receives a trusted authenticated user UUID
- **THEN** it must independently authorize the requested resource and action; authentication provides identity but no membership or tenant access grant

#### Scenario: Exception - Revalidation cannot be established
- **WHEN** a business operation cannot establish its required current identity or authorization because of DB failure or stale state
- **THEN** it must not proceed solely on the entry check; the feature must define safe rejection and retry behavior

#### Scenario: Security - Untrusted tenant and user claims
- **WHEN** a client supplies another user_id, Workspace id or role
- **THEN** the supplied values cannot override authenticated identity or prove access, and active/unverified email cannot establish tenant ownership

#### Scenario: Concurrency - Permission changes between entry and write
- **WHEN** session revocation, account disable, membership removal or role reduction overlaps a protected write after entry authentication
- **THEN** that feature must define a coordination protocol and test commit ordering; neither this identity result nor simply using one transaction guarantees permission throughout the operation

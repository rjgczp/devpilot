# session-management Specification

## Purpose

定义 DevPilot AI 服务端 Session 的秘密令牌与 PostgreSQL 存储、生命周期和浏览器 Cookie 安全边界，使过期、撤销、禁用及跨源请求有一致可验证的行为，并为开发 HTTP 和生产 HTTPS 提供明确配置约束。

场景分类：Success（成功）、Exception（异常）、Security（安全）、Concurrency（并发）。用户最终批准本轮实现清理Worker；Design的独立Change建议仅保留为评审取舍，不删除或停用本要求。过期/撤销校验本身不依赖 Worker。

## Requirements

### Requirement: Session secrets have high entropy and digest-only storage
Every new session SHALL use 32 cryptographically random bytes encoded as canonical unpadded Base64URL in a 43-character Cookie value. PostgreSQL SHALL store only the full SHA-256 digest of decoded token bytes. Authentication SHALL strictly validate the canonical token before hashing and use full digest equality, without accepting database digests, client-selected tokens or alternate token transports as credentials.

#### Scenario: Success - Independent secrets and stored representation
- **WHEN** sessions are created for the same or different users
- **THEN** their fresh random values differ, token_hash is exactly 32 bytes, and database rows contain neither original bytes nor encoded tokens

#### Scenario: Security - Token format and alternate transport
- **WHEN** a token has wrong length, invalid alphabet, padding, noncanonical bits or duplicate same-name Cookies, or appears only in URL, JSON or Authorization
- **THEN** it cannot authenticate and no expensive password work occurs; the server does not choose an arbitrary duplicate Cookie

#### Scenario: Exception - Randomness failure or hash collision
- **WHEN** cryptographic randomness fails or insertion hits token_hash uniqueness unexpectedly
- **THEN** creation returns safe 500 without issuing a Cookie, automatic retry or exposing token material

#### Scenario: Concurrency - Secrets and digest lookups remain independent
- **WHEN** sessions are created and queried concurrently
- **THEN** each request hashes its own decoded token and no shared mutable token buffer can authenticate another request

### Requirement: Sessions have a constrained additive database schema
A new explicit migration SHALL create sessions with database-generated UUIDv7 id, user_id foreign key with delete cascade, unique non-null 32-byte token_hash, non-null timestamptz created_at and expires_at, and nullable revoked_at. Expiry SHALL follow creation; revocation SHALL not precede it. User, expiry and revoked-row access SHALL be indexed. Existing user rows and applied migrations SHALL remain unchanged.

#### Scenario: Security - Invalid SQL and dependent deletion
- **WHEN** direct SQL supplies missing required fields, non-32-byte/duplicate digest, unknown user, invalid expiry or revocation time
- **THEN** constraints reject invalid rows; deletion of a user removes its dependent sessions without allowing orphan identity

#### Scenario: Success - Upgrade and repeated migration
- **WHEN** a populated users-only schema is upgraded and up is repeated
- **THEN** sessions and migration history are created atomically, users/credentials are preserved, and repeated up leaves data/schema unchanged

#### Scenario: Exception - Failure and disposable rollback
- **WHEN** a later test migration fails, or a confirmed disposable database performs the session migration's native one-step down then up
- **THEN** failure leaves no partial schema or applied version, and down removes only sessions while preserving users; shared/production releases retain status/up-only and forward-repair procedures

#### Scenario: Concurrency - Explicit migration serialization
- **WHEN** two releases need schema changes in one shared database
- **THEN** a single controlled migration step applies them serially; documentation does not assume an unconfigured native CLI prevents concurrent DDL

### Requirement: Database time determines session validity
Session validity SHALL require expires_at greater than current database statement time, null revoked_at and a currently active user. SESSION_TTL SHALL default to 24h and allow only 5m–24h. Creation SHALL derive expiry from database creation time. Reads SHALL not extend expiry or write last-seen state. Cookie expiry SHALL never replace server validation, and no idle or sliding renewal or Refresh Token is provided.

#### Scenario: Success - Expiry boundary and unchanged lifetime
- **WHEN** database time is before, equal to or after expires_at
- **THEN** the session is valid only strictly before expiry with other conditions satisfied, even if its Cookie persists, and repeated reads do not change stored times

#### Scenario: Security - Revoked or disabled identity
- **WHEN** a session is revoked or its user's current status is disabled
- **THEN** identity lookup rejects it irrespective of Cookie lifetime or status at login

#### Scenario: Security - Re-enable limitation is explicit
- **WHEN** an operator changes disabled back to active without revoking that user's sessions
- **THEN** otherwise unexpired/unrevoked sessions may become valid again; documentation requires future disable operations to revoke all sessions atomically and does not claim an account-management API exists in this change

#### Scenario: Exception - Validity query cannot be completed
- **WHEN** the DB is unavailable or the validity query times out
- **THEN** the request receives the documented dependency error without treating Cookie expiry or cached identity as a fallback

#### Scenario: Concurrency - Expiry and revocation independent of cleanup
- **WHEN** expired and revoked rows remain physically stored because cleanup is absent, stopped or behind while requests query them concurrently
- **THEN** every new validity query rejects them using database predicates, without relying on a deletion job or renewing any lifetime

#### Scenario: Success - Default absolute lifetime and no refresh
- **WHEN** SESSION_TTL is absent and a session is committed
- **THEN** database expires_at-created_at is exactly 24h; repeated access leaves both times unchanged, and neither sliding renewal nor Refresh Token issuance is provided

### Requirement: Revocation has explicit concurrency semantics
Revocation SHALL be idempotent and affect only the supplied session. Validation beginning after confirmed revoke commit SHALL reject it. Requests already validated before revocation or a status change can finish; the service SHALL not promise cancellation or transactional authorization of every in-flight request. Future protected business writes require authorization appropriate to their transaction boundary.

#### Scenario: Concurrency - Concurrent logout and identity read
- **WHEN** an identity read obtains its valid snapshot before revocation while another request revokes that token
- **THEN** the earlier read may finish successfully but a new read after revoke confirmation is denied

#### Scenario: Concurrency - Duplicate revocation and independent sessions
- **WHEN** concurrent requests revoke the same token
- **THEN** confirmed results are idempotent and other tokens remain unaffected, including after cleanup deletes the revoked row

#### Scenario: Success - Confirmed revoke blocks subsequent validation
- **WHEN** one current session is successfully revoked
- **THEN** a subsequent fresh validation rejects the token even if its row still exists

#### Scenario: Exception - Unknown revoke outcome
- **WHEN** a timeout or connection loss prevents confirming revoke commit
- **THEN** the service returns a safe dependency error, does not claim rollback/completed logout or clear Cookie, and a later explicit retry remains idempotent

#### Scenario: Security - Current token cannot select another session
- **WHEN** a request tries to revoke a different session using a user_id, session row id or stored digest without that session secret
- **THEN** those values do not select a revocation target or authorize access

### Requirement: Browser session Cookie has fixed security attributes
Production SHALL issue __Host-devpilot_session with Secure, HttpOnly, explicit SameSite=Lax, Path=/ and no Domain. Development SHALL use devpilot_session with the same attributes except Secure=false under the explicit local-HTTP policy. Confirmed expiry SHALL set bounded remaining Max-Age and UTC Expires. Deletion SHALL use identical scope/security attributes, Max-Age=0 and past Expires. No token SHALL appear in JSON or logs.

#### Scenario: Success - Production and local Cookie contracts
- **WHEN** login commits in production HTTPS or permitted development HTTP
- **THEN** only the selected name is issued with all specified attributes, no Domain, and a lifetime no longer than the confirmed database expiry

#### Scenario: Security - Scope-safe deletion and environment separation
- **WHEN** confirmed logout deletes the Cookie or the service switches modes
- **THEN** deletion uses the current mode's identical Cookie scope, and authentication ignores the other mode's Cookie rather than weakening security attributes

#### Scenario: Security - JSON and diagnostic privacy
- **WHEN** responses/logs are captured using sentinel password, PHC, token, digest or driver details
- **THEN** only successful Set-Cookie contains the bearer token; none of the sensitive values appear in JSON or application logs

#### Scenario: Exception - Creation not confirmed
- **WHEN** login commit cannot be confirmed or token generation fails
- **THEN** no new Cookie is issued, and attributes cannot be weakened as a recovery fallback

#### Scenario: Concurrency - Read responses do not overwrite login Cookie
- **WHEN** an old me response overlaps a successful login
- **THEN** the read sends no Set-Cookie and cannot renew, overwrite or remove the newly issued Cookie; overlapping login/logout mutations retain their documented response-ordering limitation

### Requirement: Authentication configuration fails closed before listening
Runtime SHALL read AUTH_MODE, AUTH_ORIGIN and SESSION_TTL only from environment and validate them before listening. AUTH_MODE SHALL default to production, which requires an explicit HTTPS origin. Development SHALL require HTTP loopback origin and explicit IP loopback HTTP_ADDR with matching port. Origin SHALL contain only scheme, host and optional legal port. Invalid settings SHALL fail with credential-free diagnostics.

#### Scenario: Exception - Required and bounded configuration
- **WHEN** AUTH_ORIGIN is missing, AUTH_MODE is unknown, TTL is malformed/outside 5m–24h, or Origin has credentials, any path, query, fragment, wildcard, list or invalid host/port
- **THEN** startup fails before listening with only the invalid variable name and safe reason

#### Scenario: Success - Secure default and proxy independence
- **WHEN** production uses a valid HTTPS AUTH_ORIGIN behind a TLS terminator
- **THEN** Cookie Secure remains true regardless of internal HTTP, request Host or forwarded headers; documented deployment requires protected internal listening and mandatory external HTTPS

#### Scenario: Success - Explicit local exception
- **WHEN** development specifies a loopback HTTP origin and explicit IP loopback listener with the same port
- **THEN** local HTTP can use the development Cookie; wildcard, public/LAN listener, non-loopback origin, mismatched port or HTTPS development origin is rejected

#### Scenario: Security - No implicit environment downgrade
- **WHEN** a production request claims HTTP or sends spoofed Host/Forwarded/X-Forwarded-* headers
- **THEN** it cannot change Cookie name, Secure policy or configured CSRF target

#### Scenario: Concurrency - Per-process immutable policy
- **WHEN** multiple instances with valid explicit configurations handle requests concurrently
- **THEN** each uses its own startup policy without learning Origin or Secure settings from any request; deployment review requires a consistent intended public-origin/mode policy

### Requirement: Login and logout require explicit same-origin CSRF proof
Login and logout SHALL require exactly one valid non-null Origin matching AUTH_ORIGIN by complete scheme/host/effective-port and exactly one X-DevPilot-CSRF: 1 header before credential or session work. If Sec-Fetch-Site exists, it SHALL be a single same-origin value. Missing, ambiguous or mismatched proof SHALL return 403 csrf_rejected without Cookie changes. No CORS permission or Referer-only fallback SHALL be provided.

#### Scenario: Success - Protected login without a prior session
- **WHEN** a CSRF-valid same-origin JSON login has no Cookie
- **THEN** credential verification may proceed; absence of existing authentication does not remove login CSRF checks

#### Scenario: Security - Forged source and header combinations
- **WHEN** Origin is missing/null/repeated/multi-value, uses an attacker suffix, different subdomain/scheme/port, only Referer, or the custom header is missing/repeated/wrong
- **THEN** both POST endpoints return 403 with no credential lookup, Argon computation, database mutation or Set-Cookie

#### Scenario: Security - Fetch metadata and preflight
- **WHEN** an unsafe request has Sec-Fetch-Site other than same-origin, or a cross-origin client attempts preflight
- **THEN** unsafe requests fail CSRF checks and OPTIONS returns 405 without CORS grants; absent Fetch Metadata does not waive Origin/header proof

#### Scenario: Success - Non-browser clients and read-only identity
- **WHEN** curl calls a protected POST or a browser queries GET me
- **THEN** curl must supply the same Origin/custom-header proof, while me needs only its session Cookie and performs no session or Cookie writes

#### Scenario: Exception - Unparseable proof fails closed
- **WHEN** Origin is syntactically invalid or contains ambiguous values
- **THEN** 403 is returned with no Referer/Host fallback and no credential or session side effects

#### Scenario: Concurrency - Proof is request-local
- **WHEN** a valid same-origin POST and a forged POST run concurrently
- **THEN** the valid proof cannot be reused as global authorization for the forged request, which is rejected before work

### Requirement: Cleanup is bounded and independent of validity
The service SHALL run a managed cleanup worker every 15 minutes, deleting at most 1000 expired or revoked rows per attempt within a 2s DB deadline. Worker executions SHALL not overlap within a process; cooperating instances SHALL avoid blocking on the same candidate rows. Failures SHALL wait until the next interval without leaking secrets or changing validity checks. Shutdown SHALL cancel and await the worker within the existing total budget.

#### Scenario: Success - Batch size and retained active sessions
- **WHEN** expired/revoked rows exceed a batch and active sessions coexist
- **THEN** one attempt deletes at most 1000 eligible rows without deleting valid sessions or looping until exhaustion

#### Scenario: Concurrency - Multi-instance cleanup and database outage
- **WHEN** multiple workers share the database, candidates are locked, or DB/schema is unavailable
- **THEN** candidate locks are skipped, failures emit only safe categories, HTTP startup/health contracts remain intact, and stale rows never become valid merely because cleanup failed

#### Scenario: Concurrency - Process shutdown with cleanup in flight
- **WHEN** SIGINT/SIGTERM arrives during cleanup
- **THEN** worker context is canceled and existing managed-worker/resource shutdown ordering and total deadline are preserved without extra waits

#### Scenario: Exception - Failed batch waits for next interval
- **WHEN** an attempt encounters a database timeout, missing schema or connection failure
- **THEN** it emits only a safe category, waits until the next interval, and does not retry immediately or alter authentication behavior

#### Scenario: Security - Valid sessions are not garbage
- **WHEN** unexpired, unrevoked sessions coexist with cleanup candidates
- **THEN** the batch preserves valid rows, deletes only eligible candidates and never logs token/digest material

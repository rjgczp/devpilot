# Spec Delta

## Purpose

定义 DevPilot AI 用户注册的请求、账户创建结果与安全错误契约，使客户端可预测地处理成功、无效输入、重复邮箱和依赖故障，并确保密码及内部信息不被公开。

## ADDED Requirements

### Requirement: Strict registration request
The service SHALL accept only POST /api/v1/auth/register with application/json (optional UTF-8 charset), no content encoding, a body of at most 4096 bytes, and exactly one JSON object containing only string fields email and password. Raw JSON SHALL reject malformed Unicode escapes and unpaired UTF-16 surrogates before decoding; decoded UTF-8 validity alone is insufficient. Invalid requests SHALL NOT create users or start hashing.

#### Scenario: Valid JSON request
- **WHEN** a POST contains one bounded JSON object with valid email and password strings
- **THEN** the service proceeds to registration

#### Scenario: Malformed or ambiguous payload
- **WHEN** the body is empty, malformed, null, an array, multiple JSON values, invalid UTF-8, contains duplicate or unknown keys, or omits a field or uses a non-string field
- **THEN** the service returns 400 with code invalid_request and does not hash or persist

#### Scenario: Invalid raw Unicode escapes
- **WHEN** raw JSON contains an isolated high/low surrogate, reversed or mismatched surrogate pair, truncated escape or non-hex Unicode escape
- **THEN** it returns 400 invalid_request before hashing or storage, even if a permissive JSON decoder would replace it with U+FFFD

#### Scenario: Valid supplementary and literal replacement characters
- **WHEN** the password uses valid raw UTF-8 or paired surrogate escapes encoding the same supplementary scalar, or includes an explicitly valid U+FFFD scalar
- **THEN** the scalar is accepted within the password bounds, a surrogate pair counts as one code point and the exact decoded bytes reach hashing without normalization

#### Scenario: Unsupported representation
- **WHEN** a POST has missing or unsupported Content-Type, a non-UTF-8 charset, or any Content-Encoding header
- **THEN** it returns 415 with code unsupported_media_type and does not hash or persist

#### Scenario: Body limit bypass attempt
- **WHEN** a JSON POST exceeds 4096 bytes, including a chunked body or trailing whitespace
- **THEN** the service returns 413 with code request_too_large without hashing or persisting

#### Scenario: Wrong HTTP method
- **WHEN** a request uses a method other than POST on the registration path
- **THEN** the service returns 405 with Allow: POST and error code method_not_allowed; HEAD has the same status and headers with no body

### Requirement: Predictable email and password validation
The service SHALL use an MVP email policy: trim surrounding ASCII SP/HT/CR/LF, lowercase the whole address, and accept an ASCII dot-atom local part and DNS domain, local length 1–64 and total length at most 254 bytes. Unsupported formats SHALL NOT be described as universally invalid. Passwords SHALL contain 15–128 Unicode code points and at most 512 UTF-8 bytes without trimming, normalization, truncation or composition rules. Policy violations SHALL return 422 validation_failed.

#### Scenario: Normalized email and exact password
- **WHEN** the email is " Alice.Example+tag@Example.COM " and a valid password contains Unicode and spaces
- **THEN** the stored email is alice.example+tag@example.com and the hash uses the exact decoded password bytes

#### Scenario: Length and syntax boundaries
- **WHEN** email or password exceeds its stated bounds, email has an empty part, local consecutive/edge dots, display name, quoted local part, IP literal, fewer than two domain labels, a label over 63 bytes, a trailing domain dot, an invalid DNS label or non-ASCII character, or password has fewer than 15 code points
- **THEN** the service returns 422 with only affected field names and fixed reason codes, without hashing or persisting

#### Scenario: Password policy boundary success
- **WHEN** an otherwise valid request has a 15-code-point password, or a 128-code-point password within 512 bytes, including Unicode or spaces
- **THEN** the password is accepted without requiring particular character classes

#### Scenario: Sensitive validation input
- **WHEN** validation fails on a password or an email containing SQL-like text
- **THEN** no response or log echoes the submitted value, and no SQL is constructed from that input

#### Scenario: Mailbox outside the MVP policy
- **WHEN** an address uses an internationalized or quoted local part outside this supported subset
- **THEN** registration returns 422 without hashing or persisting, and API documentation describes an MVP compatibility limit rather than claiming the mailbox is invalid in all mail systems

### Requirement: Password storage resists offline guessing
The service SHALL store only a salted Argon2id v19 PHC string using memory 65536 KiB, 3 iterations, parallelism 1, a fresh 16-byte cryptographic random salt and a 32-byte output. Passwords and hashes SHALL NOT appear in HTTP responses or logs. Hashing failure SHALL prevent persistence and return 500 internal_error.

#### Scenario: Independent salts
- **WHEN** two distinct users register with the same valid password
- **THEN** the stored PHC strings have the specified algorithm and parameters, distinct salts and distinct hashes, and recomputation with the original password matches each derived key

#### Scenario: Salt generation failure
- **WHEN** cryptographic randomness or the hashing dependency fails
- **THEN** the service creates no row and returns a fixed internal_error without raw error details

#### Scenario: Credential disclosure probe
- **WHEN** registration succeeds or fails with a recognizable sentinel password
- **THEN** captured responses and application logs contain neither the sentinel password nor its PHC hash or salt

### Requirement: Successful registration creates an unverified account
After persisting a new user, the service SHALL return 201 with data containing only id, email, status and created_at. Initial status SHALL be active and email_verified_at SHALL be null. Active SHALL mean not disabled, not verified or authenticated; consumers SHALL NOT trust email ownership from active alone. Registration SHALL NOT issue tokens, cookies, sessions or mail, nor accept client-supplied account state.

#### Scenario: Successful registration
- **WHEN** a valid request uses an unused normalized email and persistence succeeds
- **THEN** exactly one user exists and the response fields match its UUID, normalized email, active status and UTC RFC3339 timestamp

#### Scenario: Persistence not confirmed
- **WHEN** the database reports an error or an ambiguous connection loss during insertion
- **THEN** no 201 is returned and the service does not retry or claim the insert was rolled back without evidence

#### Scenario: Account privilege injection
- **WHEN** the request includes id, status, email_verified_at or other extra fields
- **THEN** it returns 400 invalid_request and creates no account or authentication artifact

#### Scenario: Active does not prove mailbox ownership
- **WHEN** a newly registered user has status active and email_verified_at null
- **THEN** the account is represented as not disabled with unverified email ownership, and no response or documented downstream policy treats active alone as proof of email ownership

#### Scenario: Future email trust decision
- **WHEN** a consuming feature requires verified email ownership and encounters an active account with null email_verified_at
- **THEN** its acceptance policy rejects active as sufficient evidence and requires an independently established verification state; this change adds no such feature or verification endpoint

### Requirement: Duplicate registration preserves existing accounts
The service SHALL return 409 email_conflict when normalized email uniqueness prevents insertion, including concurrent requests and case variations. It SHALL NOT change the existing user's password, status, timestamps or verification state. This explicit conflict intentionally exposes email occupancy; the API SHALL NOT claim enumeration resistance.

#### Scenario: Duplicate and case variant
- **WHEN** an existing user's email is registered again with different casing or surrounding ASCII whitespace
- **THEN** the response is the same fixed 409 email_conflict and the existing row remains unchanged

#### Scenario: Concurrent first registrations
- **WHEN** two valid requests concurrently register the same normalized email
- **THEN** exactly one returns 201 and one returns 409, and exactly one complete user row exists

#### Scenario: Attempt to replace a password
- **WHEN** an existing email is registered with a different password
- **THEN** it receives 409, returns no existing user data and preserves the original password hash and account state

### Requirement: Bounded registration processing
The service SHALL allow at most two concurrent password hashes per process, with no waiting queue, and return 503 registration_busy with Retry-After: 1 when full. Registration SHALL use a 5-second processing deadline and at most 2 seconds for database work within that budget. Cancellation before insertion SHALL prevent database writes; a running hash MAY finish but SHALL retain its concurrency slot until finished.

#### Scenario: Capacity available
- **WHEN** one or two valid registrations are in progress and dependencies respond within the deadline
- **THEN** they can complete successfully and all acquired capacity is released after use

#### Scenario: Cheap rejection before resource acquisition
- **WHEN** an oversized, malformed or policy-invalid registration arrives while hash slots are available or saturated
- **THEN** it returns its documented 4xx response without acquiring a hash slot, invoking Argon2id or writing to the database

#### Scenario: Hash capacity exhausted
- **WHEN** two hashes are running and another valid registration arrives
- **THEN** it returns 503 registration_busy without hashing, queueing or inserting, and health endpoints remain available

#### Scenario: Server processing deadline
- **WHEN** the server processing deadline expires before insertion while the client request is still active
- **THEN** the service does not start insertion, releases capacity after any real hash returns, and attempts safe 503 service_unavailable without promising a hard interrupt of Argon2id

#### Scenario: Client cancellation or disconnect
- **WHEN** the client request context is canceled or the client disconnects before or during database work
- **THEN** the service stops initiating work, propagates cancellation to database operations and does not attempt to guarantee or write a 503 response for that canceled request; a possibly committed INSERT remains an unknown outcome

#### Scenario: Cancellation during both hashes
- **WHEN** two hashes are running and their clients cancel before hashing completes
- **THEN** both slots remain occupied until their hash calls actually return; another valid registration receives 503 registration_busy, no replacement hash starts, and canceled requests do not begin insertion

#### Scenario: Resource release and measured occupancy
- **WHEN** concurrent success, hash-error and cancellation tests complete, and a benchmark runs with real parameters at concurrency one and two
- **THEN** tests observe at most two running hashes and eventual reuse of every slot, while the benchmark records latency distribution and process memory including overhead rather than asserting an exact 128 MiB process cap

### Requirement: Hash resource limits do not establish abuse protection
Documentation SHALL identify the two hash slots as a per-process resource bound, not complete rate limiting or abuse prevention. Public deployment SHALL require implemented and reviewed registration rate limiting and anti-abuse controls as separate follow-up work. Performance targets SHALL be established from real benchmarks; safety parameters SHALL NOT be lowered without explicit review.

#### Scenario: Recorded resource baseline
- **WHEN** implementation benchmarks run with the agreed parameters
- **THEN** recorded hardware, Go version, latency and memory measurements support a reviewed performance budget without representing an unmeasured target as achieved

#### Scenario: Benchmark misses the intended budget
- **WHEN** measurements exceed the intended latency or resource budget
- **THEN** the result triggers review of capacity and budgets, and the implementation does not silently reduce memory, iterations or other safety parameters

#### Scenario: Public exposure review
- **WHEN** the registration endpoint is proposed for public deployment with only the two hash slots as protection
- **THEN** the deployment review remains unsatisfied until registration rate limiting and anti-abuse controls are implemented and reviewed; this change records that prerequisite without implementing those controls

### Requirement: Stable safe registration responses
Registration responses SHALL use application/json, Cache-Control: no-store and X-Content-Type-Options: nosniff, with data or error. DB connection failure SHALL return 503 service_unavailable; DB timeout SHALL return 503 database_timeout; missing users and other defects SHALL return 500 internal_error. Errors SHALL use fixed messages without raw PgError, SQLSTATE, SQL, traces or submitted values. Logs SHALL omit raw errors and sensitive values; responses SHALL NOT expose credentials.

#### Scenario: Documented client error
- **WHEN** registration returns any documented 4xx response
- **THEN** the JSON error matches its documented status/code and contains neither data nor submitted values

#### Scenario: Database connection unavailable
- **WHEN** PostgreSQL cannot be reached or its connection is lost during database work
- **THEN** registration returns fixed 503 service_unavailable and logs a safe connection category without exposing raw PgError or promising that a possibly submitted INSERT did not commit

#### Scenario: Missing users is a deployment defect
- **WHEN** PostgreSQL reports undefined_table for the users relation because its migration was not applied
- **THEN** registration returns fixed 500 internal_error, logs only a safe schema_missing category, and health endpoints retain their connectivity-only contracts

#### Scenario: Database deadline or confirmed statement timeout
- **WHEN** the database child context reaches its deadline or the server reports a confirmed statement timeout
- **THEN** registration returns fixed 503 database_timeout without raw PgError or a claim that insertion definitely failed; an unexplained query cancellation is not automatically labeled as a timeout

#### Scenario: Unknown insertion outcome
- **WHEN** a timeout or connection loss prevents confirmation of an INSERT that may have committed
- **THEN** the documented 503 response reports that registration could not be confirmed, does not promise failure or rollback, and causes no automatic server retry or credential replacement

#### Scenario: Unexpected persistence error
- **WHEN** an error other than the named email unique violation or a classified dependency failure occurs
- **THEN** it returns 500 internal_error and does not expose SQLSTATE, constraint names, connection details, email or credentials

### Requirement: Registration contract has executable verification
The repository SHALL provide unit tests, PostgreSQL 18 integration tests mandatory in CI, and API/developer documentation covering every registration status, input bound, normalization rule, account state and enumeration trade-off. Integration tests SHALL use an explicitly separate test database and fail rather than silently skip when invoked without it.

#### Scenario: Reproducible verification
- **WHEN** a developer follows documented commands on an isolated PostgreSQL 18 test database
- **THEN** unit and integration suites exercise registration success, boundary validation, real hashes, duplicate races and safe failures and agree with the API contract

#### Scenario: Integration dependency absent
- **WHEN** the integration suite is explicitly run without a reachable test database or required schema setup
- **THEN** it exits unsuccessfully with a credential-free diagnostic instead of reporting skipped success

#### Scenario: Test cleanup isolation
- **WHEN** integration setup receives the application's development database URL or an unapproved database name
- **THEN** it rejects the target before any schema mutation, migration down or row deletion

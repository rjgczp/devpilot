# Spec Delta

## MODIFIED Requirements

### Requirement: Bounded registration processing
The service SHALL share at most two concurrent Argon computations per process across registration hashing and login real/dummy verification, with no waiting queue, and return 503 registration_busy with Retry-After: 1 when full. Registration SHALL use a 5-second processing deadline and at most 2 seconds for database work within that budget. Cancellation before insertion SHALL prevent database writes; a running hash MAY finish but SHALL retain its concurrency slot until finished.

#### Scenario: Capacity available
- **WHEN** one or two valid registrations are in progress and dependencies respond within the deadline
- **THEN** they can complete successfully and all acquired capacity is released after use

#### Scenario: Cheap rejection before resource acquisition
- **WHEN** an oversized, malformed or policy-invalid registration arrives while hash slots are available or saturated
- **THEN** it returns its documented 4xx response without acquiring a hash slot, invoking Argon2id or writing to the database

#### Scenario: Hash capacity exhausted
- **WHEN** two registration/login Argon computations are running and another valid registration arrives
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

#### Scenario: Mixed endpoint capacity
- **WHEN** one registration hash and one real or dummy login verification occupy the process budget
- **THEN** another valid registration receives 503 registration_busy with Retry-After: 1, and no third Argon computation starts; login database work after verification does not retain an Argon slot

## ADDED Requirements

### Requirement: Shared password budget preserves registration isolation
Sharing the password budget SHALL preserve registration input, status, message, projection, hashing parameters and no-auth-artifact contracts. Login/session-specific faults SHALL not poison shared capacity or mutate registration state. Both endpoints SHALL release capacity after actual computation ends, including failures and panics. Intentional shared saturation and common DB failures SHALL remain explicit limitations rather than guarantees of endpoint independence.

#### Scenario: Success - Existing registration contract remains intact
- **WHEN** valid registration succeeds or duplicates an existing canonical email after shared-budget wiring
- **THEN** it returns the existing 201 Public projection or 409 email_conflict, uses unchanged PHC parameters, and issues no token, Cookie, Session or mail while preserving prior credentials/state

#### Scenario: Exception - Login-specific failures do not poison registration
- **WHEN** an admitted login encounters injected Verify panic, invalid PHC, missing sessions table or session-token RNG/create failure, and its password work has returned
- **THEN** all acquired capacity is reusable and an otherwise healthy registration can complete with users/schema/password dependencies available; no global authentication-failed flag is propagated to registration

#### Scenario: Security - Cheap rejection and privacy regressions are forbidden
- **WHEN** malformed, oversized, policy-invalid or raw-Unicode-invalid registration arrives during login load
- **THEN** its existing 4xx occurs before shared-slot acquisition or Argon/DB work; responses/logs expose no credentials and duplicates cannot overwrite password, status or verification fields

#### Scenario: Concurrency - Mixed cancellation and endpoint-specific busy codes
- **WHEN** real/dummy login work and registration hashing occupy both slots, then one client cancels
- **THEN** no third Argon call starts until a computation actually returns; registration uses registration_busy and login authentication_busy with Retry-After: 1, after which released capacity is reusable by either endpoint

#### Scenario: Concurrency - Database waiting does not reserve password capacity
- **WHEN** both login workflow slots are waiting on session database operations after verification has returned
- **THEN** they do not hold Argon slots and a registration with available password capacity can hash; the shared pool still imposes its normal bounded DB wait and no starvation-free guarantee is claimed

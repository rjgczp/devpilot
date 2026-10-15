# User Authentication Delta

## MODIFIED Requirements

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

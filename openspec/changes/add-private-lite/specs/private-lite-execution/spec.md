## Purpose
Define a separately enabled, trusted-host private HTTPS prototype that reuses existing scheduling and physical release while constraining all execution traffic outside Probe authority.

## ADDED Requirements
### Requirement: Lite is an explicit separate profile
The system SHALL default closed and require explicit Private-Lite configuration and entry. It SHALL retain existing Owner, SQL, resource and physical-release protocols without altering approved or fixture modes.
#### Scenario: Missing enable or legacy entry
- **WHEN** Lite enable/configuration is absent or supplied to an old entry
- **THEN** no execution is started and no legacy allowance is expanded

### Requirement: Operator directory and configured DNS constrain execution
The system SHALL accept only operator-approved scoped/versioned HTTPS targets and one pinned IPv4 TCP53 resolver. It SHALL retain complete DNS checks and binding, reject IPv6 target connections and never fall back to system DNS or 192.0.2.53.
#### Scenario: Unapproved or changed address
- **WHEN** scope, target, DNS digest or selected IPv4 differs from approval
- **THEN** no target request is sent
#### Scenario: Dangerous or IPv6 destination
- **WHEN** a target is private, metadata, management, special-use or IPv6
- **THEN** it is rejected by application and independent network constraints

### Requirement: Timed host and namespace policies precede execution
The system SHALL install default-deny namespace and veth-scoped host constraints before execution. Probe/Worker SHALL not modify them. Host policy table loss, load failure, timeout or explicit target revocation SHALL block new and established target traffic without relying on periodic detection.
#### Scenario: Host table lost
- **WHEN** either cross-checking host table is deleted while the other remains
- **THEN** packets fail closed and no new execution is authorized
#### Scenario: Termination or expiry
- **WHEN** permit expires, target is revoked or Supervisor is lost
- **THEN** network permission is removed and original trusted termination/quarantine semantics remain

### Requirement: Prototype evidence has an explicit boundary
Acceptance SHALL use disconnected Linux namespaces and disposable PG18, preserve legacy checks and disclose shared-host risk. It SHALL not alter real Lighthouse, production migration/Gate or send a public probe.
#### Scenario: Missing real-host evidence
- **WHEN** isolated acceptance succeeds
- **THEN** evidence is reported only for that topology, never as Lighthouse deployment acceptance

#### Scenario: All host nft tables lost
- **WHEN** both host nft policy tables or the complete nft ruleset disappear
- **THEN** an independently installed TC egress guard drops IPv4/IPv6 packets without the nft positive mark, including established traffic
#### Scenario: Docker API unavailable during revoke
- **WHEN** the daemon is unavailable but live-restore retains actors
- **THEN** the pinned namespace executor revokes permission without Docker API access and never substitutes this for signed physical release

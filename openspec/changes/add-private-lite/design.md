## Context
See proposal.md. Existing Kernel and Runtime own signed PID1 stop receipts, leases and SQL projection. Their default profile must remain byte-compatible on the wire.

## Goals / Non-Goals
Deliver an isolated prototype only. No Lighthouse, cloud resource, production SQL or public target access.

## Decisions
- A separate explicit CLI and profile-specific Go binaries opt in; no environment/default fallback. Private-Lite retains the existing private-approved SQL/Worker protocol in the disposable database; runtime status remains private-lite. No new migration or result store.
- Add a pinned DNS parameter only through Lite entry points. Old entry points reject it. Complete A/AAAA/CNAME review and digest binding remain; a selected target must be IPv4 TCP443.
- Retain run/gateway Permit filters and PID1 lifecycle. Add a trusted host adapter: narrow veth-scoped nft tables, timed DNS/target tuples, and positive packet marks across separate hooks. Each host table requires the other; deleting either table blocks traffic, including established flows. Rule installation is atomic and completes before any Worker/Probe. No broad established-flow bypass.
- Host root, kernel, Docker and the guard anchor are trusted. Simultaneous deliberate deletion of all host enforcement by root is outside the model, not claimed safe. No namespace-to-VM equivalence.
- Run tests in disconnected Linux host/holder/receiver namespaces with no upstream route. The test host stands in for Lighthouse; its rules never touch the real Docker host. Use existing PG18 harness for Owner, SQL, Supervisor loss and physical release.

## Risks / Trade-offs
- Shared kernel/daemon compromise can affect management and MySQL; independent VM required if that risk is unacceptable.
- New profile is not a deployment approval. Real host Docker hook ordering, cloud routing and capacity remain unverified.
- Revocation fails closed before physical drain; may stop the other bounded run too. Quarantine is not bypassed.

## Migration Plan
No production migration. Remove only owned isolated test resources; Gate stays closed outside disposable tests.

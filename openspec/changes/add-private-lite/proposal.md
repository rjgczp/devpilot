## Why
The existing routed profile requires a dedicated PCI NIC. Evaluate a separately opt-in, trusted-host profile for a small operator-approved HTTPS directory without buying resources or changing Lighthouse.

## What Changes
- Add a closed-by-default Private-Lite prototype, configurable pinned IPv4 DNS and IPv4-only HTTPS admission.
- Reuse existing Scheduler SQL, Worker, Owner consent, Permit, PID1 Supervisor and physical receipts; add only a network adapter and profile-specific binaries.
- Add two-stage host filtering outside Probe/Worker authority, scoped to one owned veth, plus the original run/gateway filters.
- Test only disconnected Linux namespaces and disposable PG18. No production migration, Gate, cloud network or public request.

## Capabilities
### New Capabilities
- `private-lite-execution`: explicitly enabled trusted-host execution profile with bounded host-network grants.
### Modified Capabilities
None. Existing private-approved and fixture contracts stay unchanged.

## Impact
Profile-specific composition, DNS parameter plumbing and narrow shared hooks; no new scheduling/result system. Host root, kernel and Docker daemon remain trusted; no claim of VM isolation or protection after simultaneous removal of all host defenses.

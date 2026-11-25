## 1. Profile and DNS
- [x] 1.1 Add explicit Lite-only execution/network profiles with configured DNS, IPv4 admission, legacy rejection tests and profile usage documentation.
## 2. Host adapter
- [x] 2.1 Add veth-scoped expiring cross-check host filters and reuse existing runtime/physical receipts; verify deletion, revoke and compatibility tests and document trust/minimum privilege.
## 3. Isolated acceptance
- [x] 3.1 Run disconnected Linux packet and disposable PG18 lifecycle acceptance, including DNS changes, unauthorized/private/IPv6 rejection, expiry and Supervisor loss; save evidence and add focused CI.
## 4. Review delivery
- [x] 4.1 Verify final diff, OpenSpec and applicable CI; submit independent PR with SHA, evidence and unproven shared-host risks, without merge or deployment.

CI status is recorded in verification.md and the PR; running long checks are not reported as passed. No merge/deployment is part of these completed implementation tasks.

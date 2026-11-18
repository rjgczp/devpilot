# Inactive M16 candidate

Order: immutable M1–10,12,13 → unchanged, separately isolated-approved M14 → M15 → this M16 candidate. No Migration11; no Goose activation.

Exact source hashes and generated diff: `permission-diff.json`. Builder only reads files.

- Add `private-approved` to the existing execution_mode/Gate CHECK sets. No new column, table, role or identity registration.
- Replace owner-only `private_gate_allows(text,uuid)` to require matching deployment and actual LOGIN/incarnation modes. Fixture/private-isolated identities cannot use private-approved.
- Replace only the inline Gate predicate in `authorize` and `execution_status`. API owner keeps existing SELECT; no helper EXECUTE.
- Add `approved_worker_ready(uuid)`, a definer own-incarnation boolean including original identity checks, fresh storage admission and closed/emergency/expiry gates. Only new runtime privilege: Worker group EXECUTE on this function. REVOKE ALL FROM PUBLIC; no direct table/column privileges. Existing private_worker_ready remains private-isolated only.
- No changes to SQL2s/lock250ms, Owner consent, target budgets, Fence, physical release, Result or retention logic. Gate/mode updates are operator actions, absent from candidate.

Application requires deployment gate closed + emergency stop + no unphysically-released Run. Production application, five runtime LOGINs in release proposal, concrete registrations and real egress remain unapproved. Requested isolated application is only disposable PG18/native Linux with no upstream, existing test LOGINs and private-approved Gate for owned HTTPS validation. This is neither fixture nor private-isolated admission.

Product owner approved exact SHA256 `264ddb9bcb8bf48cf305bc81d2ce58434f0e0ff2dac9ad5f2b45ae0210fcf095` on2026-11-17 for the stated disposable PG18/Linux/no-upstream scope. Bound record: `isolated-test-approval.json`. No production authority.

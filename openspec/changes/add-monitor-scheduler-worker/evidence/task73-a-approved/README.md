# Task7.3 A correction — independently approved design, CI pending

On2026-10-28 Asia/Shanghai, the human owner explicitly approved the A design:
without trustworthy persisted denial provenance, unresolved started remains
unknown rather than inferred canceled. The same message authorized committing
only the already directed-tested correction and necessary tests/evidence, then
one normal Push/PR CI run at the frozen commit. This is not final7.3 approval.

Parent HEAD f596a25553dcffb77a9e972d547d6e5a383c593d.
All14directed PG18/Linux invocations (7normal/7race, each once) and build/vet
match source inventory093e195b725a25d9d55659a4a8a2e2f5cca6d4cd760f0c7825cc02a38ee71d22.
The commit changes no runtime source from those tests; added verification and
this evidence directory are excluded by the existing source-inventory contract.
The old dirty parent receipts are preserved, not relabeled as clean commit runs.
New frozen-commit CI must be read independently; no success is predeclared.

The only SQL change is inside recover_started: remove its unsupported
started→canceled inference. No migration1–7, role, ACL, column, function signature,
HTTP result, physical release, budget or target/network permission changes.
The full recovery manifest contains22tests/44mode-cases including the two new
actual-database provenance-collision tests; no old test was removed or skipped.

B's original IPv620s timeout remains unresolved. Its original PR failure,
Push cancellation, raw log/route/namespace/receiver records and prior negative
risks are preserved in task73-ci-blocker and task73-review2. No fixture B fix,
longer timeout, skip or automatic retry is included. One new Push and PR event
is explicitly authorized for this new A commit; not a retry-to-green loop.

Progress31/47,7.3unchecked, no7.4; PR11 Open, production hard closed.
The preceding uncommitted/stopped passages are dated history, superseded only
for this explicitly authorized commit/CI action, not erased or rewritten.

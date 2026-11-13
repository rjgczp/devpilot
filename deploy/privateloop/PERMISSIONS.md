# PR23 exact, inactive permission/Gate candidate

Candidate: `private-mode.sql.candidate`, SHA256
`20a4c1b1f7ae9f968984d8c12aba80f3e52a5d85a126c96c21bd953aa9c18a0a`.
This is a proposed future Migration14, outside `migrations/`; only the explicitly approved disposable test runner applies
it after verifying the approval hash. Migration1–13 remain unchanged and11 remains absent.

- Add `scheduler_control.execution_mode`, default `fixture`, constrained to
  `fixture/private-isolated`. Only the existing trusted administrator provisions
  modes. No runtime table/column UPDATE or SELECT privilege is granted.
- Add the actual `private-isolated` Gate enum value. Merely applying the candidate
  leaves Gate closed and all existing mode defaults unchanged.
- Add internal `private_gate_allows(text,uuid)`: owner-only, no runtime EXECUTE.
  It requires Gate to match the registered incarnation mode. A private identity
  cannot execute under `fixture`, including after a Gate change/race.
- Add `private_worker_ready(uuid)`: boolean only, restricted to the caller's
  existing registered Worker identity. **Only new runtime grant:** EXECUTE on
  this function to existing `devpilot_scheduler_worker`.
- Replace exactly one Gate predicate in each of ten existing functions, listed
  in `private-mode-diff.json`. Original bodies, lock order, budgets, fences,
  Owner checks, physical-proof checks and ACLs otherwise stay unchanged. Result
  settlement remains available with Gate closed; only future rearming is gated.
- No new role/LOGIN, generic SQL API, Worker/Supervisor role membership or DML
  permission. Tests reuse original disposable fixture LOGINs; the production
  PR21 LOGIN candidate remains unapplied.

Why needed: current constraints/functions recognize only closed/fixture; using
fixture for private execution is forbidden. Worker has no direct control-table
read, so the new boolean function attests real mode without exposing those rows.

Requested validation authority is **only disposable PG18/Linux**, locally and
in CI, on the existing network-none/veth fixture with no upstream. It includes
applying this candidate and explicitly provisioning private-isolated mode/Gate
for two restricted Workers and the original trusted control identities. No
production migration, real egress, deployment or new LOGIN is authorized.
Independent product approval was recorded for this exact hash on2026-11-13,
limited to disposable no-upstream validation. Production activation is prohibited.

P1 correction from the first isolated application: authorize/execution_status
are owned by devpilot_scheduler_api_owner and cannot call the owner-only helper.
Those two predicates are now inlined using that owner's existing SELECT on
scheduler_control. This adds no GRANT or caller authority; all other changes are
identical. The original approved candidate/failure is retained outside Git.
The corrected hash is independently approved and recorded in isolated-test-approval.json.

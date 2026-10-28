# Task5.4 original permission approval provenance

This evidence-only addendum follows independent technical acceptance of implementation
`e3a3d03b2a319cfe3c8c09f00327b7ae273f8eb1`. It changes no implementation or permission.

## Original source and exact reply

The original human approval exists in Codex Desktop session
`01a1571b-9759-7f22-957a-cc243a16a505`, local source file:

```text
/Users/sansea/.codex/sessions/2026/10/22/rollout-2026-10-22T14-39-40-01a1571b-9759-7f22-957a-cc243a16a505_01a16256-3967-7770-8c1f-30c176009d2f.jsonl
```

| Original line | UTC timestamp | Record |
| --- | --- | --- |
| 1329 | 2026-10-22T08:29:06.785Z | Exact request_user_input_async function call and arguments |
| 1330 | 2026-10-22T08:29:06.789Z | Corresponding question UI event |
| 1344 | 2026-10-22T08:31:23.321Z | Original role=user reply |
| 1345 | 2026-10-22T08:31:23.322Z | Corresponding UserMessage UI event |

The human replied at **2026-10-22 16:31:23.321 Asia/Shanghai**:

> 批准仅上述3项能力及信任边界

The request call ID is `call_05046c585a5b4bf38297c2b0e73a348d`.
The reply questionItemId is
`["request_user_input_async","call_05046c585a5b4bf38297c2b0e73a348d",0]`.
The original role=user message ID is
`msg_01a162bc-80b9-7300-8ef8-b142a19bed0b`.
Parsing the raw reply confirms its quoted question equals the original request
title exactly; the explicit answer matches the approved option. A selected
option without this submitted user message would not count as approval.

[original-records.jsonl](original-records.jsonl) contains only these four
unmodified JSONL records with their original newline bytes, not the full
session, compacted replacement history or an assistant-written summary.
[manifest.json](manifest.json) provides each source line, timestamp, SHA256,
excerpt hash and source-prefix-through-line1345 hash. The source log may later
append records; this prefix binding does not depend on its eventual whole-file
hash. The excerpt and hashes support reproducible review, but are not an
externally signed approval or independent human identity authentication.
`accepted=true` in the intervening tool output only acknowledges delivery; it
is not an approval and is not the basis of this record.

## Exact approved scope

All return `jsonb`, use the existing non-LOGIN function owner, fixed search_path
and REVOKE PUBLIC. Sole **runtime** EXECUTE grantees:

| Function signature | Role | Scope |
| --- | --- | --- |
| scheduler_private.quarantine_physical(uuid,uuid,uuid,uuid,uuid,bigint) | devpilot_scheduler_controller | Quarantine an existing prepared Run, never release capacity |
| scheduler_private.accept_termination(uuid,uuid,uuid,uuid,uuid,bytea) | devpilot_scheduler_supervisor | Consume a verified, durably committed physical receipt and atomically return held slots |
| scheduler_private.reconcile_termination(uuid,uuid,uuid,uuid,uuid,uuid,bytea) | devpilot_scheduler_supervisor | Query an uncertain Commit by full scope and fixed receipt ID/digest; no repeat consumption |

The approved question expressly states that the independent trusted Supervisor
adapter verifies Ed25519 and matching committed network/PIDFD/retirement proof
**before opening the database transaction**. PL/pgSQL itself does not verify
Ed25519. The database trusts the bound Supervisor SQL identity and checks the
frozen public-key ID, complete lineage and single-consumption ledger. This is
not protection against compromise of that trusted SQL identity. No additional
role attributes, table/column/secret permissions or other functions are granted.
This approval is specific to task5.4, not borrowed from Checkpoint2/3 or task6.5.

## Preserved acceptance boundaries and failure evidence

The review found no need to change the implementation. Physical slot return
sets resource/physical-stop accounting only; it does not complete the HTTP
attempt, fabricate success/failure, write current_observation or schedule a new
cycle. A started attempt can still block its Monitor under the existing index.
Task5.4 stays checked, task6.3 stays unchecked, overall progress stays **28/47**.

The original SIGCONT-only management-confirmation timeout remains unresolved at
the exact syscall/wait level. Network revocation without PIDFD/retirement proof
did not authorize receipt consumption or release. Preserve all original failure
logs, the disproved signal hypothesis and later diagnostic evidence in
[release54-development](../release54-development/), especially
`management-timeout-normal-command.log.gz`,
`management-timeout-normal-run-0-lifecycle.stderr.gz`,
`management-timeout-normal-postgres-final-ledger.log.gz`,
`management-timeout-result.json.gz` and that directory's `manifest.json`.
Later successful explicit-stop tests do not establish the exact first cause or
prove absence of concurrency defects. No risk record or raw failure is removed.

PR11 remains Open. No merge, Archive, deployment, production migration or
production target-network permission is authorized. Stop for final review.

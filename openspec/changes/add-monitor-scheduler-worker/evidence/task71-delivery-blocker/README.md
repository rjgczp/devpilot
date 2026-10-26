# Documentation HEAD Budget CI blocker — approved fixture repair

Remote HEAD ef689e82c11d04e695ca10fb9b55d079f7db7492 is documentation-only
relative to the fully successful f30dcdd implementation. Its Push40278790796
execution-security failed TestBudgetGlobalRateConcurrentStarts in race-4;
normal23 passed and race stopped after this failure. Later safety/result steps
were skipped by fail-fast. Never report this SHA as fully green. Original ZIP
Artifact12632200133 SHA256596d64a6f7ff96f7a9580900b36f16210cc059a34fc0894b893ccc7dbcd73fec
is retained remotely and copied under /private/tmp/devpilot-result71/final-budget-blocker/.

Actual9accepted/0refused, accepted DB span199.096ms exceeded the required125ms
burst prerequisite. Initial8tokens +1.592768refill -9Start =0.592768remaining,
exactly the actual ledger592768microtokens. All9physical slots stayed held;
no result/termination/release was manufactured. This sample does not prove a
rate overspend, but the strict short-window fixture did fail. The151ms gap
while waiters blocked behind transaction898 is observed; why that transaction
held its lock so long is not conclusively proven. No timeout, permission or
assertion was relaxed; no workflow/test rerun was requested.

The human user separately approved only this historical5.2fixture repair and
real PG18 normal/race regression: exact reply `授权最小fixture修复及回归`,
question call `call_c110ff45c95e484f99d0627d8fb83161`. See authorization.json.
The proposed diff was applied without changing production Start/ACK semantics,
permissions, assertions or budgets. Nine independent real READ COMMITTED
single-statement autocommit connections remove the client-side explicit Commit
round trip from this one database burst measurement. The original live lock
snapshot remains captured while blocked; output occurs after barrier Commit.

Directed real PG18 normal/race both exit0: accepted8/refused1, accepted spans
39.876ms and40.396ms. No Probe or target TCP was launched. Raw source/binary/log
hashes, actual commands, locks and ledger are in directed/ and the compressed
record receipts. These local tests correctly record dirty HEAD ef689e8 plus
source inventory9d2aa5bc…; they are not final clean-SHA CI evidence. Complete
23normal/23race also passed (46/46, no skips); command/log/source/binary binding
and raw lock/ledger/PostgreSQL logs are retained in budget-normal/ and
budget-race/. See regression-summary.json. Final frozen Push/PR CI remains
required before delivery can be reported green.

All earlier actual passes and failures remain valid at their exact source
versions. ef689e8 PR40278797102 later completed all3 jobs successfully; its
separate Push40278790796 failed and remains a failure. Do not average events or
substitute an older green SHA for a failing current delivery gate.

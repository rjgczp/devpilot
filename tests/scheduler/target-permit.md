# Task 6.3: default-deny hook and one-attempt target protocol

Scope: current product-owner approval following Checkpoint4 review at
`0fd6502f30c0882ae083773feb7f24b8fe41a524`. This batch implements only the
independently testable portion of 6.3. **6.3 remains unchecked**, as do 5.4,
6.4 and 6.5. Production execution remains hard closed.

The existing private HTTP engine performs its original bounded A/AAAA/CNAME
validation and whole-answer policy before selecting `ips[0]`. Its sole literal
TCP call now passes a private `targetPermitHook`. A nil hook rejects. The hook
cannot replace the selected address or resolver. It runs inside the original
probe timeout; the original TCP3s/TLS3s/header5s/byte ceilings remain. No callback
retry, alternate address, redirect, proxy, reconnect, or allow cache is added.
Permit refusal returns `nil Result, target_permit_unavailable`, not a fabricated
target HTTP failure. A separate atomic phase marker handles Transport returning
on cancellation while the dial callback is still waiting for permission.

The fixture-only input bridge binds the candidate to its sealed configuration
and sends a fixed **2048-byte** request through an anonymous pipe. It includes
full W/P/M/run/v/g/epoch/fence and namespace/Supervisor incarnations, original
host, scheme/port, selected literal IP, and the earlier of the original probe
and immutable physical permit deadlines. It contains no URL path, DB secret,
HMAC key or Cookie. A second anonymous pipe carries exactly one equally bounded
reply. Both require EOF, canonical strict JSON, valid lineage/deadline and exact
request binding; truncation, extra bytes, unknown/duplicate keys, invalid Unicode,
missing/expired/mismatched reply and channel failure deny. Local pipe operations
have a maximum2s budget further reduced by the probe deadline; cancellation
interrupts blocked reads/writes. This bounded wait may refuse slow DNS/control
plane fixtures; it never expands target permission.

A control-plane `TargetController` can only be derived from the process-local
confirmed `StartSnapshot`. Copies/new controllers share that Start's atomic
one-attempt bit, burned even on rejection or unknown Commit. Start retains its
already-validated original Claim Lease timestamps (not returned by start_run),
without changing SQL or allowing snapshot reconstruction. The request must
match the frozen origin, complete lineage and immutable deadline. The trusted
fixture directory derives HMAC keys entirely in control-plane memory. Only the
newest generation is tried; no old-key fallback or second candidate request.

The controller calls the **existing** Worker `reserve_target` restricted
VOLATILE function. Its parent-first locks, fresh final Owner/v/g/epoch/expiry/
Lease/Fence/stop checks, and origin/IP reservations are unchanged. No migrations,
ACLs, functions or role grants are added. Unknown Commit gives no reply and never
calls an installer, even when the real database committed. The connection has
returned to the pool before any external management call.

Only after confirmed reservation does `TargetSupervisor.InstallTarget` run.
The reply must bind the exact request, reservation permit UUID and deadline.
There is deliberately **no concrete installer, executable scheduler Probe or
new public execution method** in this batch. Missing installer, disconnect,
unknown installation acknowledgement, wrong lineage/literal/host/ID/deadline or
expired acknowledgement denies. The interface is the future independent trusted
management channel, not Worker-supplied proof and not cryptographic authentication
of arbitrary messages. Fixture protocol witnesses in tests only inspect call
ordering/binding; they never install rules or prove physical stop.

No database connection spans management/pipe waits. Failed/unknown reservations
remain held; tests neither synthesize termination receipts nor write release
fields. Destruction of the guarded disposable PG cluster is teardown only.

## Acceptance and remaining dependencies

- Linux offline unit/race: exact hook gate, original deadline, request binding,
  anonymous pipes, one consumer, malformed replies and no redaction leakage.
- Guarded PG18 normal/race: real Commit/lost ACK/rollback, absent Supervisor,
  mismatched/disconnected/late acknowledgement and a real origin-lock barrier
  crossing request expiry; raw locks/timelines/SQLSTATE and held ledger captured.
- Existing isolated `tests/execution/run-docker.sh`: original76 kernel security
  cases plus5 denial cases, each with real DNS and zero target connect attempts
  for the5 denials. The pre-existing test-only allow hook is explicitly local to
  the historical network-none fixture and is **not** a scheduler permit. These
  denial controls simulate callback outcomes; real PG Commit uncertainty is
  separately tested with real transactions, not claimed as one network topology.

Full scheduler acceptance still requires6.4 default-deny namespace/veth rules,
trusted independent Supervisor rule-install acknowledgements and uncredentialed
network-capable Probe IPC, then6.5 physical stop. The existing input-only Probe
continues to deny every socket via seccomp and is unchanged. No full permitted
scheduler GET, independent physical stop, periodic cycle or safe release is
claimed here. Those dependencies prevent checking6.3 despite the allowed
protocol/default-refusal work and original-kernel regressions passing.

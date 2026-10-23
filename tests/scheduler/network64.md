# Task6.4 fixture network boundary

This is only the approved plan A test component, excluded from ordinary Linux
builds by `schedulerfixture && linux`. The new command has no socket listener,
HTTP client, DNS resolver, production option, environment unlock, or DB account.
The API dependency graph excludes the management package. Tasks6.3/5.4/6.5 are
not completed by this component.

## Allocation and independent policy

The trusted outer allocator uses Docker only to create disposable network-none
namespaces, cgroups and read-only filesystems. It passes no Docker socket or
source/config volume to the unprivileged child. NET_ADMIN management containers
create fresh point-to-point veth pairs. One peer is moved via NETLINK into the
owned Run namespace; another joins the independent receiver namespace. A short
allocator helper shares only those disposable PID/network namespaces to move
the second peer. No host PID/network namespace, privileged mode or SYS_ADMIN is
used. The child has its own PID/mount/cgroup namespace and shares only Run netns.

Each Run has its own namespace and forward guard:

```
unprivileged child -> Run eth0 -- veth -- guard eth0
                                         guard eth1 -- veth -- receiver rxN
```

There is no Docker bridge, host interface, default route, real target, upstream
resolver, NAT, tunnel, PG path or management listener. Receiver public-shaped
addresses are bound only to loopback in the isolated witness namespace. Private,
metadata, internal-public-shaped, IPv4-mapped and translation addresses exist
only as hostile fixture destinations. Static neighbors and explicitly disabled
link-local autogeneration avoid an ICMPv6/RA/multicast exception. IPv6 stays on.
Only exact host routes through the guard are installed. The allocator records
Docker identities, namespace inodes, veth attributes, addresses, routes and peers.

Two independent nft `inet` tables have DROP input/output/forward policies before
any child starts. Run output and guard forwarding accept only exact Resolver
192.0.2.53 / fd64:53::53 TCP53, plus one timed IP/TCP80-or443 target tuple. Return
rules use the same expiring tuple; no broad ESTABLISHED rule exists. A first-SYN
one/day burst marks only the first target conntrack entry; later connections
cannot borrow that mark. The refill interval exceeds the <=35s capability life.
DNS is not a target exception and UDP/default Docker DNS are denied.
Resolver elements also expire at the immutable namespace hard deadline; they
cannot keep a disconnected or zombie child's DNS capability alive indefinitely.

The management process checks exact NET_ADMIN-only capabilities/no_new_privs,
the expected namespace, veth-only active interfaces, no default/encapsulated
route, address/route hashes and the full functional rule hash. Counter values
and timed target elements are excluded from the functional hash; before install
those target sets must be empty. Live element expiries are checked separately against the
hard deadline, so normalizing display TTLs does not hide permission extension.
Fixed fixture IPs and host are compiled into
the fixture boundary. Bounded/strict JSON rejects duplicates, unknown keys,
trailing input, invalid Unicode and excessive frames. No raw tool error is logged.

## Immutable capability

The trusted bootstrap binds complete scope/run/v/g/epoch/fence, Supervisor and
namespace incarnations, actual namespace inode, route/address/rule hashes and
a fixed hard deadline. A private exclusive bootstrap ledger in management tmpfs
burns that namespace's bootstrap even if its rule table is lost. Probe shares
neither that filesystem nor the management process's PID namespace. Failed
installation also consumes the operation; restart/reconnect cannot renew it.

Installation follows the existing confirmed target budget Commit. It checks the
full expected lineage, original fixture host, selected literal IP, port, permit
ID and bounds. The management channel is trusted IPC, not a self-authenticating
signature or a new DB authority. The existing Worker TargetController checks
and its parent-lock/Fence/budget protocol are unchanged.

Kernel nft timeouts are relative to insertion. The implementation subtracts
the entire bounded2s command interval before installation and floors to
milliseconds, so it only reduces the lifetime. Too little time fails closed.
Both the original wall deadline and a process-local monotonic deadline constrain
installation; no heartbeat/renew operation exists. Evidence derives a
conservative expiry upper bound from actual nft millisecond output and a later
Linux clock sample. Management EOF does not renew permissions; kernel expiry
works after the process exits.

## Actual acceptance and commands

On a Linux runner with pinned Go1.24.3/Goose3.26.0 and Docker:

```sh
docker build -t devpilot-scheduler-fixture -f tests/scheduler/Dockerfile .
NETWORK64_TOOLS=/absolute/fresh/tools sh tests/scheduler/build-network64.sh
python3 tests/scheduler/run-network64.py --tools /absolute/fresh/tools --evidence /absolute/fresh/evidence
```

The native normal/race binaries are required, build metadata is checked, and any
missing dependency/IPv6/capability, test failure or missing confirmation fails.
There is no skip/retry or simulated expiry path. Existing execution-security CI
runs this task-specific acceptance after the unchanged76+5 and historical
Snapshot suites, uploading raw evidence. It does not complete task10.1's later
dedicated scheduler-security job.

The adversarial child is an independent test witness, not a replacement HTTP
executor. It deliberately makes raw socket attempts to prove independent deny.
Allowed controls perform actual TCP53, HTTP80 and certificate/SNI-verified TLS443
against the receiver. Malicious attempts have real strace attempts and zero
receiver arrivals; connect failures are never described as zero attempts.
Tests include per-Run isolation, second connection/permit rejection, scope/Run/
Fence/host/IP/expiry mismatches, admin/userns/raw-socket denials, IPv6 escapes,
Supervisor loss, local/guard table loss, restart, default-route drift and actual
post-DNAT IPv4/IPv6 destination blocking.

`TestBudgetNetworkPolicyPG` runs only with the explicit `networkfixture` tag and
an external allocator. It uses a new guarded PG18 cluster, existing Supervisor
prepare/consume capabilities, real Claim/Start/target commits and existing
HMAC catalog/budget functions. Prepared proof hashes refer to actual verified
Linux namespaces. Normal uses IPv4 TLS and race uses IPv6 TLS. It verifies no
business DB connection remains across installation, a second target capability
fails, and the physical reservation remains held. No table/column/EXECUTE/role
attribute/Migration change is introduced. A missing external channel fails.

## Deliberate remaining dependencies

The original DNS/IP/TLS/HTTP kernel and input-only no-socket Probe seal remain
unchanged. Running that actual scheduler Probe in this topology requires the
later combined6.3/6.5 acceptance; this test actor is not that proof. Supervisor
physical kill, existing-flow emergency stop, signed termination/irreversible
node recovery, quarantine release, results and periodic cycles remain unaccepted.
Deleting a local table is independently contained by the forward guard; missing
guard policy refuses new installation. This is not a guarantee against an
administrator simultaneously removing every trusted enforcement layer. Broader
drift/emergency continuity belongs to6.5; no polling-based physical-stop claim.

Fixture container destruction is teardown only. PG evidence retains started
runs, reserved physical slots and NULL termination receipts; Goose Down is
expected to refuse those rows. The disposable cluster is then destroyed, not
updated to manufacture release. Production networking remains unverified and
hard closed; PR11 stays Open for independent review.

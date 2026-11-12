# Private HTTPS boundary (execution stays closed)

PR22 provides two separately built Linux executables: `privateprobe` (unprivileged
single-use HTTP engine) and `privatenetwork` (trusted namespace policy installer).
The shared `internal/probenetwork` implementation is extracted from the reviewed
fixture, including its unchanged network revoke checks; the old fixture adapter
retains its original fixed host/IP restrictions. Ordinary API and private Worker
builds contain neither network administration nor a Probe launcher.

This is not an activated private scheduler. The Worker stays closed, there is
no new DB Gate, and no role, GRANT, SQL function or migration is added/applied.
The PR21 LOGIN candidate still needs separate approval. A private DB Gate and
trusted deployment/target catalog must be reviewed before future activation.
Fixture DB authorization is used only in disposable test clusters, never as a
private production Gate.

## Build and isolated demonstration

On Linux with Go1.24.3:

```sh
CGO_ENABLED=0 go build -tags=privateprobe -o privateprobe ./cmd/privateprobe
CGO_ENABLED=0 go build -tags=privateprobe -o privatenetwork ./cmd/privatenetwork
```

With Docker, Python3 and OpenSSL3 (ED25519), run the bounded demonstration:

```sh
sh tests/privateprobe/run.sh /absolute/new/output-directory
```

It creates only disposable, network-none namespaces and fresh point-to-point
veth links to the existing dual-stack receiver. There is no upstream, bridge,
host network, published port or Docker socket in children. Public-looking
addresses on that wire are local aliases, not Internet traffic. Tests use the
original real Owner/Start/target-budget and signed physical-stop PG18 protocol.
Only the allow cases send a GET; missing Permit, management loss, DNS rebinding,
poisoned IPv6 answers and invalid TLS are tested without fallback or retry.
The original fixture's HTTPS path is separately checked once.

## Minimum trusted Linux launch contract

An offline trusted allocator must create a fresh per-Run PID/network namespace
and independent forwarding guard first. No default route, unknown interface,
NAT, upstream or host network is accepted. `default-deny.nft` is a standalone closed baseline for that namespace only; never
apply it on the host. Do not preapply it before Boot: the manager requires a
pristine namespace, atomically installs the same drop policies, and adds only
short-lived resolver exceptions. The concrete manager installs
and audits the existing three drop-policy hooks and expiring resolver/target
sets. Each Run has a maximum35s immutable physical Permit; all timed exceptions
expire conservatively before its deadline, even if the manager is SIGKILLed.

`privatenetwork fingerprint` reads the actual namespace routes/addresses.
The trusted allocator supplies its namespace ID and fingerprint, full lineage,
role (`run` or `gateway`), original probe addresses, HTTPS host, **one selected
public literal IP**, deadline, and explicit host/control-plane denied prefixes.
This configuration goes on the manager's private stdin, not from Probe. The
manager has exactly NET_ADMIN, no DB/cloud credentials, no listener, and can only
operate inside its bound namespace. Install additionally requires the existing
committed target reservation UUID/time/deadline and exact complete request.
It rechecks live topology/rules and applies a single consumed SYN burst. EOF or
error attempts to flush all exceptions within2s; failure yields no release proof,
and kernel expiry remains the independent backstop, including SIGKILL.

Probe receives no arguments/configuration environment, secrets or management
channel. The trusted parent delivers the existing sealed input/receipt on FD3/4,
one bounded Target Permit request/reply on FD5/6, and a **read-only anonymous
lifetime pipe on FD7**. Only the independent PID1 Supervisor holds its writer.
Probe accepts UID/GID65534, no groups/capabilities, NNP, read-only root,
128MiB/CPU1/PID128/FD256 and the existing all-thread seccomp restrictions.
Its environment must be exactly LANG=C/TZ=UTC. The PID1 owner must stay untraced;
its death kills the Probe PID namespace. The pipe is not a release receipt.

The original engine validates the whole A/AAAA/CNAME result and chooses one
literal; the same exact IP/host/HTTPS443 must match both committed reservation
and independently installed policy. The existing pinned address registry rejects
private, link-local, metadata, mapped/translation and restricted space in both
families. Explicit host/control exclusions only subtract permission. It never
re-resolves for dialing, chooses another address, follows redirects, uses a
proxy or retries. TLS uses the controlled image's normal trust store.

The lifetime pipe is checked before and after the single Permit round trip,
immediately before the original literal dial. Lost/invalid pipe, missing/late/
mismatched confirmation or expired Permit denies. Instantaneous distributed
failure detection is not claimed: after the final check, the independent PID1
lifetime and kernel deadline bound the race. No signal/log, EOF, expiry, failed
GET or container deletion returns a DB slot. Only the unchanged independent
network revoke, pidfd/reap, namespace retirement and signed proof can settle it.

## Remaining activation work

Private Gate/identity permissions, an actual trusted allocator/PID1 deployment,
default-denied routed Internet egress, concrete host/control exclusions and
operator-approved real target binding remain separate review items. No public
route is created here. Retention/storage admission remains unimplemented;
Task8.3 stress and Migration11 remain deferred. OpenSpec stays37/47. This PR
performs no deployment, production migration, permission activation or Archive.

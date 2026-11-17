# Operator-approved HTTPS admission (closed by default)

This adds runnable admission for the existing private service. Monitor URL and
finite Owner consent remain unchanged. A Web user cannot approve their own URL.
An operator separately reviews DNS and creates one exact Monitor/configuration
approval; approval is necessary in addition to Owner authorization. Existing
Result/status/history APIs remain the only observation path.

## Review and approve

Build the existing native Linux image with `sh scripts/private-service.sh build`.
It includes a small operator-only `/toolbin/privateapproval` tool. `review`
performs only bounded TCP DNS to the existing controlled `192.0.2.53:53` wire,
using the exact Probe resolver, full A+AAAA traversal, CNAME consistency and
pinned address policy. Run it only in an independently isolated review namespace;
this command creates no route/firewall, target GET or SQL permission. Its resolver
address is deliberately not a user-configurable Internet DNS endpoint.

Create a root/operator-owned request JSON file (all UUIDs and version from the
existing authenticated Monitor API, URL copied exactly):

```json
{
  "approval": {
    "workspace_id": "<uuid>", "project_id": "<uuid>", "monitor_id": "<uuid>",
    "configuration_version": "<decimal>",
    "url": "https://operator-approved.example.com/check",
    "expires_at": "<explicit UTC expiry, at most 24 hours>"
  },
  "denied_prefixes": ["<actual host/control-plane/VPN addresses as CIDRs>"]
}
```

In that namespace, with only this request mounted read-only:

```sh
/toolbin/privateapproval review --request-file /run/request.json
```

Inspect the returned full `records`, `addresses`, `host` and timestamp. Add it as
`review` in the request. **Explicit operator action**, within30s of the review:

```sh
/toolbin/privateapproval approve --request-file /run/request.json
```

This offline step emits an approval, never enables SQL or routes. It binds W/P/M,
configuration version, full URL, complete DNS record digest, all validated A/AAAA
addresses and an explicit expiry≤24h. Digests include CNAME and every received
address RR, including authority/additional records. RR order/duplicates/cache
TTL changes are normalized; content, alias or address changes require reapproval.
Every Run resolves and validates both families again before asking for a Permit. New Claims also require enough approval time for the existing45s Lease; this only subtracts admission, never extends that Lease.
No cached DNS result authorizes a dial; the engine connects once to its selected
literal (no fallback), retaining TLS hostname verification. TTLs are not used to
renew approvals or Permit lifetime. An approval cannot extend the original
≤35s Permit, ≤10s example check or SQL2s/lock250ms budgets.

Put the emitted object in:

- a read-only `approval_file` for the trusted launcher, with no HMAC key;
- the existing Worker catalog as `approvals: [<object>]`, **instead of** `targets`.
  Keep the existing `generation` and `key` fields. The key remains Worker-only;
  neither Probe nor network manager receives it.

Register the exact origin/IP keys using the **existing** Controller target-budget
rotation path. This PR grants no new catalog/registration privilege and does not
perform that action on a real deployment. Expiry is intentionally fail-closed;
manual reapproval plus a normal bounded service restart is required. No auto
refresh daemon or wider hostname wildcard is introduced.

## Runtime wiring

Copy the existing private-service config, use `mode: "private-approved"`, add
`approval_file` and `denied_prefixes` with actual v4/v6 host/control exclusions,
and set `target_host`/`target_ip` to the approved host and selected validated
literal. Other identity, immutable image ID, PG/storage and journal requirements
remain. Start explicitly:

```sh
sh scripts/private-service.sh start --config /secure/service.json \
  --state /secure/state --run-approved
sh scripts/private-service.sh stop --state /secure/state
```

Without the explicit switch the entry remains closed. `--run-isolated` cannot
activate this mode. The existing physical Supervisor is reused; DB return of
capacity still requires original independently verified termination proofs.
Recovery-required domains cannot be erased or restarted into a new GET.

The trusted allocator still accepts **only an owned network-none target
namespace with no default v4/v6 route or published port**. It creates fresh
point-to-point links through a separate default-drop gateway. This is executable
controlled-network validation, **not a live Internet deployment adapter**. Only
the selected approved literal443 and fixed resolver53 receive host routes; there
is no host default route, NAT, proxy or general forwarding service. Both run and
gateway independently check lineage/host/literal/DNS digest/expiry before
installing timed nft elements. Probe has no DB/key/NET_ADMIN/control channel.
Full DNS validation also uses the operator exclusions. Approval, Permit or
Supervisor loss refuses new execution; unknown remains unknown and restart
never resends it. Pause, Result, physical release and recovery keep their
existing separate paths.

## Validation and pending authority

Native Linux: `sh tests/targetadmission/run.sh /absolute/new-evidence-directory`.
This uses owned DNS/CNAME and HTTPS with an owned test CA on no-upstream
namespaces, actual PG18, two native Workers and original Supervisor. It checks
three60s cycles including Supervisor-loss/unknown/restart and real history;
separate fresh disposable clusters check unapproved target, approval expiry,
changed DNS and private/metadata rejection in both families. Directed kernel
expiry checks use synthetic management proofs, never physical DB release claims.

M16 candidate: `approved-mode.sql.candidate`; exact source and privilege delta:
`PERMISSIONS.md`, `permission-diff.json`. Application is blocked unless a
separate hash-bound isolated approval exists. M1–10/12/13 and M14/M15 candidate
bytes are unchanged; Migration11 stays absent. No production application or
Gate opening is authorized by this PR.

Remaining live-test blockers (at most3):

1. **P0 authority:** production M14→M15→M16, existing five-LOGIN proposal and
   concrete identity/catalog/storage/key registration need separate approval.
   The sole new M16 runtime GRANT is Worker EXECUTE on its own readiness boolean.
2. **P0 network integration/configuration:** a real dedicated Linux upstream
   namespace and trusted DNS service must be bound to explicit routes and the
   reviewed default-deny policy; the owned no-upstream adapter cannot silently
   switch to the host public route. Actual Internet opening remains forbidden.
3. **P1 host acceptance:** real VPN/HTTPS certificate, firewall, persistent
   journal/storage and key-file access boundaries remain unverified.

OpenSpec37/47 unchanged;8.3/11 and later unaccepted tasks remain deferred. Original
125ms/250ms/55P03 timing, IPv6 and management-confirmation unknown risks and all
negative artifacts remain; this work makes no historical root-cause repair claim.

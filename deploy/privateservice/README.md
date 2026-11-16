# Private service launcher (Linux, no upstream)

This entry replaces per-Run test coordination with restricted service binaries.
It is **not a public/private Internet deployment**. Execution is closed by default;
this revision accepts only the existing operator-owned, no-upstream HTTPS target
`a.fixture.example.com` at `93.184.216.34` or `2001:4860:ffff::34`.
It cannot admit a Dashboard user's arbitrary URL or add a public route.

## Start and stop

Requirements: native Linux with pidfd, Docker Engine, Python3, OpenSSL ED25519,
existing internal dual-stack PG18 control network and existing network-none test
target (only loopback initially). The launcher user is trusted to administer
Docker. Root journal/signing authority stays in this parent, outside children.

```sh
sh scripts/private-service.sh build
# Save the printed immutable sha256 image ID in service.json.
sh scripts/private-service.sh init --state /absolute/private-state
sh scripts/private-service.sh start --state /absolute/private-state # closed
sh scripts/private-service.sh start --run-isolated \
  --config /absolute/service.json --state /absolute/private-state
sh scripts/private-service.sh status --state /absolute/private-state
sh scripts/private-service.sh stop --state /absolute/private-state
```

`start` returns only after **both** Workers complete validated startup. `stop`
closes admission first; status changes to `stopped` after bounded draining of the
existing45s Lease and physical stop. It does not extend any SQL2s/lock250ms budget.
`run` supports foreground supervision by an existing Linux service manager;
there is no automatic restart policy or generic management socket.

Copy `service.example.json`, use absolute paths, chmod config0600 and journal0700,
and keep each secret in an operator-owned private directory. Each credential
file must be readable by its bound container UID only (a read-only mounted file
owned by that UID with0400, or0444 beneath a host0700 directory). No secret is
passed through argv/environment or copied into the image. Keep the journal and
key stable across restarts; never delete it to make an unknown Run eligible.

The **offline approved operator** must have already registered the two Worker
LOGINs/incarnations, Supervisor, Controller and Reporter; pinned `public.pem`;
bound the catalog and real storage device/FSID/postmaster; and authorized the
Monitor through the existing Owner API. This launcher creates no role, GRANT,
schema, registration, authorization or Gate. M14/M15 candidates retain their
prior disposable-only approval; they are not production migrations.

Supported storage topology is PG18 default named volume `/var/lib/postgresql`,
PGDATA `/var/lib/postgresql/18/docker`, WAL/base/default temp on that device,
no external tablespace/archive/replication slot, stderr Docker json-file/local
logs. The offline binding must verify these facts as in the lifecycle test.
`metadata_mounts` must exactly match the inspected PG named volume at `/storage`
and that container's actual log directory at `/logs-metadata`; arbitrary mounts
are rejected. Reporter uses O_PATH/fstatfs only, with content unreadable and
read-only mounts. Deduplicate device/FSID in `bindings_file` as in M15. This is
real filesystem occupancy, not lower-layer reservation or the deferred10GiB
pressure proof; insufficient/stale/missing storage remains closed.

## Identity and failure boundary

| Process | UID | Credential/capability |
| --- | --- | --- |
| Worker0 / Worker1 |65532 /65533|each own Worker LOGIN and catalog; no launcher/key/network authority|
| Supervisor pair |65531|existing Supervisor LOGIN, pinned public key; no signing key/Controller secret|
| Controller |65530|existing Controller LOGIN; recovery/retention, no signing/execution|
| Sampler / independent watchdog |65529|Reporter LOGIN only, sampler metadata mounts|
| Probe |65534|sealed single Run input, CA, one Permit channel and read-only lifetime pipe; no DB/control secret|
| Trusted parent / PID1 / network managers |host / separate namespaces|original stop/signing/journal and fixed NET_ADMIN operations; no DB credentials|

All control children are cap0, read-only, bounded containers on the internal
control bridge. IPC consists of inherited anonymous pipes with typed bounded
messages, no SQL transport or listener. Controller has two bound Supervisor
channels and a separate completion/recovery-only parent channel; Worker logs
cannot assert a DB result. That channel rechecks the existing SQL state before
marking a journal terminal. Physical acceptance and terminal recovery are
separate durable facts, so a lost Result ACK still reconciles on restart. Probe is only in its own network domain;
the existing per-Run network rules and lifetime pipe remain authoritative.

Supervisor/Reporter/Worker loss closes both Workers' admission. Existing Result
and stop paths can drain. No result/exit/flag/container deletion is physical
proof. Supervisor loss first leaves unknown and capacity held; restart can only
read the immutable signed stop bundle, independently verify it, commit the
existing termination function, then request existing Controller recovery. It
never reconstructs/replays the old input or GET. The observation relay can wait for the existing10s HTTP timeout within the
unchanged35s physical hard deadline; no SQL/lock/Lease/Permit budget grows.
Controller peer loss does not
cancel the other peer's ongoing physical/Result settlement.

If the trusted parent itself dies before a complete signed bundle exists,
restart reports `recovery_required` and stays closed. Reconciliation of such
unsigned domains requires an operator using the existing trusted physical-stop
procedure; automatic recovery of this case is not claimed. Do not delete
containers/journals or manufacture a release. Re-registration after credential
or incarnation expiry is an offline operator action.

## Directed verification

```sh
sh tests/privateservice/run.sh /absolute/new-evidence-directory
```

The native Linux test applies the **unchanged hash-approved** M14/M15 only in a
fresh disposable PG18. Test coordination provisions/observes resources, never
performs execution SQL/Permit/physical settlement. The actual service starts two
Workers, commits a real result, stops/restarts without replay, loses Supervisor
after a second GET, preserves unknown/unreleased capacity, verifies immutable
physical proof on restart, completes a future60s Run, and loses Reporter while
Pause/PATCH remain usable. Receiver GETs, API history, SQL timeline, stop bundles
and process status are retained. No105-cycle or unrelated historical matrix is
repeated. The Mac launcher intentionally refuses active mode; CI uses native
Linux. VPN/cert/firewall/host key ACL and real public egress remain unverified
and disabled. OpenSpec37/47,8.3 and production M14/M15 remain unchanged.

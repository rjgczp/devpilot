# Task6.3 tracing repair: failed, stopped, not submitted

Baseline HEAD `856cbbd45aa19297e4694f7b90b2b44261049b42`; dirty source inventory
`9008c5c7e580cc80f9875463479155a269bc30ac0fff70a2bf46ce61a88bd704`.

See [failure analysis](../../../../../tests/scheduler/joint63-blocker.md).
The separate original negative archive at ../joint63-negative/ is unchanged.

This archive includes actual commands, exits, source and tool hashes, image
metadata, receiver, /proc/TracerPid, strace, pidfd/signals/waitpid, firewall and
routes, signed physical proof and PG ledger. It also contains the topology-only
repair diff vs the saved pending draft, full draft diff vs HEAD, and current
baseline GitHub metadata. manifest.json hashes each archived member.
No private signing key or executable binary is included.

First normal HTTPS case failed on strict connect audit: two extra Python relay
AF_UNIX NSCD attempts. Physical proof/signature exists, but was not delivered to
DB consumer; quarantine and all capacity stay held, no HTTP result persisted.
Only 1/46 planned cases ran and failed. 45 unexecuted, no complete race/fault or
historical rerun claim. Static Change strict/diff checks do not imply runtime
acceptance. After failure only documentation/evidence packaging occurred.

Task6.3 remains unchecked,28/47. No commit/push/new CI; PR11 Open and production
execution hard disabled. All files remain pending independent review.

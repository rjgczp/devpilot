# Task6.3 NSCD repair: failed ninth case, stopped

HEAD remains `856cbbd45aa19297e4694f7b90b2b44261049b42`. No commit or push.
Tested dirty source inventory:
`e7ef2bb27d920355b3124834900d29f63f0795a6a063412b7b31724d1329c3ae`.

See [full analysis](../../../../../tests/scheduler/joint63-blocker.md) and
[narrow repair diff](nscd-only.diff). The archive includes actual command/exit,
source/tool/image hashes, Python stack/operation/glibc attribution controls, all
nine PG18/Linux cases, /proc/strace/pidfd/signal/wait, receiver, network rules,
physical signature and final PG resource ledgers. manifest.json hashes members;
review-evidence.tar.gz.sha256 verifies the archive. No private signing key.

NSCD source regression: four real controls passed, fixed startup zero sockets.
Joint matrix: 8 normal cases passed, ninth permit-missing failed on host log
redactor base64(None), then stopped. 37 cases unexecuted; no race matrix or full
historical rerun. Exact previous raw reply was not persisted by the throwing
redactor; the denial/nil source path and actual TypeError are retained.

Four confirmable cases safely returned physical capacity with real proofs; four
unconfirmed fault cases kept quarantine and global1, no receipt or release.
Failed ninth case stayed reserved/held/global1, no receipt or HTTP result; only
DNS accepted by receiver, no final child trace, so no full zero-target-syscall
claim. Teardown does not prove physical return. Task6.3 unchecked;28/47.

Earlier two negative archives and unresolved5.4 SIGCONT risk remain. Migration
1–8, DB capabilities, Go kernel and network audit whitelist unchanged. PR11 Open,
production hard closed, no group7, merge, archive or deploy. Static strict/diff
checks passed but cannot replace joint acceptance. Await independent review.

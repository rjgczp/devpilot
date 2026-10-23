# Task6.3 explicit denial repair — matrix passed, verification stopped

Status28/47,6.3 unchecked, uncommitted/unpushed. HEAD
`856cbbd45aa19297e4694f7b90b2b44261049b42`. Tested dirty source inventory
`c44586b33cc960267c2217567b34924d311aafa13d2d957d4b23879a0543bbcf`.

See [analysis](../../../../../tests/scheduler/joint63-blocker.md) and
[denial-only diff](denial-only.diff). All original failed archives are unchanged.

Passed:46/46 real joint normal/race;6.4/6.5/5.4;147/147 PG18 in each mode;
Go unit/race/vet/fuzz/classifiers;81 legacy76+5 network outcomes;unchanged API
zero-target/CGO guard;Change strict/diff check. Explicit null is denial via
zero-byte pipe close, never an empty/successful frame. Real permit-missing
normal/race IPC is preserved; each has DNS1/targetTCP0/GET0, actual stop proof
and safe return, no HTTP result written.

Stopped: supplementary planner wrongly demanded the terminationfixture-specific
TestBudgetPhysicalStopPG in joint-pg.test. Actual code/traceback and static binary
tag/symbol evidence are retained. No planner retry or further tests. Remaining
23 pure Budget cases per mode and four historical Snapshot cases per mode are
not completed. Do not make a completion application or substitute old CI.

Full raw logs, commands/exits, source/binary/image hashes, receiver, namespace/
firewall/route,proc/TracerPid,pidfd/signal/wait,signatures and PG ledgers:
`/private/tmp/devpilot-joint63-denial-review/real-evidence.tar.gz`.
Its SHA256 is in real-evidence.tar.gz.sha256; manifest.json hashes archived files.
Large raw archive remains local, not a GitHub Actions Artifact; no test binaries
or private signing keys are packaged. Raw failed planner stderr came from
functions tool chunk4f2a23, not record.py; original listing stdout was not saved.
Later static symbols are explicitly distinct evidence.

PR11 remainsOpen;baseline CI successes apply only to baseline856cbbd. No new CI,
DB grant/migration,group7,production execution,merge,archive or deployment.
Await independent review of the planner/remaining verification.

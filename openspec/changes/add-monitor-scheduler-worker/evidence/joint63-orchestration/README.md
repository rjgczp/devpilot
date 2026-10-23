# Supplemental Budget and historical Snapshot acceptance

The previous failure was a Python inventory assertion, not a Go compile error.
The original failed planner, tool stderr/exit provenance and exact tag metadata
remain in ../joint63-denial-review/ unchanged. No Go compiler stderr existed.

The base23 Budget functions are now compiled separately with exactly
`integration,schedulerfixture,budgetfixture`, normal and Race. The checked-in
budget-tests.json must exactly equal the actual compiled listing: no skip,
missing, extra or duplicate test can pass. Each test owns a fresh guarded PG18
network-none database,120s process cap, the original actual45s/60s waits, captured
ledger/locks/logs, and no artificial physical-release UPDATE before teardown.
The accepted networkfixture/terminationfixture/releasefixture/jointfixture
binaries remain separately built and exercised by their original runners.

All23 normal and23 Race Budget tests passed. All four historical Snapshot tests
passed normal and Race against their own fresh Schema6 databases, prepared by
native Goose3.26.0. The live test uses the original receiving-endpoint barrier,
network audit and no-upstream Probe. The local trusted Linux test controller
alone uses Docker CLI/socket to orchestrate the original child containers; that
socket/CLI/DB credentials are never mounted/passed into the Probe. In CI the
original native host controller remains unchanged.

An initial local host-controller orchestration assumed Docker internal-network
port publication. Docker returned an empty port mapping; the helper stopped
before schema preparation/test bodies. Its exact script, inspect, traceback
and receipt are retained. The corrected controller shares only its fresh PG18
network namespace; no public routes or host port are needed. This is an
orchestration repair, not a change to the original Snapshot assertions.

manifest.json lists every raw file and archive digest. The external local raw
archive omits copied ELF files, whose binary hashes remain in command receipts.
Fresh actual Actions artifacts on the eventual frozen commit provide complete
raw execution, PG18, audit, physical-stop and safe-release evidence; local dirty
passes are not attributed to a commit that did not yet exist. No new safety
runtime, database capability, role/ACL, production build entry or group7 work.

Original diff context and go-version build output contain whitespace that Git
interprets as source formatting errors. raw-packaging.json maps byte-identical
gzip copies to originals preserved locally and in the prior checksummed raw
archives. Decompression hashes are verified; no content was trimmed or altered.

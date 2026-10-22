# Authorized minimal registration deadline repair

The user explicitly approved the review-only minimal deadline gate repair and regression after the final evidence commit exposed a new failure. This authorization is additional to the CI-orchestration-only batch. It does not authorize scheduler6.4 or permission expansion.

## Observed failure (not retried)

HEAD `a5b49c16355418b8c9dcf63e72907702961b6eba`, source inventory `1956bbc2c6b2ad25504c5d6fe5dae5df42ff6cb101cbc10a32e1965d12fe2228`:
- Push39709818391 integration failed at historical TestServerDeadlineAndUnknownCommit, handler_test.go335/338: expired request reached Store and returned201 instead of503. All147 realPG18 normal tests had already passed. Race and API audit in this Push were skipped after the failed other-packages step; do not count them as passes.
- Sibling PR39709822402 passed all three jobs. That success does not invalidate the Push failure.
- verify and execution-security passed in both runs. Original failing native command, source/log hashes and raw failure logs are preserved here and in linked Actions Artifacts. No blind rerun was requested.

The fixture uses real1ms processing timeout and5ms Hash sleep. Handler.expired previously checked only ctx.Err(), which can lag the actual deadline until the timer cancellation callback executes. The actual failing run has no runtime timer trace, so the exact scheduling interleaving remains unproven. A deterministic new unit context models Deadline already past while Err remainsnil; the old implementation fails this gate regression. This is a unit gate proof, not PG18 or physical network evidence.

## Minimal repair

Handler.expired additionally compares time.Now() to the context Deadline before permitting Store. Go's monotonic time is retained. No public budget, signature, credential, database privilege or scheduler behavior changes. All original registration test source/assertions, including the1ms/5ms fixture and ambiguous-Commit assertions, are byte unchanged. One new regression checks past deadline rejected503 and future deadline accepted even before cancellation. No timeout extension or new skip.

Development receipts record dirty=true and source inventory `d1bc1c8c89abd6c6257201e62cadc1e51b3d4c2d0b45d5cad1b8a6314d9aec1c`; they are not frozen-HEAD CI evidence. regression-before exit1 is the expected failing new assertion on old code. registration-normal/race initially failed due sandbox localHTTP listener/cache denial; their full logs are retained. With normal authorized host permissions, registration-normal-cache, registration-race-cache, full unit, full race and vet exited0. Final delivered HEAD must independently pass actual Push/PR CI; its artifacts provide immutable checkout/source/log/binary hashes and exact test commands. The new regression adds one other-package test, so expected other-package count becomes152; PG18 remains147.

Tasks remain25/47, 6.3/5.4/6.4/6.5 unchecked, production execution disabled, PR11Open. No merge/archive/deploy or new network/Supervisor work.

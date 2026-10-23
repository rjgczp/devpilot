# PR11 CI repair: frozen implementation verification

Frozen implementation: `691c9933956aa7e1d7e6a30cfde628941b9cac06`.
Source inventory SHA256: `1956bbc2c6b2ad25504c5d6fe5dae5df42ff6cb101cbc10a32e1965d12fe2228`.

- [Push CI](https://github.com/rjgczp/devpilot/actions/runs/39708326035): verify, integration, execution-security all success.
- [PR CI](https://github.com/rjgczp/devpilot/actions/runs/39708331695): verify, integration, execution-security all success.
- Both runs: 147 PG18 tests normal and race; 151 other-package tests each mode; four historical Snapshot tests each mode on separate Schema6; original 76 network scenarios plus five Target Permit denial scenarios; API audit real exit0 and deliberate bypass audit exit1.
- Each real authorization test retained its 60-second wait, each real Lease test its 45-second wait; each Go test process retains timeout120s. No skips or test removals. Exact names/commands/elapsed times/exits/log hashes are in plans, receipt indexes and summary.json.
- Change strict on clean frozen HEAD: exit0, receipt and original log included.

Original failure logs and before/after test-list comparison remain in ../ci-repair-before/. No runtime source, Migration1–8, SQL ACL, old security assertion or API network whitelist changed in this CI repair.

## Raw evidence and validation

summary.json links every GitHub Actions Artifact and records its GitHub-reported archive digest. Large raw traces/logs remain there. *-artifact-file-sha256.json lists locally verified extracted-file SHA256 values; *-receipt-index.json records commands, exits, clean checkout HEAD, source inventory, log and binary hashes. Run and artifact API metadata are also preserved. Downloaded receipt logs were checked against recorded hashes; both normal/race plans prove complete coverage; the five permit-denial traces each record one DNS attempt and zero target attempts. Positive-control audit failures are expected detections, not passing unsafe traffic.

The delivery evidence commit only updates this directory and verification.md. Its latest HEAD CI must be checked separately; these results explicitly belong to 691c993, not an untested descendant. Delivery run links and actual conclusions are supplied in the final review handoff. Checkpoint tasks stay25/47; 6.3/5.4/6.4/6.5 remain incomplete. Production execution stays disabled.

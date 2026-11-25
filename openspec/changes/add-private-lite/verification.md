# Isolated acceptance, 2026-11-25

Code/test HEAD: `6053e2cf5f265248cfc55dfb3260f7e2f4774acc`; base main: `6a925ade180ed9d0c6daf889ee78509c4af7f705`.

[Private-Lite run 45574026633](https://github.com/rjgczp/devpilot/actions/runs/45574026633) passed. Artifact `private-lite`, ID `15002809964`, ZIP SHA256 `6ecc68f9d96389d3d7b8cca14ffefc2c6a16af184c2d550afbe8272b64660c83` includes source/binary hashes, logs and signed physical proofs. Every packet / allow / unapproved / DNS-change / private4 / private6 / approval-revoked receipt has exit 0 and clean fixed HEAD.

- Packet paths were calibrated before enforcement, including IPv6. Unauthorized destination/port, metadata, private, host management and IPv6 traffic was then denied. Removing either host table, expiry, revoke and latched close blocked new AND established connections; closed admission could not renew.
- PG18 reused the existing Owner/Permit/Supervisor protocol: three GETs, two completed and one fault-injected unknown Run, History, no restart resend and trusted physical release. The runtime status is `private-lite`; the inherited `approved-https-result.json` deliberately names its unchanged `private-approved` SQL protocol.
- Unapproved catalog scope claimed no Run. DNS binding change, private A and private AAAA each sent zero target GETs. Removing operator approval from an idle running runtime closed admission with zero completed Runs. Established-flow revocation is independently covered by the packet test.
- At inspection, verify / private-probe / private-service / approved-https also passed on this HEAD. The existing integration and execution-security long jobs were still running, not failures or claimed passes. Final documentation HEAD automatically receives its own applicable CI; see PR checks for current status.

Earlier raw failures are preserved in GitHub runs `45572205119` (new script Git safe-directory omission) and `45572785857` (old closed host anchors blocked clean restart). Fixes do not relax budgets/assertions: anchors retire only after original signed physical drain; the foreground harness waits/reaps before restarting. Local resource refusal evidence remains under `/Users/sansea/.codex/artifacts/devpilot/private-lite-20261125/allow-v4`; the original storage watermark stayed unchanged.

Scope: all DNS/TLS receivers and host stand-ins are disconnected, with no upstream/default route. Local disposable containers/temporary secrets were cleaned. No real DNS endpoint, real host Docker hook coexistence, Lighthouse capacity, five production LOGIN bindings or resistance to hostile host root/kernel/daemon (including removal of all enforcement) is proved. No Lighthouse/production SQL/Gate/public probe was touched. Run and privilege instructions: `deploy/private-lite/README.md`.

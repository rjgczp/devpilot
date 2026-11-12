# Private management MVP — execution CLOSED

This example packages the existing Go API and compiled React Dashboard behind a
single HTTPS origin. It is for an existing, invited VPN network on a dedicated
native Linux host. It installs no VPN and is not a public registration service.
There is no Worker/Probe/Supervisor service. API/PG have no external attachment. Saving a
URL or recording Owner consent does not execute it. History stays genuinely
empty until a separately approved execution service exists; the existing UI
reports management/execution-closed state.

Prerequisites: Docker Engine/Compose, Python3, a configured VPN IPv4 on the host,
private DNS, and a certificate/key for that DNS name trusted by invited browsers.
Start with 2vCPU/4GiB RAM and 40GiB persistent storage for this management-only
stack; this is an initial allocation, not a measured capacity guarantee. Runtime
PG/API/web memory caps are 1GiB/256MiB/128MiB. Build dependencies are downloaded
only during image preparation. No public ACME/HTTP challenge is configured.

## Prepare and start (only when deployment is separately authorized)

From `deploy/private` in the checked-out repository:

```sh
cp private.env.example .env
# Edit all four nonsecret settings: existing VPN IP, HTTPS port, DNS, peer CIDRs.
python3 configure.py --init-secrets .secrets
# Supply .secrets/tls.crt (PEM chain) and .secrets/tls.key (unencrypted PEM key).
chmod 700 .secrets
chmod 444 .secrets/tls.crt .secrets/tls.key
docker compose --profile tools build
docker compose up -d --wait postgres
docker compose run --rm migrate migrate
docker compose run --rm migrate provision
docker compose run --rm migrate check
docker compose up -d api web
```

Open `https://<PRIVATE_HOST>:<PRIVATE_HTTPS_PORT>` from the invited VPN, register,
log in, create a Workspace/Project/Monitor, and inspect its closed execution
state/history. HTTPS origin is fixed by those settings. `AUTH_MODE=production`
uses the existing `__Host-` Secure/HttpOnly/SameSite cookie and Origin/CSRF checks.
The gateway adds the same Origin/proof check to the existing registration route.
Do not use Vite, development cookies, a public bind address, or TLS-verification
bypass as deployment shortcuts. VPN-peer admission is separate from Workspace
membership: normal API authorization still applies.

The configuration rejects wildcard/public listener addresses and public/anywhere
CIDRs. Docker publishes only the explicitly configured VPN IP and HTTPS port;
Nginx also denies all unlisted source networks, including static files. It never
trusts client `Forwarded`/`X-Forwarded-*` headers. Do not add another load balancer
or address-rewriting gateway without reviewing its peer-identity boundary. Verify
host/cloud rules from both an invited and a noninvited client before use; a
private-looking address is not proof the VPN/firewall is correctly configured.

Login/registration share a 5/minute per-peer bucket (burst5), plus 1/second
aggregate (burst10); rejection returns429 and Retry-After60. Existing bounded
password computation remains unchanged. Other authenticated reads are not put in
this authentication bucket. Tune only after review, not to bypass a failed test.

## Database/secret and lifecycle boundaries

Use only a **new dedicated** `devpilot_private` database/volume for this example.
The offline migrator's PG bootstrap administrator (superuser, needed by existing
role/owner migrations) is distinct from
`devpilot_private_app`. Only PG and the explicitly invoked tools service mount
its password. API mounts only its own DSN secret, loaded with DATABASE_URL_FILE;
setting both DSN sources fails. Web mounts only its certificate/key. Compose
does not reliably remap uid/mode for file-backed secrets: host parents are0700,
files0444 so the distinct nonroot container UIDs can read only their mounted
files. Never widen the parent, add secrets to Git/build contexts, or print them.

`provision.sql` is the exact separately approved runtime ACL, applied once by the
offline tool, never by API startup. It gives only the existing API function role
and explicit business-table/column operations, including row-lock and unchanged
invoker-trigger prerequisites. No scheduler owner/Worker/Controller/Supervisor
membership, direct Run/control/budget access, DDL, TEMP or migration-table access
is granted. Tenant isolation remains enforced by the existing API and scoped
functions; this role is a trusted API identity, not a user SQL login.

The tools command applies the existing migrations **1–10,12,13**, without edits,
renumbering or Migration11. It refuses an existing open deployment and checks
`gate=closed AND emergency_stop` after migration and before provisioning.
Do not point it at an existing/live application database. `provision` deliberately
refuses an existing app role instead of silently changing its password/ACL.
Normal `up api web` never runs migrations or provisioning.

`docker compose down` retains data; do not add `-v` on a persistent instance.
Back up the dedicated DB and keep secrets/certificate copies privately before
maintenance. Upgrade and restore procedures require their own reviewed operator
steps; this example does not auto-deploy, auto-migrate, or auto-restore. All
API/backend and database networks are internal; PG/API publish no ports. Only
the fixed TLS proxy has a routable ingress bridge attachment, necessary for host
port publication, and it has IP forwarding disabled and no network-management
capability. That attachment is not an HTTP execution service or a claim of safe
public probe egress. Logs rotate at3×10MiB
per service and contain no HTTP access log or password arguments.

## Verification and remaining scope

```sh
go test ./internal/config ./internal/auth ./internal/app
python3 -m unittest discover -s tests/private -p 'test_*.py' -v
python3 tests/private/run.py --require-host-ingress --logs /absolute/new/log-directory
```

The last command creates/destroys its **own** labelled volume and PG18 database,
uses a disposable TLS CA, tests the compiled UI, real Cookie/CSRF, business writes,
closed Owner consent/history, shared rate limiting, forbidden peer/XFF bypass,
and denied runtime SQL privileges. Native Linux CI additionally requires the
published host HTTPS path; Docker Desktop container-only success cannot prove
native host ingress or the actual VPN/cloud boundary. It calls no monitor target.

Real HTTP execution, dynamic target admission, public egress/physical-stop service
and continuous retention/storage admission remain unavailable. This does not
complete Task8.3 or9.2–10.5; OpenSpec stays37/47. No physical release, SSRF, rate,
transaction or lock-wait assertion is weakened.

References: [NGINX rate limiting](https://nginx.org/en/docs/http/ngx_http_limit_req_module.html),
[Compose networking](https://docs.docker.com/compose/how-tos/networking/),
[Compose secrets](https://docs.docker.com/compose/how-tos/use-secrets/).

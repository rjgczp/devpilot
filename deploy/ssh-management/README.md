# SSH-only Linux management deployment

Uses the existing private management stack with a new database and persistent
volume. No execution services, M14–16, runtime identities or outbound probe route.
Do not use the release private-beta overlay while execution remains closed.

On the selected host use a dedicated source directory and Compose project
`devpilot-mvp`. `deploy/private/.env` must contain:

```
PRIVATE_BIND_IP=127.0.0.1
PRIVATE_HTTPS_PORT=18443
PRIVATE_HOST=devpilot.localhost
PRIVATE_ALLOWED_CIDRS=127.0.0.1/32,172.30.87.1/32
```

The second allowlisted source is Docker's dedicated entry bridge gateway, as
seen for host-local SSH forwarding; it is not a public/VPN peer allowlist. Check
that subnet 172.30.87.0/24 and port 18443 are unused before creating the stack.
The overlay caps PG/API/web at 256/160/64 MiB, with low PG connections and buffers;
monitor actual resource use on a shared server. Images are built off-host for
linux/amd64 and transferred through SSH; no registry credential or SSH key is
included. Watchtower updates are disabled for these three services.

Generate secrets using the existing `deploy/private/configure.py`, supply a TLS
certificate for `devpilot.localhost`, and keep the secret directory 0700. Only
public CA material should be copied to a client; never copy the TLS private key.
Use a trusted private CA and add its public certificate to the intended browser's
trust store before use. Do not disable certificate verification.

All operator commands use both files, with `deploy/private` as project directory:

```
docker compose -p devpilot-mvp -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml up -d --wait postgres
# Only after explicit authorization for this NEW database's management schema/role:
docker compose -p devpilot-mvp -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml run --rm migrate migrate
docker compose -p devpilot-mvp -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml run --rm migrate provision
docker compose -p devpilot-mvp -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml run --rm migrate check
docker compose -p devpilot-mvp -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml up -d api web
```

For image-only installs add the local `images.yaml` override which maps api,
web and migrate to the corresponding SHA-tagged images. Do not run `--build` on
the shared low-memory host. Archive only tracked source; never archive `.env`,
`.secrets`, SSH configuration, or private keys.

From the workstation:

```
ssh -N -o ExitOnForwardFailure=yes -L 18443:127.0.0.1:18443 cloud
```

Open `https://devpilot.localhost:18443`. Registration/login and Workspace,
Project and Monitor CRUD are available. History is empty until separately
authorized execution exists. Stop with `docker compose ... stop`; retain the
volume, secret directory and TLS material for restart. Never use `down -v`.

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
The overlay caps PG/API/web at 256/256/64 MiB, with low PG connections and buffers;
monitor actual resource use on a shared server. Images are built off-host for
linux/amd64 and transferred through SSH; no registry credential or SSH key is
included. Watchtower updates are disabled for these three services.

After rendering, run `chmod 755 deploy/private/.runtime` so the nonroot gateway
can traverse its nonsecret configuration mount even with operator umask 077.
Keep `.secrets` and the host installation directory 0700.

Generate secrets using the existing `deploy/private/configure.py`, supply a TLS
certificate for `devpilot.localhost`, and keep the secret directory 0700. Only
public CA material should be copied to a client; never copy the TLS private key.
Use a trusted private CA and add its public certificate to the intended browser's
trust store before use. Do not disable certificate verification.

All operator commands use both files, with `deploy/private` as project directory:

```
docker compose -p devpilot-mvp --env-file deploy/private/.env -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml up -d --wait postgres
# Only after explicit authorization for this NEW database's management schema/role:
docker compose -p devpilot-mvp --env-file deploy/private/.env -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml run --rm migrate migrate
docker compose -p devpilot-mvp --env-file deploy/private/.env -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml run --rm migrate provision
docker compose -p devpilot-mvp --env-file deploy/private/.env -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml run --rm migrate check
docker compose -p devpilot-mvp --env-file deploy/private/.env -f deploy/private/compose.yaml -f deploy/ssh-management/compose.yaml up -d api web
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

## AI deployment candidate (inactive)

`compose.ai.yaml.candidate` and `ai.env.example` prepare compatibility only. The
server's `manage.sh` does not load them automatically. They contain no key or
secret file and authorize no rollout, model request or outbound connection.
After separate rollout/provider approval, layer the candidate **after** the
private and SSH Compose files; never run it alone with `-f`. Secret paths resolve
relative to the first `deploy/private/compose.yaml`, so the future host file is
`deploy/private/.secrets/ai_api_key`. Keep its parent 0700 and file 0444 for the
API's UID 65534; only API receives the named secret, retaining its `app_url`.

All `AI_*` absent leaves diagnosis disabled. Partially configuring the API fails
startup; the candidate also requires a fixed provider URL and model. It creates
no egress: API stays on internal networks and must not join `entry`. The client
sets `Proxy=nil`, so environment HTTP proxies do not supply a model channel.
Provider connectivity remains separately authorized and unconfigured here.

A future rollout must rebuild both API and Dashboard from the integrated SHA,
update their image mappings, and rerender `deploy/private/.runtime/nginx.conf`
with the AI location's 32-second timeout. Retain `chmod 755` on `.runtime` for the
nonroot gateway. Recreate only DevPilot API/web; this requires no database
migration or permission change. This candidate has not been activated or tested
against a real model, and does not change the closed execution Gate.

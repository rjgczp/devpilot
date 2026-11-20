# Read-only AI diagnosis MVP

POST `/api/v1/ai/diagnose` accepts `workspace_id`, `project_id`, `monitor_id`.
It requires the same session cookie and Origin/CSRF proof as existing mutations.
Every request resolves the Monitor and recent history through existing scoped
monitor reads before calling the model. Only current Workspace members can read.
The Dashboard exposes diagnosis in the Monitor history view.

Configure the Go API process (never Vite/browser environment):

```
AI_BASE_URL=https://your-approved-provider.example/v1
AI_MODEL=your-approved-model
AI_API_KEY_FILE=/run/secrets/ai_key
AI_TIMEOUT=20s
```

The client appends `/chat/completions`. `AI_API_KEY` is an alternative server
secret source; using both key sources fails startup. All settings absent means
AI is disabled (503), with existing API behavior preserved. HTTPS is required,
except loopback HTTP for local compatible providers. Redirects and ambient proxy
configuration are disabled. Use only an approved fixed provider. The management
stack's internal API networks intentionally have no public egress: setting these
variables does not authorize or provision an outbound route. Model/provider
connectivity requires its own environment configuration and approval.

The model receives sanitized monitor configuration (no name or target URL) and
at most 10 existing history rows. Selected configuration/result identifiers are
sent to the configured provider. No second monitoring data store is created.
Tools `get_monitor` and `get_recent_results` have no arguments and can only read
these authorized snapshots. They cannot perform probes, writes, URL fetches,
SQL or system commands. There is no automatic remediation.

A diagnosis has at most 3 model requests and 3 tool calls, 800 output tokens per
request, 24,576 request bytes, 32,768 provider-response bytes and 6,000 final
content bytes before fixed uncertainty notices. Input history is bounded to 10
rows. The server allows two active diagnoses and one request per session per
30 seconds (bounded 512-entry recent-session cache). The total deadline defaults
to 20 seconds and cannot exceed 30 seconds. Failures are not retried. There is
no global billing quota; operators should also set a provider-side spend cap.

Successful `data` contains `summary`, `evidence`, `possible_causes`, `confidence`,
`limitations`, `recommendations`. The server validates this structure, rejects
truncated/invalid output, forces low confidence for empty history and always adds
an uncertainty notice. The Dashboard renders model text without HTML or links.
Model output remains a hypothesis, not proof of root cause.

Errors use existing JSON envelopes: `ai_unavailable` (503), `ai_busy` (429),
`ai_timeout` (504), `ai_provider_failed` (502). Provider bodies/credentials are
not returned or logged. Configure the existing TLS gateway's AI location with
32-second read timeout; other API routes retain their original timeout.

Validation:

```
go test -race ./internal/ai ./internal/config ./internal/app ./cmd/api
# Isolated PG18 DB, goose on PATH; never use a production/application DSN:
go test -race -tags=integration ./tests/integration -run '^TestAIDiagnoseWorkspaceBoundaryPG$' -count=1
cd web && npm ci && npm run build && npm run test
# With the project's Playwright browser installed:
npm run test:e2e -- ai-diagnosis.spec.ts
```

No real model credentials are required for these tests; mock providers exercise
bounded tools, errors, cancellation and tenant isolation. Real provider/model
quality and latency remain a separate acceptance step.

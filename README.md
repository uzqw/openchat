# OpenChat — Gemini Q&A

A Q&A service built on host OpenCLI (`@jackwener/opencli@1.8.7`) + a visible Chrome instance:
a Go + PocketBase (SQLite) backend providing a FIFO queue, conversation/task models, and a REST API,
with a React 19 + TypeScript + Vite + Tailwind single-column frontend. The provider is **Gemini**
only: every conversation runs on the Gemini adapter. Site capability details live in
`internal/opencli/site.go`. The `OPENCLI_SITE` env var is retained for compatibility and must be
`gemini` (anything else is rejected at startup).

Docs: `architecture.md` (design); `docs/opencli-contract.md` and `docs/domain-api.md` (API & data
model); `docs/deployment-operations.md` (deployment & ops); `docs/roadmap.md` (roadmap).

## Prerequisites

- Go `>= 1.25` (this repo's `go.mod` declares `go 1.25.0`)
- Node.js `>= 20` (to build `web/`)
- `@jackwener/opencli@1.8.7` installed on the host, matching Browser Bridge Extension `v1.0.23`
- A visible Chrome running with the extension connected, using a **dedicated OpenCLI profile**
  (the app exclusively owns its Adapter tab)
- Gemini Web logged in once manually in that profile (one manual login on first use)
- Backend should run under a **dedicated OS service account/HOME**, not mixed with daily OpenCLI config

> ⚠️ The platform can operate your real Gemini account: read "Security & Notes" before use.

## Install

```bash
# Backend
go build -o openchat-server ./cmd/server     # CGO_ENABLED=0 also works
go build -o mcpserver ./cmd/mcpserver        # read-only MCP bridge (see below)

# Frontend (web/dist is statically served by the backend, so build it first)
cd web && npm ci && npm run build && cd ..
```

## Configuration

The backend is configured entirely via environment variables (see `.env.example`; copy it to
`.env` and run `set -a; . ./.env; set +a`). It **refuses to start (fail closed)** when critical
config is missing. Required: `PB_DATA_DIR` (data dir), `OPENCLI_PROFILE` (dedicated profile);
non-loopback listeners additionally require `BASIC_AUTH_USER` / `BASIC_AUTH_PASS`,
`OPENCLI_TRUSTED_HOST`, `OPENCLI_TRUSTED_ORIGIN`. Full variable table:
[`docs/deployment-operations.md`](docs/deployment-operations.md) §4.

Fail-closed rules: refuses to start on non-loopback listen without Basic Auth credentials,
trusted Host, or trusted Origin; `OPENCLI_DEV_NO_AUTH` is loopback-only; write operations
(ask/retry/login) are refused when a local `~/.opencli/clis/<site>` override exists, an OpenCLI
plugin is installed, or the version ≠ `1.8.7`.

## Run

```bash
export PB_DATA_DIR=/var/lib/openchat/pb_data
export OPENCLI_PROFILE=openchat-gemini
export BASIC_AUTH_USER=... BASIC_AUTH_PASS=...
# (non-loopback also needs OPENCLI_TRUSTED_HOST / OPENCLI_TRUSTED_ORIGIN)
./openchat-server            # or go run ./cmd/server
```

On startup it automatically: tightens data-dir permissions → runs startup recovery (stale
pending→canceled, running→unknown+quarantined, active→archived) → runs version/doctor probes →
starts listening. Health check (backend + SQLite only, no OpenCLI):

```bash
curl -fsS http://127.0.0.1:8090/api/health
```

Open `http://127.0.0.1:8090/` in a browser (Basic Auth protected). API overview:
`docs/domain-api.md`; errors are uniformly
`{"error":{"code":"stable_code","message":"safe message"}}`.

## Build & Redeploy (production)

### Build

```bash
# Frontend first: the backend statically serves web/dist; skipping the build serves stale assets
cd web && npm run build && cd ..

# Backend binary to the production path (CGO_ENABLED=0 also works)
go build -o openchat-server ./cmd/server
```

### Deploy to grokbot

Production runs on grokbot (10.0.0.2, WireGuard intranet); the public entry `chat.gostapi.com`
goes through local caddy → Authelia forward-auth → WireGuard → grokbot `socat` →
`127.0.0.1:18090`. The grokbot sandbox has no systemd, so it uses setsid + a watchdog for
self-healing.

Scenario A: edit on grokbot directly (current main path)

```bash
cd /workspace/wp/openchat
cd web && npm run build && cd ..     # only if the frontend changed

# Back up the old binary (for rollback), then build and restart
go build -o /opt/openchat-server.new ./cmd/server
mv /opt/openchat-server /opt/openchat-server.bak.$(date +%s)
mv /opt/openchat-server.new /opt/openchat-server
/opt/start-openchat.sh               # pkill old process → setsid nohup
```

Scenario B: edit locally, push over

```bash
cd /home/ubuntu/wp/openchat
tar czf - --exclude=.git --exclude=node_modules --exclude=dist . \
  | ssh grokbot 'tar xzf - -C /workspace/wp/openchat'
ssh grokbot 'cd /workspace/wp/openchat && cd web && npm run build && cd .. \
  && go build -o /opt/openchat-server ./cmd/server && /opt/start-openchat.sh'
```

Verify (on grokbot):

```bash
curl -s http://127.0.0.1:18090/api/health          # backend + SQLite
curl -s http://127.0.0.1:18090/api/providers       # gemini snapshot; check site / logged_in / models
curl -sI https://chat.gostapi.com/                 # unauthenticated should 302 → /authelia/
```

### Provider: Gemini only

The provider is fixed to Gemini. Every conversation carries a `provider` field (always
`gemini`), and resume/follow-ups run on the Gemini adapter. `/api/providers` returns the Gemini
snapshot; `POST /api/providers/gemini/login` and `/refresh` operate the shared adapter tab.

```bash
# Check Gemini login state
opencli --profile openchat-gemini gemini status --format json   # expect "Login": "Yes"
```

Rollback: `mv /opt/openchat-server.bak.<timestamp> /opt/openchat-server && /opt/start-openchat.sh`.

## External AI access (MCP, read-only)

`cmd/mcpserver` exposes conversation history to other AI agents (Claude Code / Cursor / Desktop):
a **read-only** stdio bridge where each tool maps to an existing GET endpoint and renders
transcripts as AI-friendly Q/A text. No write operations are exposed by design.

```bash
go build -o mcpserver ./cmd/mcpserver
OPENCHAT_API_URL=http://127.0.0.1:8090 ./mcpserver   # same host is fine; no listening port
```

Optional env vars: `OPENCHAT_API_USER`/`OPENCHAT_API_PASS` (if the API has Basic Auth enabled),
`OPENCHAT_API_TIMEOUT_SECONDS` (default 120). Tools: `list_conversations` (paginated list),
`get_conversation` (full transcript, for summarization), `get_turn` (single turn details).
AI client config:

```json
{"mcpServers": {"openchat-history": {"command": "/opt/openchat/bin/mcpserver", "args": []}}}
```

## Tests

```bash
# Backend
go test ./...
go vet ./...
go build ./...

# Frontend (in web/)
npm ci
npm run lint
npm test -- --run
npm run build
```

All automated tests never touch real Gemini accounts: they inject an executable fake OpenCLI
(`internal/opencli/fakeopencli`) covering success, waiting, exit codes, timeouts, invalid JSON,
oversized stdout/stderr, cancel and resume; data always uses temp dirs, never real `pb_data`.

## Manual Gemini smoke test

A smoke test that sends real prompts to a real Gemini account is run by
`scripts/smoke-gemini.sh` and **requires explicit opt-in**:

```bash
LIVE_GEMINI_SMOKE=1 OPENCLI_PROFILE=openchat-gemini scripts/smoke-gemini.sh
```

The script verifies in order: `opencli --version` (pinned 1.8.7) → `doctor` → `gemini
status/whoami` → `models` → first turn of a new session `ask --new true` → second turn in the
same session (confirms context works). Same discipline as the backend:

- Refuses to run without `LIVE_GEMINI_SMOKE=1`; even if Chrome/login state is auto-detected, it
  never sends real prompts by default.
- Never logs out, never forces timeouts, never changes account security state.
- `auth_required` is only verified manually under a separate dedicated **logged-out** profile
  with `LIVE_GEMINI_AUTH_SMOKE=1`, checking only that `whoami` exits with code 77.
- Timeout/kill scenarios are covered by fake OpenCLI tests (`internal/runner`), not in this script.
- If the environment lacks the conditions (no opt-in, no visible Chrome, or no login state), do
  not run the script; honestly record `live Gemini smoke: not run` with the reason; never fake results.
- The script never prints cookies / profile contents / sensitive paths.

## Security & Notes

- Global Basic Auth on UI/API, with only `/api/health` explicitly open; PocketBase admin and
  system routes are protected/disabled; production transport uses HTTPS/VPN (Basic Auth must not
  be exposed over plaintext HTTP on the public internet).
- The dedicated profile's OpenCLI Adapter tab is exclusively owned by this app; no manual
  navigation except login and failure confirmation, and no other OpenCLI client uses it.
- The platform never copies or exports Chrome cookies; data dir and backups are owner-only.
- Before using real Gemini, be aware: it consumes official Gemini quota, and automation that
  violates the site's ToS carries an account-ban risk; pinned OpenCLI/Extension versions only
  prevent dependency drift, not website DOM drift.

# Deployment

Operating Cribbager in production. For running it locally, see
[Development](development.md).

Live at **https://cribbager.org**, deployed on fly.io.

## Continuous deployment

Every merge to `main` runs CI and, if it passes, auto-deploys to fly via the `deploy`
job in `.github/workflows/ci.yml` (`flyctl deploy --remote-only --ha=false`). A
post-deploy smoke check then hits `/healthz`, `/readyz`, and `/` to confirm the
release is actually serving and can reach the database. **No manual deploy is needed.**

A deploy briefly restarts the single machine: SSE clients reconnect automatically and
in-progress games reload from Postgres, so the blip is a reconnect rather than data
loss.

CI authenticates to fly with a `FLY_API_TOKEN` repository secret (a scoped fly deploy
token, `fly tokens create deploy`).

## Single-machine constraint

Deployed as a **single always-on machine** — a hard requirement, not a preference: all
game state is **in memory** and players talk over SSE, so both players of a head-to-head
game must hit the same process.

**Always deploy with `--ha=false`, and never `fly scale count` above 1.** A second
machine would split players across processes and break head-to-head play. `fly.toml`
keeps the single machine always-on so an idle shutdown doesn't drop in-progress games.

## Manual deploy

The CD pipeline handles deploys, but to deploy by hand (e.g. first-time setup):

```bash
fly auth login
fly apps create cribbager        # once; the `app` name in fly.toml must match
fly deploy --ha=false            # builds the Dockerfile and brings up ONE machine
```

## Persistence (Postgres)

By default games live only in memory. Set `DATABASE_URL` to a Postgres DSN and
in-progress games, accounts/sessions, and finished-game history are written through to
Postgres and reloaded at boot, so a deploy or restart doesn't drop them. Postgres is
portable (fly-managed, Neon, Supabase, or local Docker) — nothing here is
provider-specific.

The deploy command is unchanged — the app just needs `DATABASE_URL` in its
environment. No migration step (the schema is created on boot). Provision a Postgres
either way:

```bash
# Fly-managed: attach sets the DATABASE_URL secret automatically (and restarts).
fly postgres create
fly postgres attach --app cribbager <pg-name>

# Or an external, portable Postgres (Neon/Supabase, etc.) — set the secret yourself:
fly secrets set DATABASE_URL='postgres://USER:PASS@HOST:5432/DB?sslmode=require' --app cribbager
```

Notes:
- **`sslmode`** is the usual snag with `lib/pq`: external Postgres wants
  `?sslmode=require`; fly's *internal* Postgres usually wants `?sslmode=disable`.
- **Boot is fail-fast** — if `DATABASE_URL` is set but the database is unreachable, the
  app exits rather than silently running without persistence (check `fly logs`). Safe
  order: deploy the code first (runs in-memory, can't fail on the DB), then provision +
  attach Postgres to switch persistence on.
- The stores share **one connection-limited pool**, and every query has a short
  timeout, so a slow database degrades gracefully instead of wedging requests.

### Backups & durability

Postgres holds the only irreplaceable data — accounts and the permanent finished-game
history (`results`), which is never reaped. **Configure backups**: fly-managed Postgres
should run scheduled snapshots (or a `pg_dump` cron to object storage); a managed
provider (Neon/Supabase) gives point-in-time recovery — note its retention as your RPO.
In-progress games are best-effort (they reload on restart, and a lost DB just drops
them); accounts and history are what a backup protects. The terminal write of a
finished game is retried so a transient blip doesn't lose it.

## Password reset (email)

Registered players reset a forgotten password from the homepage: **Forgot password?**
emails a single-use link to `/reset.html?token=<id>`, valid for **1 hour**, consumed on
first use. The reset-request endpoint always returns the same generic response whether
or not the email is registered, so it can't be used to discover which addresses have
accounts.

Without `SMTP_HOST` the reset link is only logged (dev). In production, point `SMTP_*`
at any provider — there's no lock-in. For example, [Resend](https://resend.com) over
SMTP:

```
SMTP_HOST=smtp.resend.com
SMTP_PORT=587
SMTP_USER=resend
SMTP_PASS=<your Resend API key>
SMTP_FROM=you@your-verified-domain   # verify the domain's SPF/DKIM in the provider
BASE_URL=https://cribbager.org       # so the emailed link points at the public site
```

Locally, `docker compose` runs **Mailpit** instead (see [Development](development.md)).

## Configuration reference

All set via `fly secrets set` (or local env). Non-secret toggles live in `fly.toml`.

| var | meaning |
| --- | --- |
| `DATABASE_URL` | Postgres DSN. Unset → in-memory only (no persistence). |
| `SECURE_COOKIES` | Set (e.g. `1`) behind https so session cookies carry the `Secure` flag. In `fly.toml`. |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USER` / `SMTP_PASS` / `SMTP_FROM` | Reset-email transport. Unset → links are logged only. |
| `BASE_URL` | External origin for emailed links, e.g. `https://cribbager.org`. Unset → derived from the request (honoring `X-Forwarded-Proto`). |
| `STATS_TOKEN` | If set, gates `GET /stats` **and `GET /metrics`** behind this bearer token. Unset → both are open. |
| `GOMEMLIMIT` | Soft heap ceiling. Set to `220MiB` in `fly.toml` for the 256 MB machine. |
| `ADDR` / `WEB` | Listen address / static dir (mostly for dev). |

## Observability

- `GET /healthz` — liveness; returns `ok` whenever the process is up. (fly's health check.)
- `GET /readyz` — readiness; pings the database. Use it for a post-deploy check; it is
  deliberately *not* the liveness probe, so a transient DB blip doesn't restart the app.
- `GET /stats` — `{"games": N, "subscribers": M}` (gate with `STATS_TOKEN`).
- `GET /metrics` — Prometheus text-format metrics (gated by the same `STATS_TOKEN`).

Active game/subscriber counts are also logged each reap cycle.

### Metrics (`GET /metrics`)

Hand-written Prometheus exposition (no `client_golang` dependency — the format is
trivial and the project keeps deps minimal). Gate it in production with
`STATS_TOKEN`; scrape it with the same bearer token.

| metric | type | meaning |
| --- | --- | --- |
| `cribbager_http_requests_total{class=…}` | counter | Requests handled, labeled by status class (`2xx`/`3xx`/`4xx`/`5xx`/`other`). Low cardinality — no raw paths or game ids. |
| `cribbager_games_live` | gauge | Live game sessions in memory. |
| `cribbager_sse_subscribers` | gauge | Live SSE stream subscribers. |
| `cribbager_uptime_seconds` | gauge | Process uptime. |
| `go_goroutines` | gauge | Current goroutine count. |
| `go_memstats_heap_alloc_bytes` | gauge | Allocated heap bytes. |
| `go_memstats_sys_bytes` | gauge | Bytes obtained from the OS. |
| `go_memstats_gc_total` | counter | Completed GC cycles. |

### Alerting

Fly does not alert on app metrics out of the box — alert rules are infra config
that lives wherever you scrape from (Fly's managed Prometheus + Grafana, or your
own). The two alerts worth wiring:

**1. Elevated 5xx rate.** Fires when more than ~5% of responses are 5xx over 5
minutes (server-side errors the recover middleware turned into 500s, or capacity
503s). Prometheus rule:

```yaml
- alert: HighHTTP5xxRate
  expr: |
    sum(rate(cribbager_http_requests_total{class="5xx"}[5m]))
      / clamp_min(sum(rate(cribbager_http_requests_total[5m])), 1)
      > 0.05
  for: 5m
  labels: { severity: page }
  annotations:
    summary: "Cribbager 5xx rate above 5% for 5m"
```

**2. High memory.** The machine has 256 MB and `GOMEMLIMIT=220MiB`. Prefer Fly's
own machine-memory metric (`fly_instance_memory_mem_total` / `…_available` via the
Fly metrics integration) so you catch the *whole* process RSS, not just the Go
heap. As an app-side proxy, alert when Go's reported system memory crosses ~200 MB:

```yaml
- alert: HighMemory
  expr: go_memstats_sys_bytes > 200e6
  for: 10m
  labels: { severity: page }
  annotations:
    summary: "Cribbager Go runtime memory above 200 MB (256 MB machine)"
```

Tune the thresholds to real traffic before promoting either to a page.

## Governance (todo)

Branch protection on `main` (require the `go` / `web` / `lint` checks before merge) is
**deferred** — it needs a public repo or a paid plan. Enable it when the repo goes
public; until then the "CI gates deploy" guarantee is by convention. Likewise the
in-CI smoke tests run non-blocking for now and can be promoted to a required gate once
proven stable.

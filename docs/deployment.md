# Deployment

Operating Cribbager in production. For running it locally, see
[Development](development.md).

The official instance runs at https://cribbager.org. To host your own, with your own
domain, follow **[`deploy/macos/README.md`](../deploy/macos/README.md)**; this page covers
how the app behaves in production, whoever hosts it.

## Deploying

The macOS guide covers setup, continuous deployment, rollback, backups and operation
(see [`deploy/`](../deploy/README.md) for the options). In short: a launchd job on the Mac
fetches `main` every minute, builds, restarts the app, checks `/readyz`, and rolls back on
failure. CI does not deploy anything.

## Single-machine constraint

Deployed as a **single always-on machine** — a hard requirement, not a preference: all
game state is **in memory** and players talk over SSE, so both players of a head-to-head
game must hit the same process.

**Never run a second instance** (another Mac, another launchd job, a container beside it).
A second process would split players across processes and break head-to-head play. The
launchd app job keeps exactly one process alive, and the deployer restarts it in place.

## Persistence (Postgres)

By default games live only in memory. Set `DATABASE_URL` to a Postgres DSN and
in-progress games, accounts/sessions, and finished-game history are written through to
Postgres and reloaded at boot, so a deploy or restart doesn't drop them. Postgres is
portable (local, Neon, Supabase, Docker) — nothing here is provider-specific. The macOS
setup runs a local Postgres 16 (provisioned by `deploy/macos/install.sh`).

The app just needs `DATABASE_URL` in its environment. No migration step (the schema is
created on boot). The macOS setup uses:

```
DATABASE_URL=postgres://cribbager:<password>@/cribbager?host=/usr/local/cribbager/run&sslmode=disable
```

For an external Postgres (Neon/Supabase, etc.) use `?sslmode=require` and a normal host.

Notes:
- **`sslmode`** is the usual snag with `lib/pq`: external Postgres wants
  `?sslmode=require`; a unix socket or loopback wants `sslmode=disable`.
- **Boot is fail-fast** — if `DATABASE_URL` is set but the database is unreachable, the
  app exits rather than silently running without persistence (check
  `/var/log/cribbager/app.log`; launchd restarts it, and the wrapper waits for Postgres
  first, so a slow boot order self-heals).
- The stores share **one connection-limited pool**, and every query has a short
  timeout, so a slow database degrades gracefully instead of wedging requests.

### Backups & durability

Postgres holds the only irreplaceable data — accounts and the permanent finished-game
history (`results`), which is never reaped. **Backups**: the macOS setup takes a nightly
age-encrypted `pg_dump` (retention, restore drill and off-machine copies are in
[`deploy/macos/docs/backups.md`](../deploy/macos/docs/backups.md)); a managed provider (Neon/Supabase) would instead give
point-in-time recovery — note its retention as your RPO.
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
BASE_URL=https://example.com         # so the emailed link points at your public site
```

Setup details: [`deploy/macos/docs/email.md`](../deploy/macos/docs/email.md). Locally, `docker compose` runs **Mailpit** instead (see [Development](development.md)).

## Configuration reference

In production these live in one env file (`/usr/local/etc/cribbager/env`, mode 600, see
`deploy/macos/env.example`); locally they are plain environment variables.

| var | meaning |
| --- | --- |
| `DATABASE_URL` | Postgres DSN. Unset → in-memory only (no persistence). |
| `SECURE_COOKIES` | Set (e.g. `1`) behind https so session cookies carry the `Secure` flag.  |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USER` / `SMTP_PASS` / `SMTP_FROM` | Reset-email transport. Unset → links are logged only. |
| `BASE_URL` | External origin for emailed links, e.g. `https://example.com`. Unset → derived from the request (honoring `X-Forwarded-Proto`). |
| `STATS_TOKEN` | If set, gates `GET /stats` **and `GET /metrics`** behind this bearer token. Unset → both are open. |
| `GOMEMLIMIT` | Soft heap ceiling. Optional soft heap ceiling. |
| `ADDR` / `WEB` | Listen address / static dir (mostly for dev). |

## Observability

- `GET /healthz` — liveness; returns `ok` whenever the process is up. (the cheap "is it up" probe.)
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

Nothing alerts out of the box; alert rules live wherever you scrape from. Scrape
`/metrics` with the `STATS_TOKEN` bearer token (it is public without one, so **set it in
production**). The two alerts worth wiring:

**1. Elevated 5xx rate.** Fires when more than ~5% of responses are 5xx over 5
minutes. Prometheus rule:

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

**2. High memory.** Alert on Go's reported system memory (tune the threshold to the
Mac's real baseline before paging on it):

```yaml
- alert: HighMemory
  expr: go_memstats_sys_bytes > 500e6
  for: 10m
  labels: { severity: page }
  annotations:
    summary: "Cribbager Go runtime memory above 500 MB"
```

Also watch that a backup actually ran: `/usr/local/cribbager/backups/.last-success`
should be under ~26 hours old.

## Governance

A self-hosted macOS install pulls `main` and deploys whatever is there, so **branch protection on `main`
(require the `go` / `web` / `lint` / `smoke` checks) is the deploy gate** — keep it on.
The deployer itself does not look at CI status.

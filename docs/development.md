# Development

Running and developing Cribbager locally. For deploying and operating it, see
[Deployment](deployment.md).

Needs **Go 1.25+**. Node is only needed for the optional headless smoke tests — the
game itself has no build step and no Node dependency.

## Running

One process, from the repo root:

```bash
go run ./cmd/cribbager-server     # serves the game + API on :8080
# open http://localhost:8080/
```

The homepage lets you start a game **vs the bot**, **invite a friend**, or **join**
one by link or game ID, and lists any games you have in progress. Each game lives at
its own `/game.html?game=<id>` URL, so a refresh resumes it.

No build: edit anything under `web/public/src/` and just refresh — the browser loads
the ES modules directly. (`ADDR` changes the port; `WEB` serves a different
directory.) Two standalone design tools are served alongside the game:
`/board-designer.html` and `/card-designer.html`.

### Play with a friend (human vs human)

"Invite a friend" creates an open game and shows a link plus a **game ID** — either
one is the credential (no accounts, no tokens). Share it; the other player opens the
link or pastes the ID on the homepage to join. The game runs over a Server-Sent-
Events stream, so each player sees the other's moves live, reconnects on refresh, and
is told if the opponent disconnects or leaves.

## Local stack (Postgres + Mailpit)

By default the server runs fully in memory — no database needed. To exercise the
durable paths (persistence, accounts, reset email) locally, `docker compose` brings up
Postgres and **Mailpit** (a local SMTP catcher):

```bash
docker compose up -d db   # just Postgres
DATABASE_URL=postgres://cribbager:cribbager@localhost:5432/cribbager?sslmode=disable \
  go run ./cmd/cribbager-server
```

Or run the whole stack (Postgres + Mailpit + app) with `docker compose up --build`;
reset emails then land in Mailpit's web UI at <http://localhost:8025>.

## Command-line client

The `cribbager` CLI plays in the terminal and exposes the scorers:

```bash
go run ./cmd/cribbager play                  # play a game vs the bot
go run ./cmd/cribbager score 5C 5D 5H JS 5S  # score a hand + cut
go run ./cmd/cribbager peg   5H 5S 5C        # score a pegging count-sequence
```

## Tests

```bash
go test ./...                                    # engine, scorers, bots, server
go test ./internal/scoring/... -run Exhaustive   # exhaustive scoring proof (slower)
npm --prefix web test                            # web client unit tests

# Full-game UI smoke tests (headless Chrome; need a running server + puppeteer-core):
npm --prefix web install                         # first time only
go run ./cmd/cribbager-server &
SMOKE_URL=http://localhost:8080/ npm --prefix web run playthrough     # vs the bot
SMOKE_URL=http://localhost:8080/ npm --prefix web run playthrough-mp  # human vs human

# Postgres integration tests (self-skip unless a database is reachable):
docker compose up -d db
TEST_DATABASE_URL=postgres://cribbager:cribbager@localhost:5432/cribbager?sslmode=disable \
  go test ./internal/server/ -run TestPg
```

CI runs `-short` on PRs (skipping the multi-million-iteration exhaustive proofs) and
the full suite on `main`; the smoke tests also run in CI in headless Chrome.

### Linting

golangci-lint (config in `.golangci.yml`) and ESLint (`web/eslint.config.js`), both
run in CI on every PR:

```bash
golangci-lint run ./...
npm --prefix web run lint
```

## Developing the bot

One bot ships — the `champion`. To improve it, add a challenger in `internal/bot/lab`
(copy `candidate.go`), then measure it against the champion over duplicate deals —
promote only when the paired-margin CI clears zero:

```bash
CHALLENGE=candidate go test ./internal/bot/lab -run ChallengerVsChampion -v
```

The bot's exact crib-value table is regenerated with `go generate ./internal/bot/eval`.

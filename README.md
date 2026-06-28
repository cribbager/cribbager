# Cribbager

A cribbage game for the browser — play a hand against the bot, or against a friend
over a shared link. A Go server runs the game, the bot, and the human-vs-human
session; the browser client is plain JavaScript (ES modules, **no build step**) that
the server serves directly.

**Live at https://cribbager.org.**

## What's inside

- An **event-sourced cribbage engine** with exhaustively-proven scoring.
- A single strong **bot** (the "champion").
- Real-time **human-vs-human** play over Server-Sent Events — live moves, reconnect on
  refresh, opponent-left notices, and rematch.
- Optional **accounts** (username/password, password reset by email) with per-player
  **game history + stats**.
- **Durable Postgres persistence**, deployed on fly.io with **CI auto-deploy** on every
  merge to `main`.

## Quick start

One process, from the repo root (needs **Go 1.25+**):

```bash
go run ./cmd/cribbager-server     # serves the game + API on :8080
# open http://localhost:8080/
```

Start a game vs the bot, invite a friend, or join one by link/ID. There's no build
step — edit anything under `web/public/src/` and just refresh.

## Documentation

- **[Development](docs/development.md)** — running locally, the test suite, the CLI,
  linting, and improving the bot.
- **[Deployment](docs/deployment.md)** — fly.io deploy + CI/CD, Postgres persistence &
  backups, password-reset email, and the full configuration reference.

## Layout

```
cmd/        Go binaries: the server and the command-line client
internal/   the engine — scoring, game rules, the bot, the HTTP server
web/        the browser client (vanilla JS, no build), served by the server
docs/       development & deployment guides
```

## License

Copyright © 2026 The Cribbager Authors.

Licensed under the **GNU Affero General Public License, version 3** (or, at your
option, any later version) — see [LICENSE](LICENSE). In short: you're free to use,
study, modify, and share it, but if you run a modified version as a network service,
you must make your source available to its users. (Same license as
[Lichess](https://lichess.org).)

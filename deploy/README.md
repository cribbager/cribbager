# Deployment options

| Option | Where | Choose it when |
| --- | --- | --- |
| macOS launchd + Cloudflare Tunnel | [`deploy/macos/`](macos/README.md) | You want to host it yourself on a Mac that stays on, with your own domain, Postgres, auto-deploy and backups. |
| Docker Compose | `docker-compose.yml` in the repo root | You are developing locally (app, Postgres, Mailpit). Not a production setup. |

Whatever you pick, run exactly one app instance: game state lives in memory.

`fly.toml` and the `Dockerfile` in the repo root are kept for reference and local container use; fly.io is no longer used.

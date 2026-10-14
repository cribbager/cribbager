# Operating

Read this for day-to-day running: deploys, logs, secrets, upgrades, uninstall.

## Deploys

Every 60s `deploy.sh` fetches `main` from GitHub (no credentials). If `main` moved, it builds
`releases/<sha>/`, repoints `current`, restarts the app (games reload from Postgres; SSE
clients reconnect), and polls `/readyz`. If that fails it points `current` back and does not
retry that commit; the next commit is tried normally. It only fast-forwards, and CI status
is not consulted, so protect `main` on GitHub.

**Docs-only skip.** The app is rebuilt only when `cmd/`, `internal/` (not tests or
`testdata/`), `web/public/`, `go.mod` or `go.sum` changed. Commits touching only docs,
`deploy/`, `.github/` or tests are logged as skipped; `current` may sit behind `main` until
the next app change. When git cannot compare, it builds.

```bash
D="sudo -u _cribbager /usr/local/libexec/cribbager/deploy.sh"
$D                     # run one deploy cycle now
$D status              # current, previous, failed, held, skipped
$D rollback            # previous release (or: rollback <full-sha>); also holds auto-deploys
$D resume              # follow main again
```

To skip a bad commit for good, revert it on `main`.

## Logs and jobs

```bash
tail -f /var/log/cribbager/app.log     # also postgres, cloudflared, deployer, backup
launchctl print system/org.cribbager.app | grep -E 'state|pid|exit'
sudo launchctl kickstart -k system/org.cribbager.app            # restart a job
sudo launchctl bootout system/org.cribbager.cloudflared         # stop (until install.sh or reboot)
sudo launchctl bootstrap system /Library/LaunchDaemons/org.cribbager.cloudflared.plist
```

Only `deployer.log` and `backup.log` rotate. If another log grows large, move it aside,
recreate it with `sudo install -o _cribbager -g wheel -m 640 /dev/null <file>`, and kickstart
that job.

## Rotate secrets

Edit `/usr/local/etc/cribbager/env`, then restart the job named.

| Secret | How |
| --- | --- |
| `STATS_TOKEN` | `openssl rand -hex 24`; restart `app` |
| `SMTP_PASS` | New key at your provider; restart `app`; delete the old key |
| `TUNNEL_TOKEN` | Tunnel page > **Refresh token** in Cloudflare; restart `cloudflared` |
| Database password | New hex password in `DATABASE_URL`; `sudo deploy/macos/install.sh`; restart `app` |
| age key | New pair ([backups.md](backups.md)); replace `AGE_RECIPIENT`; keep the old private key for old backups |

## Upgrades

- **Scripts**: changes under `deploy/macos/` are not deployed automatically (they are
  root-owned in `libexec`). After one lands: `git pull && sudo deploy/macos/install.sh`.
- **Go**: `brew upgrade go`; the next deploy uses it. If `go.mod` needs a newer Go, builds
  fail (no toolchain downloads) and the running release stays up.
- **Postgres**: `brew upgrade postgresql@16`, then restart the `postgres` job. A major
  version change needs a dump and restore, which this repo does not cover.

## Developing on the same Mac

Production never reads your working directories; the deployer has its own repo cache and
the launchd scripts are copies. Keep your clone under your home directory. Watch for:

- **Port.** Production owns `127.0.0.1:8080`. Run dev elsewhere:
  `ADDR=127.0.0.1:8081 go run ./cmd/cribbager-server`.
- **`install.sh` from a dirty checkout** installs your uncommitted `deploy/macos/` edits.
  Run it only from a clean, current `main`.
- **Database.** Never point a dev `DATABASE_URL` at the production database. Use a separate
  one, or none (the server runs in memory).
- **Merging is deploying.** App changes on `main` go live within about a minute.

## Uninstall

```bash
for j in deployer backup cloudflared app postgres; do sudo launchctl bootout system/org.cribbager.$j; done
sudo rm /Library/LaunchDaemons/org.cribbager.*.plist /etc/newsyslog.d/cribbager.conf
sudo rm -r /usr/local/libexec/cribbager /var/log/cribbager
# Data and secrets; only when you are sure:
sudo chmod -R u+w /usr/local/cribbager && sudo rm -r /usr/local/cribbager /usr/local/etc/cribbager
sudo dscl . -delete /Users/_cribbager && sudo dscl . -delete /Groups/_cribbager
```

Then delete the tunnel in Cloudflare and the API key at your email provider.

# Self-hosting Cribbager on a Mac Studio (launchd + Cloudflare Tunnel)

Runs the Go server as **one process** under launchd on an always-on Apple-silicon Mac,
with Postgres on the same machine, published at https://cribbager.org through a
Cloudflare Tunnel. No Docker, no inbound ports, no public IP. Deploys are pulled by the
Mac itself from the public GitHub repo; GitHub never connects to the Mac.

```
 browser --https--> Cloudflare edge <--outbound-- cloudflared --> 127.0.0.1:8080 app --unix socket--> Postgres
                                                     (all launchd system daemons, user _cribbager)
 deployer (every 60s): git fetch -> go build -> flip `current` -> restart -> /readyz -> (rollback)
 backup   (03:15)    : pg_dump | gzip | age(public key) -> local dir
```

## What gets installed where

| Path | Owner | Purpose |
| --- | --- | --- |
| `/usr/local/cribbager/` | `_cribbager` | prefix: everything the service writes |
| `  repo/` | `_cribbager` | bare git cache of the public repo (fetch only) |
| `  releases/<sha>/` | `_cribbager` | `cribbager-server` + `web/public` + `REVISION`; last 5 kept |
| `  current` | `_cribbager` | symlink to the active release |
| `  pgdata/`, `run/` | `_cribbager` (700) | Postgres data dir and unix socket |
| `  backups/` | `_cribbager` (700) | encrypted dumps |
| `  state/`, `cache/`, `home/` | `_cribbager` | deploy state/lock, Go caches, HOME |
| `/usr/local/libexec/cribbager/` | `root` | the scripts launchd runs (service user cannot edit them) |
| `/usr/local/etc/cribbager/env` | `_cribbager`, 600 | all secrets and config |
| `/var/log/cribbager/*.log` | `_cribbager` | `app`, `postgres`, `cloudflared`, `deployer`, `backup` |
| `/Library/LaunchDaemons/org.cribbager.{app,postgres,cloudflared,deployer,backup}.plist` | `root` | the jobs |

The service account `_cribbager` is a hidden, non-admin user with shell `/usr/bin/false`
and no password. The jobs are **system** LaunchDaemons with `UserName=_cribbager`, so they
start at boot with nobody logged in.

---

## Setup, in order

### 1. Cloudflare dashboard (tunnel)

cribbager.org is already on Cloudflare DNS. You do **not** need Zero Trust (its "Get
Started" onboarding) for this: tunnels live under the main dashboard's **Networking** menu.
Zero Trust is only needed for the optional Access login policy in step 5. The dashboard
layout changes now and then; if a label differs, look for the same thing nearby.

1. Cloudflare dashboard, **Networking > Tunnels > Create a tunnel**. Connector type
   **Cloudflared**, name it `cribbager-studio`.
2. On the "install connector" screen choose **macOS** and copy the **token** only: the long
   `eyJ...` string after `cloudflared service install` (a Docker tab shows the same string
   after `--token`). **Do not run** that command; our launchd job runs cloudflared instead.
   Keep the token private (it lets anyone run your tunnel). It goes in the Studio's env
   file as `TUNNEL_TOKEN`; never in git, chat or a PR.
3. Open the tunnel's **Routes** tab > **Add route > Published application**: leave the
   subdomain empty, choose the domain `cribbager.org`, and set the service URL to
   `http://127.0.0.1:8080`. Save. (Older dashboards call this the **Public hostname** tab.)
4. Saving creates a proxied (orange cloud) CNAME for `cribbager.org` pointing at
   `<tunnel-id>.cfargotunnel.com`; check the DNS tab. If the apex already has an
   `A`/`AAAA`/`CNAME` pointing at fly.io, delete **only those** first, or the route cannot
   create its record. **Keep every other record**, especially Resend's email records
   (`resend._domainkey`, and the `MX` and SPF `TXT` on `send`) and `MX`/`TXT` records for
   mail. Deleting them breaks email, and only email. Optionally add a second route for
   `www.cribbager.org` to the same service if you want `www` to work.
5. `/metrics` and `/stats` exposure: they are gated by `STATS_TOKEN` (bearer token), which
   install.sh generates. That is sufficient on its own. Optional extra layer: Zero Trust >
   Access > Applications > Add > Self-hosted, domain `cribbager.org`, paths `/metrics` and
   `/stats`, policy allowing only your email (this needs the Zero Trust onboarding; skip it
   if it asks for a plan or card you do not want). If you later scrape from a machine that
   cannot do the Access login, use a service token or just rely on `STATS_TOKEN`.
6. Leave Cloudflare's SSL mode at its default; the hop from cloudflared to the app is local
   plain HTTP by design. Cloudflare proxies SSE fine; the app sends `: ping` every 25s, well
   under Cloudflare's 100s idle limit.
7. Until cloudflared runs on the Studio the site shows a Cloudflare error (e.g. 1033, tunnel
   not connected). That is expected; the tunnel shows **Healthy** once the install is done.

### 2. Resend (password-reset email)

The `cribbager.org` domain was set up in Resend for the earlier fly.io deployment, with DKIM
(`resend._domainkey`) and `send`-subdomain MX and SPF records in Cloudflare DNS. Check they
are still there (`dig +short TXT resend._domainkey.cribbager.org`); if a DNS cleanup removed
them, re-add the records Resend lists (copy the values from the Resend dashboard; they are
not reproducible from here) with the proxy set to **DNS only**, and click **Verify**.

1. Sign in at https://resend.com and open **Domains**. Confirm `cribbager.org` shows
   **Verified**. If it doesn't, or the account is new, choose **Add Domain** (`cribbager.org`,
   or a sending subdomain such as `mail.cribbager.org`), add the SPF `TXT`, DKIM and MX
   records Resend shows to **Cloudflare DNS** with the proxy status **DNS only** (grey
   cloud), and click **Verify**; it can take minutes to an hour.
2. **API Keys > Create API Key**, permission "Sending access", restricted to that domain.
   Resend never shows an existing key again, so create a new one (the old fly key is gone
   with the fly app; delete it in Resend if you find it listed). Copy the `re_...` key now
   (shown once). It goes in the env file as `SMTP_PASS` below.
3. Pick `SMTP_FROM` on the verified domain, e.g. `Cribbager <noreply@cribbager.org>`.

Limits: Resend's free tier has been 3,000 emails/month and 100/day, and sending **pauses at
the limit rather than billing you**. Plans change; **confirm the current limits (including
how many domains the free tier allows) on Resend's pricing page** before relying on this.
Password-reset volume for this app is tiny.

### 3. age key (backups)

Generate the keypair on a **different machine** (your laptop), not the Studio:

```bash
brew install age
age-keygen -o cribbager-backup-key.txt     # prints "Public key: age1..."
```

Store `cribbager-backup-key.txt` (the private key) in your password manager and/or offline.
Only the public `age1...` string goes on the Studio. **Without the private key the backups
are unreadable, including by you.**

### 4. On the Mac Studio

```bash
# Prerequisites (as your normal admin user)
brew install postgresql@16 cloudflared age go git        # Go must be >= the version in go.mod
git clone https://github.com/cribbager/cribbager.git ~/cribbager-deploy-src   # any location
cd ~/cribbager-deploy-src

# Check prerequisites without changing anything
deploy/macos/install.sh --check

# Install (creates user, dirs, plists; starts Postgres, app, deployer)
sudo deploy/macos/install.sh
```

install.sh refuses to continue, listing everything missing, if macOS is older than 13, the
machine is not arm64, or `postgresql@16`, `cloudflared`, `age`, `git` or a new-enough Go are
absent. It is idempotent: re-run it after editing the env file or updating the scripts.

First run creates `/usr/local/etc/cribbager/env` with a **generated** DB password and
`STATS_TOKEN`. Edit it (placeholders are `CHANGE_ME`):

```bash
sudo -e /usr/local/etc/cribbager/env          # or: sudo nano /usr/local/etc/cribbager/env
```

Fill in `SMTP_PASS` (Resend key), `SMTP_FROM`, `TUNNEL_TOKEN` (Cloudflare), `AGE_RECIPIENT`
(public key). Make sure it is still `600` and owned by `_cribbager`
(`ls -l /usr/local/etc/cribbager/env`; install.sh re-asserts this). Then **re-run**
`sudo deploy/macos/install.sh`: it starts cloudflared and the backup job once their secrets
exist, and tells you what is still missing. Then restart the app so it picks up SMTP:
`sudo launchctl kickstart -k system/org.cribbager.app`.

The deployer builds the first release within a minute or two (it downloads Go modules the
first time): `tail -f /var/log/cribbager/deployer.log`.

> The Go toolchain and Homebrew binaries must be world-executable, which is Homebrew's
> default. install.sh verifies `_cribbager` can run `go`.

### 5. Harden macOS (checklist)

- [ ] **Firewall on**: System Settings > Network > Firewall. Nothing needs to accept inbound
      connections (cloudflared dials out; the app binds loopback).
- [ ] **FileVault on** (disk encryption). Note: after a power loss FileVault stops at the
      pre-boot login screen until someone unlocks it, so services stay down. For a headless
      Studio, decide consciously: leave it on and unlock after outages, or accept the
      trade-off of off. (Postgres data and the env file are only as protected as the disk.)
- [ ] **Automatic security updates** on (Software Update > Automatic updates: install macOS
      updates and security responses/system files). Reboots are fine: everything restarts.
- [ ] **Remote Login (SSH) off** unless you need it; if you do, key-only, your admin user
      only. `sudo systemsetup -setremotelogin off`. Screen Sharing off unless needed.
- [ ] **Never sleep the system** (the display may sleep): when the Mac sleeps, the app,
      Postgres and cloudflared all stop and the site is down. Run
      `sudo pmset -a sleep 0 disksleep 0 displaysleep 15 womp 1 autorestart 1` and check
      `pmset -g` (`sleep 0`, `autorestart 1`, `womp 1`). The equivalent System Settings >
      Energy toggles are **Prevent automatic sleeping when the display is off**, **Wake for
      network access** and **Start up automatically after a power failure**.
- [ ] **Do not let macOS restart by itself unattended.** With FileVault on a reboot stalls
      at the unlock screen; set updates to download and notify, and pick your own reboot time.
- [ ] `_cribbager` is not an admin and has no GUI login (`id _cribbager`; it must not list
      `admin`). Keep your own admin account protected with a strong password and Touch ID.
- [ ] Keep Homebrew updated (`brew update && brew upgrade`, restart services after
      upgrading `postgresql@16`, which stays on major version 16 so the data dir stays valid).
- [ ] Auto-login disabled; screen lock enabled.

---

## Verify

```bash
# Local (on the Studio)
curl -fsS http://127.0.0.1:8080/healthz ; echo
curl -fsS http://127.0.0.1:8080/readyz  ; echo        # "ready" means Postgres answered
launchctl print system/org.cribbager.app | grep -E 'state|pid'

# Public
curl -fsS https://cribbager.org/readyz ; echo
curl -s -o /dev/null -w '%{http_code}\n' https://cribbager.org/metrics            # expect 401
TOKEN=$(sudo awk -F= '/^STATS_TOKEN=/{print $2}' /usr/local/etc/cribbager/env)
curl -fsS -H "Authorization: Bearer $TOKEN" https://cribbager.org/stats ; echo
```

`/metrics` and `/stats` returning 401 without the token proves `STATS_TOKEN` is active. If
they return data without it, the token is not set: fix the env file and restart the app.
**`STATS_TOKEN` must be set in production**; unset, both endpoints are public.

### SSE check (>3 minutes, through Cloudflare)

```bash
R=$(curl -fsS -X POST https://cribbager.org/games -H 'Content-Type: application/json' -d '{"mode":"open"}')
ID=$(printf '%s' "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin)["game_id"])')
TK=$(printf '%s' "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin)["player_token"])')
curl -N -sS "https://cribbager.org/games/$ID/stream?token=$TK" | while IFS= read -r l; do printf '%s  %s\n' "$(date +%T)" "$l"; done
```

Leave it for **more than 3 minutes**. Expect an initial `data:` line (the roster) and then a
`: ping` line every ~25 seconds (the server's SSE heartbeat). Gaps much longer than 25s, or
the connection dropping, mean something between you and the app is buffering or timing out.

### End-to-end smoke test (needs Node and Chrome on your laptop)

```bash
SMOKE_URL=https://cribbager.org/ npm --prefix web run playthrough-mp
```

Run it from a checkout of the repo with `npm ci --prefix web` done (set `CHROME=` to a Chrome
binary if it cannot find one). It plays a full human-vs-human game over the public URL.

### Reset email

On the homepage use **Forgot password?** with a registered account's email; a message from
`SMTP_FROM` should arrive. If not: `grep -i -E 'smtp|email|reset' /var/log/cribbager/app.log`
and the Resend dashboard logs.

---

## Operating

### Continuous deployment

Nothing to do. Every ~60s `deploy.sh` (as `_cribbager`) fetches `main` from GitHub over https
without credentials. If `main` moved it: builds `releases/<sha>/` with `go build`
(`CGO_ENABLED=0`, `GOTOOLCHAIN=local`) from `git archive` of that commit, copies
`web/public` alongside, repoints `current`, SIGTERMs the app (launchd relaunches it from the
new `current`; games reload from Postgres, SSE clients reconnect), and polls `/readyz`
(30 tries, 2s apart). On failure it points `current` back, restarts, logs `!!!` lines, and
**will not retry that commit**; the next commit on `main` is tried normally. A lock
prevents overlapping runs. It only fast-forwards: a rewritten `main` is refused and logged.

A commit that changes nothing the running app serves is **not** built or restarted: the
app is rebuilt only when `cmd/`, `internal/` (excluding `*_test.go` and `testdata/`),
`web/public/`, `go.mod` or `go.sum` differ from the running release. Docs, `README`,
`deploy/`, `.github/` and test-only commits are logged as skipped ("only docs/CI/deploy
files changed") and recorded in `state/skipped`, so `current` can sit behind `main` until the
next app change, which then deploys the tip including the skipped commits. `deploy.sh
status` shows the `skipped:` commit. When in doubt (git cannot compare), it builds.
CI status is not consulted: protect `main` on GitHub (required checks) so only green
commits can land.

Deploy-script changes are *not* auto-applied (they live root-owned in `libexec`). After a
merge that changes `deploy/macos/`, run `git pull && sudo deploy/macos/install.sh`.

### Deploy by hand / inspect

```bash
sudo -u _cribbager /usr/local/libexec/cribbager/deploy.sh          # run one deploy cycle now
sudo -u _cribbager /usr/local/libexec/cribbager/deploy.sh status   # current/previous/failed/hold
tail -f /var/log/cribbager/deployer.log
```

(`sudo -u _cribbager` works although the account has no shell: sudo runs the command
directly.)

### Developing on the Studio

Production never reads your working directories, so developing on the same Mac is safe. The
deployer uses its own bare repo cache (`/usr/local/cribbager/repo`) and builds into
`/usr/local/cribbager/releases/`; the scripts launchd runs are copies in
`/usr/local/libexec/cribbager`. Keep your clone under your own home directory, never under
`/usr/local/cribbager`. The places a dev setup can still interfere:

- **Port 8080.** Production binds `127.0.0.1:8080`; a dev server on the default `:8080`
  collides with it. Run dev on another port: `ADDR=127.0.0.1:8081 go run ./cmd/cribbager-server`.
- **Re-running `install.sh`** installs the scripts from the checkout it is run in, so
  uncommitted edits to `deploy/macos/` would go live. Run it only from a clean, up-to-date
  `main`.
- **Database.** The dev `docker compose` Postgres (port 5432) does not clash: production
  Postgres uses a unix socket only. Never point a dev `DATABASE_URL` at the production
  database; use a separate one (or none: the server runs in-memory without it).
- **CPU and memory.** Builds and `go test` (the exhaustive scoring proofs especially)
  compete with production for the machine; fine for a prototype, but expect the occasional
  slow response while a big test run is going.
- **Merging is deploying.** Whatever lands on `main` and affects the app is built and
  restarted within about a minute.

### Roll back

```bash
D="sudo -u _cribbager /usr/local/libexec/cribbager/deploy.sh"
$D rollback            # to the previous release; or: $D rollback <full-sha>
$D resume              # later: follow main again
```

`rollback` also **holds** automatic deploys (otherwise the deployer would roll forward again
within a minute). Releases come from `status`. To permanently skip a bad commit, revert it
on `main` (that is a new commit, so it deploys normally).

### Logs

```bash
tail -f /var/log/cribbager/app.log          # app (also: postgres.log, cloudflared.log, deployer.log, backup.log)
launchctl print system/org.cribbager.app    # state, last exit code
```

launchd keeps the long-running jobs' log files open, so only `deployer.log` and `backup.log`
are rotated automatically (newsyslog). `app.log`, `postgres.log`, `cloudflared.log` grow
slowly; when large, `sudo launchctl kickstart -k system/org.cribbager.<job>` after moving
the file aside (`sudo mv`, then `sudo install -o _cribbager -g wheel -m 640 /dev/null <file>`).

### Restart / stop a job

```bash
sudo launchctl kickstart -k system/org.cribbager.app          # restart
sudo launchctl bootout   system/org.cribbager.cloudflared      # stop (until next install.sh / reboot)
sudo launchctl bootstrap system /Library/LaunchDaemons/org.cribbager.cloudflared.plist
```

### Rotate secrets

Edit `/usr/local/etc/cribbager/env`, then:

- **STATS_TOKEN**: `openssl rand -hex 24`, edit, restart the app.
- **Resend key** (`SMTP_PASS`): create a new key in Resend, edit, restart the app, delete the old key.
- **Database password**: put a new alphanumeric password in `DATABASE_URL` (`openssl rand -hex 24`),
  then `sudo deploy/macos/install.sh` (it runs `ALTER ROLE`) and restart the app.
- **Tunnel token**: in Cloudflare Networking > Tunnels > your tunnel > **Refresh token** (or delete and recreate the tunnel)
  (or recreate), edit `TUNNEL_TOKEN`, then `sudo launchctl kickstart -k system/org.cribbager.cloudflared`.
- **Backup key**: generate a new age keypair (step 3), replace `AGE_RECIPIENT`; **keep the
  old private key** to read older backups.

### Backups and restore

`backup.sh` runs nightly at 03:15 as `_cribbager`: `pg_dump --no-owner --no-acl | gzip | age -r <public key>`
into `/usr/local/cribbager/backups/cribbager-<UTC>.sql.gz.age` (mode 600), keeps at least
the newest 7 and deletes anything older than 30 days beyond that (`BACKUP_KEEP_MIN`,
`BACKUP_KEEP_DAYS`), and records `.last-success`. Run one now:

```bash
sudo -u _cribbager /usr/local/libexec/cribbager/backup.sh && ls -l /usr/local/cribbager/backups
```

**Restore drill (do this once now, then every few months).** Bring the private key from
off-machine; this restores into a *scratch* database so production is untouched:

```bash
B=$(ls -t /usr/local/cribbager/backups/cribbager-*.age | head -1)
install -m 600 /dev/null /tmp/age-key.txt && cat > /tmp/age-key.txt      # paste the private key, Ctrl-D
PGB=/opt/homebrew/opt/postgresql@16/bin
PSQL="sudo -u _cribbager $PGB/psql -X -h /usr/local/cribbager/run -U postgres -v ON_ERROR_STOP=1"
$PSQL -d postgres -c 'CREATE DATABASE restore_test OWNER cribbager'
{ echo 'SET ROLE cribbager;'; sudo cat "$B" | /opt/homebrew/bin/age -d -i /tmp/age-key.txt | gunzip; } | $PSQL -d restore_test
$PSQL -d restore_test -c '\dt' -c 'SELECT count(*) FROM users'    # tables present, plausible counts
$PSQL -d postgres -c 'DROP DATABASE restore_test'
rm -P /tmp/age-key.txt
```

(Restoring under `SET ROLE cribbager` makes the app role own the tables; the dump has no
ownership statements. Tables are `users`, `sessions`, `password_resets`, `games`, `results`.)

**Real restore (disaster):** stop the app, replace the database, restore, start it.

```bash
sudo launchctl bootout system/org.cribbager.app
$PSQL -d postgres -c 'DROP DATABASE cribbager' -c 'CREATE DATABASE cribbager OWNER cribbager'
{ echo 'SET ROLE cribbager;'; sudo cat "$B" | /opt/homebrew/bin/age -d -i /tmp/age-key.txt | gunzip; } | $PSQL -d cribbager
sudo launchctl bootstrap system /Library/LaunchDaemons/org.cribbager.app.plist
```

On a rebuilt machine: run install.sh first (creates the empty role/database), then restore as above.

**Off-machine copies.** The Studio's own disk is a single point of failure, and a
compromised `_cribbager` could delete local backups. Copy the directory elsewhere. The
files are already encrypted, so any destination is acceptable. Options, none implemented
here: (a) set `BACKUP_POST_CMD` in the env file to a command run after each successful
backup (`$BACKUP_FILE` is the new file), e.g. an `rsync`/`rclone` call to a host or bucket
you control with a credential stored in the service user's home (`/usr/local/cribbager/home`);
(b) point Time Machine or another backup tool at `/usr/local/cribbager/backups` (but **not**
at `pgdata`, which install.sh excludes from Time Machine because a file-level copy of a
running database is not restorable); (c) pull the directory from another machine.
Monitor that `backups/.last-success` is recent.

### Upgrades

- **Go**: `brew upgrade go`; the next deploy uses it. If `go.mod` bumps beyond the installed
  Go, builds fail (`GOTOOLCHAIN=local`; no silent toolchain downloads) and the previous
  release keeps running; upgrade Go.
- **Postgres**: `brew upgrade postgresql@16` is a minor upgrade; then
  `sudo launchctl kickstart -k system/org.cribbager.postgres`. A major version change needs a
  dump/restore; not covered here.

### Uninstall

```bash
for j in deployer backup cloudflared app postgres; do sudo launchctl bootout system/org.cribbager.$j; done
sudo rm /Library/LaunchDaemons/org.cribbager.*.plist /etc/newsyslog.d/cribbager.conf
sudo rm -r /usr/local/libexec/cribbager /var/log/cribbager
# Data (database, releases, backups) and secrets; only when you are sure:
sudo chmod -R u+w /usr/local/cribbager && sudo rm -r /usr/local/cribbager /usr/local/etc/cribbager
sudo dscl . -delete /Users/_cribbager && sudo dscl . -delete /Groups/_cribbager
```

Also delete the tunnel in Cloudflare and the API key in Resend.

---

## Security

Trust boundaries, outermost first:

1. **Internet to Cloudflare.** TLS, DDoS and WAF are Cloudflare's. The Mac has no inbound
   ports and no public IP: `cloudflared` only dials out. The tunnel token is the credential
   that lets anyone terminate traffic for cribbager.org, so it is in the env file (600) and
   handed to cloudflared via its environment, never argv.
2. **Cloudflare to the app.** The tunnel delivers to `127.0.0.1:8080`; the app is forced to
   bind loopback (`run-app.sh`), so nothing on the LAN can reach it. Anything that can reach
   loopback on the Mac (any local user or process) can talk to the app, `/healthz` and
   `/readyz` included; `/stats` and `/metrics` need `STATS_TOKEN`.
3. **App to Postgres.** Unix socket only (`listen_addresses` empty, socket dir mode 700).
   `pg_hba.conf` allows exactly two things: the OS service user as superuser `postgres`
   (peer) and role `cribbager` (scram-sha-256) on database `cribbager`. The `cribbager` role
   is not superuser, cannot create roles or databases, and owns only its database
   (`PUBLIC` has no access). This limits what an SQL-injection-level bug can do. It does
   **not** separate the OS account: anything running as `_cribbager` can read the env file
   and Postgres' data directory.
4. **GitHub to the Mac (deploys).** There is no GitHub credential on the Mac, no self-hosted
   runner, and no inbound path. The Mac pulls the public repo. The trust you extend is:
   **whatever lands on `main` is built and run as `_cribbager`.** Mitigations: branch
   protection with required green CI and review on `main`; builds are `CGO_ENABLED=0`
   (no C compiler runs), `go.sum` verifies dependencies, no toolchain auto-download,
   fast-forward-only, and `_cribbager` has no sudo and no admin rights, so a malicious
   commit can read the app's secrets and data and make outbound connections, but cannot
   take over the machine without a separate local privilege-escalation bug. Treat commit
   access to `main` as equivalent to shell access as `_cribbager`.
5. **Root-owned scripts.** The scripts launchd executes live in `/usr/local/libexec/cribbager`,
   owned by root, so the service user (and therefore a malicious release) cannot rewrite its
   own deployer or the backup job. Changing them needs `sudo install.sh`.
6. **Secrets.** One env file, `_cribbager`:`wheel`, mode 600, outside git; `env.example` is
   the only thing committed. The DB password is passed to `pg_dump` via the environment, not
   argv (macOS `ps` shows other users' arguments). Backups are encrypted to a public key; the
   private key never touches the Studio.
7. **Single instance.** Run exactly one app process. Game state is in memory and SSE streams
   must reach the same process; never add a second instance or a load balancer.

Known gaps worth knowing:

- If FileVault is off the disk is unencrypted at rest; if on, an unattended reboot waits
  for an unlock (see the hardening list).
- Local backups share fate with the machine until you copy them off.

## Tests

`deploy/macos/test/` holds harnesses that run in scratch directories (no sudo, no launchd):
`test-deploy.sh` (release, symlink, rollback, lock, prune, fast-forward-only, plus a live
run with the real binary), `test-backup.sh` (needs `age`), `test-lib-env.sh`.

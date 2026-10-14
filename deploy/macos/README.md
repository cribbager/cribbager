# Self-host Cribbager on a Mac

One Go process, Postgres and a Cloudflare Tunnel, all run by launchd on a Mac that stays on.
No Docker, no inbound ports, no public IP. The Mac pulls new releases from GitHub itself;
GitHub never connects to it.

```
browser --https--> Cloudflare <--outbound-- cloudflared --> app 127.0.0.1:8080 --socket--> Postgres
deployer (every 60s): git fetch main -> go build -> restart -> /readyz -> roll back on failure
backup   (03:15)    : pg_dump | gzip | age -> local directory
```

## You need

- An Apple-silicon Mac on macOS 13+ that stays on, with an admin account.
- A domain whose DNS is on Cloudflare (free plan is enough).
- [Homebrew](https://brew.sh), and a few minutes at a terminal.
- Optional: an SMTP account for password-reset email ([email.md](docs/email.md)).

## 1. Create the Cloudflare tunnel

In the Cloudflare dashboard (Zero Trust is not needed; labels move around, look nearby):

1. **Networking > Tunnels > Create a tunnel**, connector **Cloudflared**, any name.
2. Pick **macOS** and copy only the token (the long `eyJ...` string). Do not run the install
   command it shows. The token is a credential: keep it out of git and chat.
3. **Routes > Add route > Published application**: hostname `<your-domain>` (or a
   subdomain such as `play.<your-domain>`), service `http://127.0.0.1:8080`.
4. If that hostname already has an `A`/`AAAA`/`CNAME` record, delete just that record so the
   route can create its own. Leave mail records alone.

The site shows a Cloudflare error (such as 1033) until the tunnel connects in step 4.

## 2. Install prerequisites

```bash
brew install postgresql@16 cloudflared age go git
```

Go must be at least the version in `go.mod`. Postgres stays on major version 16.

## 3. Clone and install

```bash
git clone https://github.com/cribbager/cribbager.git ~/cribbager-src && cd ~/cribbager-src
deploy/macos/install.sh --check                       # lists anything missing; changes nothing
sudo DOMAIN=<your-domain> deploy/macos/install.sh     # use the hostname from step 1
```

This creates a hidden `_cribbager` service user, the directories, five launchd jobs, a
Postgres cluster, and the env file `/usr/local/etc/cribbager/env` with a generated database
password and `STATS_TOKEN`. `DOMAIN` fills `BASE_URL` and `SMTP_FROM` in that new file; an
existing env file is never overwritten. It is safe to re-run at any time and prints what is
still to do.

## 4. Edit the env file

```bash
sudo -e /usr/local/etc/cribbager/env
```

| Key | Set it to |
| --- | --- |
| `TUNNEL_TOKEN` | The token from step 1. |
| `AGE_RECIPIENT` | Your age public key (`age1...`); see [backups.md](docs/backups.md). |
| `SMTP_*` | Optional. Leave `SMTP_HOST` empty to skip email; see [email.md](docs/email.md). |
| `BASE_URL`, `SMTP_FROM` | Already set from `DOMAIN`; correct them if you left `DOMAIN` out. |

Then re-run `sudo deploy/macos/install.sh`. It starts cloudflared and the backup job once
their secrets exist. If you changed SMTP or `BASE_URL`, also run
`sudo launchctl kickstart -k system/org.cribbager.app`.

The deployer builds the first release within a minute or two (it downloads Go modules the
first time): `tail -f /var/log/cribbager/deployer.log`.

## 5. Verify

```bash
deploy/macos/verify.sh <your-domain>            # add --sse for the ~40s streaming check
STATS_TOKEN=$(sudo sed -n 's/^STATS_TOKEN=//p' /usr/local/etc/cribbager/env) \
  deploy/macos/verify.sh <your-domain>          # also checks /stats and /metrics with the token
```

It prints PASS or FAIL per check: local `/healthz` and `/readyz`, the public URL, and that
`/stats` and `/metrics` are refused without the token. It never prints the token.

Finally, open `https://<your-domain>` and play a hand against the bot.

## Where things live

| Path | Contents |
| --- | --- |
| `/usr/local/cribbager/` | Releases, `current` symlink, Postgres data, backups (owner `_cribbager`) |
| `/usr/local/libexec/cribbager/` | The scripts launchd runs (root-owned) |
| `/usr/local/etc/cribbager/env` | All config and secrets (mode 600) |
| `/var/log/cribbager/` | `app`, `postgres`, `cloudflared`, `deployer`, `backup` logs |
| `/Library/LaunchDaemons/org.cribbager.*.plist` | The jobs |

## More

| When you need to | Read |
| --- | --- |
| Send password-reset email (Resend or any SMTP) | [docs/email.md](docs/email.md) |
| Deploy by hand, roll back, read logs, rotate secrets, upgrade, uninstall | [docs/operating.md](docs/operating.md) |
| Encrypt, copy and restore backups | [docs/backups.md](docs/backups.md) |
| Harden the Mac and understand the trust model | [docs/security.md](docs/security.md) |

Tests for the scripts, which need no sudo: `deploy/macos/test/test-*.sh`.

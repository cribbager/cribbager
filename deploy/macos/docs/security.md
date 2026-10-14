# Security

Read this to understand what you are trusting, and to harden the Mac.

## Trust boundaries

1. **Internet to Cloudflare.** TLS and DDoS protection are Cloudflare's. The Mac has no
   inbound ports; `cloudflared` only dials out. The tunnel token lets anyone terminate
   traffic for your domain, so it lives in the env file (mode 600) and reaches cloudflared
   through its environment, never argv.
2. **Cloudflare to the app.** The tunnel delivers to `127.0.0.1:8080`, and `run-app.sh`
   forces the app onto loopback, so the LAN cannot reach it. Any local process can; `/stats`
   and `/metrics` need `STATS_TOKEN`. Set it: unset, both are public.
3. **App to Postgres.** Unix socket only. `pg_hba.conf` allows the service user as
   superuser `postgres` (peer) and role `cribbager` (password) on its own database. That role
   cannot create roles or databases, which limits what a SQL bug can do. It does not
   separate the OS account: anything running as `_cribbager` can read the env file and the
   data directory.
4. **GitHub to the Mac.** The Mac pulls the public repo; it holds no GitHub credential and
   runs no self-hosted runner. **Whatever lands on `main` is built and run as `_cribbager`**,
   so treat commit access to `main` as shell access to that user. Protect `main` with
   required CI and review. Builds are `CGO_ENABLED=0` with `go.sum` verification, fast-forward
   only, and `_cribbager` has no sudo or admin rights.
5. **Root-owned scripts.** What launchd runs lives in `/usr/local/libexec/cribbager`,
   so a bad release cannot rewrite its own deployer. Changing them needs `sudo install.sh`.
6. **Secrets.** One env file, outside git. The database password reaches `pg_dump` through
   the environment, not argv. Backups are encrypted to a public key; the private key never
   touches the Mac.
7. **One instance.** Game state is in memory, so run exactly one app process. No second
   instance, no load balancer.

## macOS hardening checklist

- [ ] **Firewall on** (System Settings > Network > Firewall). Nothing needs inbound access.
- [ ] **FileVault**: on protects data at rest, but after a power loss the Mac waits at the
      unlock screen and every service stays down. On a headless Mac, choose deliberately:
      unlock after outages, or leave it off and accept an unencrypted disk.
- [ ] **Never sleep.** A sleeping Mac takes the site down.
      `sudo pmset -a sleep 0 disksleep 0 displaysleep 15 womp 1 autorestart 1`, then check
      `pmset -g`.
- [ ] **Automatic updates that do not auto-reboot**: download and notify, and reboot at a
      time you pick (a FileVault Mac stalls at the unlock screen after a reboot).
- [ ] **Remote Login (SSH) off** unless you need it (`sudo systemsetup -setremotelogin off`);
      if on, key-only.
- [ ] **`_cribbager` is not an admin**: `id _cribbager` must not list `admin`. install.sh
      refuses to run if it does.
- [ ] Auto-login off, screen lock on, strong password on your own admin account.
- [ ] Keep Homebrew current. `brew upgrade` is fine, then restart the jobs.

#!/bin/bash
# Idempotent installer for the Cribbager launchd deployment. Run on the Mac Studio:
#   sudo DOMAIN=example.com deploy/macos/install.sh   # first install (DOMAIN fills BASE_URL, SMTP_FROM)
#   sudo deploy/macos/install.sh          # re-run / update (DOMAIN is ignored once the env file exists)
#        deploy/macos/install.sh --check  # prerequisite check only (no root needed)
# Safe to re-run: after editing the env file, after pulling new scripts, after upgrades.
set -euo pipefail

SVC_USER=_cribbager
PREFIX=/usr/local/cribbager
LIBEXEC=/usr/local/libexec/cribbager
ETC=/usr/local/etc/cribbager
ENV_FILE=$ETC/env
LOGDIR=/var/log/cribbager
BREW=/opt/homebrew
PGBIN=$BREW/opt/postgresql@16/bin
DAEMONS=/Library/LaunchDaemons
MIN_MACOS=13

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
CHECK_ONLY=0
[ "${1:-}" = "--check" ] && CHECK_ONLY=1

say() { printf '==> %s\n' "$*"; }

# --- prerequisites ---------------------------------------------------------------
problems=()
need() { problems+=("$1"); }

[ "$(uname -s)" = Darwin ] || need "this installer is for macOS (found $(uname -s))"
[ "$(uname -m)" = arm64 ] || need "expected Apple silicon (arm64), found $(uname -m); Homebrew paths assume $BREW"
if command -v sw_vers >/dev/null; then
  major=$(sw_vers -productVersion | cut -d. -f1)
  [ "$major" -ge "$MIN_MACOS" ] || need "macOS $MIN_MACOS or newer required (found $(sw_vers -productVersion))"
fi
[ -x "$BREW/bin/brew" ] || need "Homebrew not found at $BREW (Apple silicon default)"
[ -x "$PGBIN/postgres" ] || need "postgresql@16 missing: brew install postgresql@16"
[ -x "$BREW/bin/cloudflared" ] || need "cloudflared missing: brew install cloudflared"
[ -x "$BREW/bin/age" ] || need "age missing: brew install age"
[ -x /usr/bin/git ] && /usr/bin/git --version >/dev/null 2>&1 || need "git missing: xcode-select --install"
command -v curl >/dev/null || need "curl missing"

GO_BIN=
for c in "$BREW/bin/go" /usr/local/go/bin/go; do [ -x "$c" ] && { GO_BIN=$c; break; }; done
[ -n "$GO_BIN" ] || GO_BIN=$(command -v go || true)
want=$(awk '$1=="go"{print $2; exit}' "$ROOT/go.mod" 2>/dev/null || true)
if [ -z "$want" ]; then
  need "cannot read the go version from $ROOT/go.mod (run this from a checkout of the repo)"
elif [ -z "$GO_BIN" ]; then
  need "Go $want or newer missing: brew install go"
else
  have=$("$GO_BIN" env GOVERSION 2>/dev/null | sed 's/^go//' || true)
  if [ -z "$have" ] || [ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -n1)" != "$want" ]; then
    need "Go $want or newer required (found ${have:-unknown} at $GO_BIN): brew upgrade go"
  fi
fi

if [ ${#problems[@]} -gt 0 ]; then
  echo "Cannot continue; fix these first:" >&2
  for p in "${problems[@]}"; do echo "  - $p" >&2; done
  exit 1
fi
say "prerequisites ok (Go $have at $GO_BIN)"
[ $CHECK_ONLY = 1 ] && exit 0

[ "$(id -u)" = 0 ] || { echo "Run with sudo: sudo $0" >&2; exit 1; }
for f in deploy.sh backup.sh lib-env.sh run-app.sh run-postgres.sh run-cloudflared.sh env.example; do
  [ -f "$HERE/$f" ] || { echo "missing $HERE/$f" >&2; exit 1; }
done

as_svc() { sudo -n -u "$SVC_USER" env -i HOME="$PREFIX/home" PATH="$BREW/bin:/usr/bin:/bin" "$@"; }

# --- service account -------------------------------------------------------------
ensure_account() {
  if dscl . -read "/Users/$SVC_USER" UniqueID >/dev/null 2>&1; then
    say "service user $SVC_USER exists"
  else
    local id
    for id in $(seq 250 399); do
      dscl . -list /Users UniqueID | awk '{print $2}' | grep -qx "$id" && continue
      dscl . -list /Groups PrimaryGroupID | awk '{print $2}' | grep -qx "$id" && continue
      break
    done
    say "creating group and user $SVC_USER (id $id)"
    dscl . -create "/Groups/$SVC_USER"
    dscl . -create "/Groups/$SVC_USER" PrimaryGroupID "$id"
    dscl . -create "/Groups/$SVC_USER" RealName "Cribbager service"
    dscl . -create "/Groups/$SVC_USER" Password '*'
    dscl . -create "/Users/$SVC_USER"
    dscl . -create "/Users/$SVC_USER" UniqueID "$id"
    dscl . -create "/Users/$SVC_USER" PrimaryGroupID "$id"
    dscl . -create "/Users/$SVC_USER" UserShell /usr/bin/false
    dscl . -create "/Users/$SVC_USER" RealName "Cribbager service"
    dscl . -create "/Users/$SVC_USER" NFSHomeDirectory "$PREFIX/home"
    dscl . -create "/Users/$SVC_USER" Password '*'
    dscl . -create "/Users/$SVC_USER" IsHidden 1
  fi
  if id -Gn "$SVC_USER" | tr ' ' '\n' | grep -qx admin; then
    echo "REFUSING: $SVC_USER is in the admin group. Remove it: sudo dseditgroup -o edit -d $SVC_USER -t user admin" >&2
    exit 1
  fi
}

# --- directories, scripts ---------------------------------------------------------
ensure_dirs() {
  install -d -o "$SVC_USER" -g "$SVC_USER" -m 755 "$PREFIX"
  install -d -o "$SVC_USER" -g "$SVC_USER" -m 755 "$PREFIX/home" "$PREFIX/releases" "$PREFIX/state" "$PREFIX/cache"
  install -d -o "$SVC_USER" -g "$SVC_USER" -m 700 "$PREFIX/pgdata" "$PREFIX/run" "$PREFIX/backups"
  install -d -o root -g wheel -m 755 /usr/local/libexec "$LIBEXEC" /usr/local/etc "$ETC"
  install -d -o "$SVC_USER" -g wheel -m 750 "$LOGDIR"
  local l
  for l in app postgres cloudflared deployer backup; do
    [ -f "$LOGDIR/$l.log" ] || install -o "$SVC_USER" -g wheel -m 640 /dev/null "$LOGDIR/$l.log"
  done
  tmutil addexclusion -p "$PREFIX/pgdata" "$PREFIX/cache" >/dev/null 2>&1 || true
}

install_scripts() {
  # root-owned, in a root-owned directory: the service user can run but not alter them.
  local f
  for f in deploy.sh backup.sh lib-env.sh run-app.sh run-postgres.sh run-cloudflared.sh; do
    install -o root -g wheel -m 755 "$HERE/$f" "$LIBEXEC/$f"
  done
  cat >/etc/newsyslog.d/cribbager.conf <<NS
# logfilename                  [owner:group]    mode count size when flags
$LOGDIR/deployer.log  $SVC_USER:wheel  640  7  1024  *  JN
$LOGDIR/backup.log    $SVC_USER:wheel  640  7  1024  *  JN
NS
}

# --- env file --------------------------------------------------------------------
ensure_env() {
  if [ ! -f "$ENV_FILE" ]; then
    say "creating $ENV_FILE with generated secrets"
    local dbpw tok dom=${DOMAIN:-}
    if [ -n "$dom" ] && [[ ! $dom =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]]; then
      echo "DOMAIN must be a bare hostname such as example.com (got: $dom)" >&2; exit 1
    fi
    [ -n "$dom" ] || say "DOMAIN not given: BASE_URL and SMTP_FROM get the placeholder example.com"
    dbpw=$(openssl rand -hex 24)
    tok=$(openssl rand -hex 24)
    (umask 077; sed -e "s/@GENERATE_DB_PASSWORD@/$dbpw/" -e "s/@GENERATE_STATS_TOKEN@/$tok/" \
      -e "s/@DOMAIN@/${dom:-example.com}/g" "$HERE/env.example" >"$ENV_FILE")
  fi
  chown "$SVC_USER:wheel" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
}
env_val() { # env_val KEY -> value from env file (parsed, not sourced)
  local line
  line=$(grep -E "^$1=" "$ENV_FILE" | tail -n1 || true)
  line=${line#*=}
  line=${line%\"}; line=${line#\"}; line=${line%\'}; line=${line#\'}
  printf '%s' "$line"
}
is_set() { [ -n "$1" ] && [ "$1" != CHANGE_ME ]; }

# --- launchd ---------------------------------------------------------------------
render() { sed -e "s|@SVC_USER@|$SVC_USER|g" -e "s|@PREFIX@|$PREFIX|g" -e "s|@LIBEXEC@|$LIBEXEC|g" \
  -e "s|@LOGDIR@|$LOGDIR|g" -e "s|@ENV_FILE@|$ENV_FILE|g" -e "s|@BREW@|$BREW|g" "$HERE/launchd/$1.plist"; }
loaded() { launchctl print "system/org.cribbager.$1" >/dev/null 2>&1; }
changed=
install_plist() { # install_plist name  -> sets $changed if the file changed
  local dest=$DAEMONS/org.cribbager.$1.plist tmp
  tmp=$(mktemp)
  render "$1" >"$tmp"
  plutil -lint "$tmp" >/dev/null
  changed=
  if ! cmp -s "$tmp" "$dest" 2>/dev/null; then
    install -o root -g wheel -m 644 "$tmp" "$dest"
    changed=1
  fi
  rm -f "$tmp"
}
activate() { # activate name
  if loaded "$1"; then
    if [ -n "$changed" ]; then
      say "reloading $1"
      launchctl bootout "system/org.cribbager.$1" || true
      launchctl bootstrap system "$DAEMONS/org.cribbager.$1.plist"
    else
      say "$1 already loaded"
    fi
  else
    say "loading $1"
    launchctl bootstrap system "$DAEMONS/org.cribbager.$1.plist"
  fi
}

# --- postgres --------------------------------------------------------------------
init_postgres() {
  if [ ! -f "$PREFIX/pgdata/PG_VERSION" ]; then
    say "initialising Postgres data directory"
    as_svc "$PGBIN/initdb" -D "$PREFIX/pgdata" -U postgres -E UTF8 --locale=en_US.UTF-8 -k \
      --auth-local=peer --auth-host=reject >/dev/null
  fi
  # Only: the OS service user as superuser "postgres" (peer) and role cribbager (scram),
  # both over the unix socket. Everything else is rejected.
  printf 'local   all         postgres    peer map=svc\nlocal   cribbager   cribbager   scram-sha-256\n' >"$PREFIX/pgdata/pg_hba.conf"
  printf 'svc  %s  postgres\n' "$SVC_USER" >"$PREFIX/pgdata/pg_ident.conf"
  chown "$SVC_USER:$SVC_USER" "$PREFIX/pgdata/pg_hba.conf" "$PREFIX/pgdata/pg_ident.conf"
  chmod 600 "$PREFIX/pgdata/pg_hba.conf" "$PREFIX/pgdata/pg_ident.conf"
}
provision_db() {
  local pw i
  pw=$(env_val DATABASE_URL | sed -n 's|^postgres[a-z]*://cribbager:\([^@]*\)@.*|\1|p')
  [[ $pw =~ ^[A-Za-z0-9]{16,}$ ]] || { echo "DATABASE_URL in $ENV_FILE must use user cribbager and an alphanumeric password (16+ chars)" >&2; exit 1; }
  for i in $(seq 1 60); do "$PGBIN/pg_isready" -q -h "$PREFIX/run" && break; sleep 1; done
  "$PGBIN/pg_isready" -q -h "$PREFIX/run" || { echo "Postgres did not come up; see $LOGDIR/postgres.log" >&2; exit 1; }
  say "provisioning role and database"
  {
    cat <<'SQL'
SELECT pg_reload_conf();
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'cribbager') THEN
    CREATE ROLE cribbager LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;
  END IF;
END $$;
SQL
    printf "ALTER ROLE cribbager PASSWORD '%s';\n" "$pw"
    cat <<'SQL'
SELECT 'CREATE DATABASE cribbager OWNER cribbager ENCODING ''UTF8'' TEMPLATE template0'
  WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'cribbager') \gexec
REVOKE ALL ON DATABASE cribbager FROM PUBLIC;
GRANT CONNECT ON DATABASE cribbager TO cribbager;
SQL
  } | as_svc "$PGBIN/psql" -X -q -h "$PREFIX/run" -U postgres -d postgres -v ON_ERROR_STOP=1 >/dev/null
}

# --- main ------------------------------------------------------------------------
ensure_account
ensure_dirs
install_scripts
ensure_env
as_svc "$GO_BIN" version >/dev/null 2>&1 || { echo "$SVC_USER cannot run $GO_BIN (permissions on its install dir?)" >&2; exit 1; }
init_postgres

install_plist postgres; activate postgres
provision_db

install_plist app; activate app
install_plist deployer; activate deployer

todo=()
if is_set "$(env_val TUNNEL_TOKEN)"; then
  install_plist cloudflared; activate cloudflared
else
  install_plist cloudflared
  todo+=("set TUNNEL_TOKEN in $ENV_FILE (Cloudflare dashboard), then re-run this installer")
fi
if is_set "$(env_val AGE_RECIPIENT)"; then
  install_plist backup; activate backup
else
  install_plist backup
  todo+=("set AGE_RECIPIENT in $ENV_FILE (age public key), then re-run this installer")
fi
case "$(env_val BASE_URL)" in *example.com*) todo+=("set BASE_URL and SMTP_FROM to your real domain in $ENV_FILE, then: sudo launchctl kickstart -k system/org.cribbager.app") ;; esac
if ! is_set "$(env_val SMTP_HOST)"; then
  todo+=("optional: password-reset email is off (SMTP_HOST unset; reset links are only logged). See deploy/macos/docs/email.md")
elif ! is_set "$(env_val SMTP_PASS)"; then
  todo+=("set SMTP_PASS in $ENV_FILE; reset mail only logs until you do, then: sudo launchctl kickstart -k system/org.cribbager.app")
fi

say "done. The deployer builds the first release within a minute or two: tail -f $LOGDIR/deployer.log"
if [ ${#todo[@]} -gt 0 ]; then
  echo; echo "Still to do:"
  for t in "${todo[@]}"; do echo "  - $t"; done
fi

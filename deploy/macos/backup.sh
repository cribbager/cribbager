#!/bin/bash
# Nightly encrypted Postgres backup (launchd: org.cribbager.backup), runs as the service
# user. pg_dump | gzip | age (public key only). The private key is NOT on this machine.
set -euo pipefail
umask 077

PREFIX=${CRIBBAGER_PREFIX:-/usr/local/cribbager}
ENV_FILE=${CRIBBAGER_ENV:-/usr/local/etc/cribbager/env}
BREW=${CRIBBAGER_BREW:-/opt/homebrew}
# shellcheck source=lib-env.sh
. "$(dirname "$0")/lib-env.sh"
load_env "$ENV_FILE"

PG_DUMP=${PG_DUMP:-$BREW/opt/postgresql@16/bin/pg_dump}
AGE=${AGE:-$BREW/bin/age}
BACKUP_DIR=${BACKUP_DIR:-$PREFIX/backups}
KEEP_DAYS=${BACKUP_KEEP_DAYS:-30}
KEEP_MIN=${BACKUP_KEEP_MIN:-7}

log() { printf '%s backup: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*"; }
die() { log "!!! FAILED: $*"; exit 1; }

[ -n "${DATABASE_URL:-}" ] || die "DATABASE_URL not set in $ENV_FILE"
case ${AGE_RECIPIENT:-} in age1*) ;; *) die "AGE_RECIPIENT (an age1... public key) not set in $ENV_FILE" ;; esac
pg_env_from_url "$DATABASE_URL" || die "cannot parse DATABASE_URL"

mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"
out=$BACKUP_DIR/cribbager-$(date -u +%Y%m%dT%H%M%SZ).sql.gz.age
part=$out.partial
trap 'rm -f "$part"' EXIT

"$PG_DUMP" --no-owner --no-acl | gzip -9 | "$AGE" -r "$AGE_RECIPIENT" -o "$part" || die "dump pipeline failed"
size=$(wc -c <"$part" | tr -d ' ')
[ "$size" -gt 200 ] || die "backup suspiciously small ($size bytes)"
head -c 22 "$part" | grep -q 'age-encryption.org' || die "output is not an age file"
mv "$part" "$out"
date -u +%Y-%m-%dT%H:%M:%SZ >"$BACKUP_DIR/.last-success"
log "wrote $out ($size bytes)"

# Retention: always keep the newest KEEP_MIN; beyond that delete files older than KEEP_DAYS.
i=0
while IFS= read -r f; do
  i=$((i + 1))
  [ "$i" -le "$KEEP_MIN" ] && continue
  if [ -n "$(find "$f" -mtime +"$KEEP_DAYS" -print)" ]; then
    rm -f "$f"
    log "pruned $f"
  fi
done < <(find "$BACKUP_DIR" -maxdepth 1 -name 'cribbager-*.sql.gz.age' | sort -r)

# Optional off-machine sync: any command, run after a successful backup (see README).
if [ -n "${BACKUP_POST_CMD:-}" ]; then
  log "running BACKUP_POST_CMD"
  BACKUP_FILE=$out BACKUP_DIR=$BACKUP_DIR sh -c "$BACKUP_POST_CMD" || die "BACKUP_POST_CMD failed (local backup is fine)"
fi

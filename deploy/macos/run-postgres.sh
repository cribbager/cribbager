#!/bin/bash
# launchd entry point for Postgres (org.cribbager.postgres), runs as the service user.
# Unix socket only (no TCP listener). launchd's SIGTERM would request a "smart"
# shutdown, which waits for the app's connections forever; translate it to "fast".
set -euo pipefail
PREFIX=${CRIBBAGER_PREFIX:-/usr/local/cribbager}
BREW=${CRIBBAGER_BREW:-/opt/homebrew}
# launchd hands daemons no locale. With LC_ALL unset the postmaster's startup locale
# lookup goes through macOS's CoreFoundation, which spawns a thread, and Postgres
# aborts: "postmaster became multithreaded during startup". Must stay in step with the
# locale initdb used (install.sh: en_US.UTF-8).
export LC_ALL=${LC_ALL:-en_US.UTF-8} LANG=${LANG:-en_US.UTF-8}

"$BREW/opt/postgresql@16/bin/postgres" -D "$PREFIX/pgdata" \
  -c listen_addresses= \
  -c unix_socket_directories="$PREFIX/run" \
  -c unix_socket_permissions=0700 \
  -c password_encryption=scram-sha-256 \
  -c max_connections=30 \
  -c log_line_prefix='%m [%p] ' \
  -c log_min_duration_statement=2000 &
pid=$!
trap 'kill -INT "$pid" 2>/dev/null' TERM INT
status=0
while kill -0 "$pid" 2>/dev/null; do
  wait "$pid" || status=$?
done
exit "$status"

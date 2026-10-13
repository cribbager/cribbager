#!/bin/bash
# launchd entry point for the app (org.cribbager.app), runs as the service user.
# Loads the env file, waits for Postgres, then execs the current release so launchd
# supervises the server process itself (deploy.sh finds it by its command line).
set -euo pipefail
PREFIX=${CRIBBAGER_PREFIX:-/usr/local/cribbager}
ENV_FILE=${CRIBBAGER_ENV:-/usr/local/etc/cribbager/env}
BREW=${CRIBBAGER_BREW:-/opt/homebrew}
# shellcheck source=lib-env.sh
. "$(dirname "$0")/lib-env.sh"

load_env "$ENV_FILE"
unset TUNNEL_TOKEN # the app has no use for the tunnel credential
# Loopback only, whatever the env file says: the tunnel is the only way in.
export ADDR=127.0.0.1:8080
export WEB="$PREFIX/current/web/public"

if [ -n "${DATABASE_URL:-}" ]; then
  for _ in $(seq 1 60); do
    "$BREW/opt/postgresql@16/bin/pg_isready" -q -h "$PREFIX/run" && break
    sleep 1
  done
fi

if [ ! -x "$PREFIX/current/cribbager-server" ]; then
  echo "no release at $PREFIX/current yet (the deployer builds the first one); retrying" >&2
  sleep 15
  exit 1
fi
exec "$PREFIX/current/cribbager-server"

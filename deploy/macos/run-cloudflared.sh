#!/bin/bash
# launchd entry point for the Cloudflare tunnel (org.cribbager.cloudflared), runs as
# the service user. Only TUNNEL_TOKEN is taken from the env file, and it is passed in the
# environment (not argv), so it never shows up in `ps`.
set -euo pipefail
PREFIX=${CRIBBAGER_PREFIX:-/usr/local/cribbager}
ENV_FILE=${CRIBBAGER_ENV:-/usr/local/etc/cribbager/env}
BREW=${CRIBBAGER_BREW:-/opt/homebrew}
# shellcheck source=lib-env.sh
. "$(dirname "$0")/lib-env.sh"

load_env "$ENV_FILE" TUNNEL_TOKEN
if [ -z "${TUNNEL_TOKEN:-}" ] || [ "$TUNNEL_TOKEN" = CHANGE_ME ]; then
  echo "TUNNEL_TOKEN is not set in $ENV_FILE" >&2
  sleep 30
  exit 1
fi
exec /usr/bin/env -i HOME="$PREFIX/home" PATH="$BREW/bin:/usr/bin:/bin" \
  TUNNEL_TOKEN="$TUNNEL_TOKEN" \
  "$BREW/bin/cloudflared" tunnel --no-autoupdate run

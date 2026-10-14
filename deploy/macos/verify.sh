#!/bin/bash
# Check a Cribbager deployment. Read-only, except --sse creates one throwaway bot game.
#   deploy/macos/verify.sh [--sse] [--no-local] [HOST|URL]
# HOST is the public hostname (https is assumed); a URL such as http://127.0.0.1:8099 is
# used as given. Default: BASE_URL from the env file, if readable.
# Env: STATS_TOKEN (checks /stats and /metrics accept it; never printed),
#      LOCAL_ADDR (default 127.0.0.1:8080), CRIBBAGER_ENV (env file path).
# Exit status is nonzero if any check fails.
set -uo pipefail

ENV_FILE=${CRIBBAGER_ENV:-/usr/local/etc/cribbager/env}
LOCAL=${LOCAL_ADDR:-127.0.0.1:8080}
SSE=0 NOLOCAL=0 TARGET=
for a in "$@"; do
  case $a in
    --sse) SSE=1 ;;
    --no-local) NOLOCAL=1 ;;
    -h | --help) sed -n '2,8p' "$0"; exit 0 ;;
    -*) echo "unknown option $a" >&2; exit 2 ;;
    *) TARGET=$a ;;
  esac
done
if [ -z "$TARGET" ] && [ -r "$ENV_FILE" ]; then
  TARGET=$(grep -E '^BASE_URL=' "$ENV_FILE" | tail -n1 | cut -d= -f2- | tr -d "\"'")
fi
[ -n "$TARGET" ] || { echo "usage: $0 [--sse] [--no-local] HOST|URL (no readable BASE_URL in $ENV_FILE)" >&2; exit 2; }
case $TARGET in http://* | https://*) URL=$TARGET ;; *) URL=https://$TARGET ;; esac
URL=${URL%/}

fails=0
pass() { printf 'PASS  %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; fails=$((fails + 1)); }
code() { curl -s -o /dev/null -m 10 -w '%{http_code}' "$@" 2>/dev/null; }
expect() { # expect LABEL WANT URL [curl args...]
  local label=$1 want=$2 got
  shift 2
  got=$(code "$@")
  if [ "$got" = "$want" ]; then pass "$label ($got)"; else fail "$label: want $want, got ${got:-none}"; fi
}

if [ $NOLOCAL = 0 ]; then
  expect "local /healthz" 200 "http://$LOCAL/healthz"
  expect "local /readyz (Postgres answers)" 200 "http://$LOCAL/readyz"
fi
expect "public $URL/" 200 "$URL/"
expect "public /readyz" 200 "$URL/readyz"
expect "/metrics without token is refused" 401 "$URL/metrics"
expect "/stats without token is refused" 401 "$URL/stats"
if [ -n "${STATS_TOKEN:-}" ]; then
  # Token goes in via a curl config on stdin so it never appears in argv.
  for p in stats metrics; do
    got=$(printf 'header = "Authorization: Bearer %s"\n' "$STATS_TOKEN" | curl -s -o /dev/null -m 10 -w '%{http_code}' -K - "$URL/$p" 2>/dev/null)
    if [ "$got" = 200 ]; then pass "/$p with STATS_TOKEN (200)"; else fail "/$p with STATS_TOKEN: want 200, got ${got:-none}"; fi
  done
fi

if [ $SSE = 1 ]; then
  echo "SSE: creating a throwaway bot game and listening for ~40s..."
  R=$(curl -fsS -m 10 -X POST "$URL/games" -H 'Content-Type: application/json' -d '{"mode":"bot"}' 2>/dev/null || true)
  ID=$(printf '%s' "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin)["game_id"])' 2>/dev/null || true)
  TK=$(printf '%s' "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin)["player_token"])' 2>/dev/null || true)
  if [ -z "$ID" ] || [ -z "$TK" ]; then
    fail "SSE: could not create a game via POST /games"
  else
    out=$(printf 'url = "%s/games/%s/stream?token=%s"\n' "$URL" "$ID" "$TK" | curl -N -sS -m 40 -K - 2>/dev/null || true)
    case $out in *'data:'*) pass "SSE initial event received" ;; *) fail "SSE: no initial data event" ;; esac
    case $out in *': ping'*) pass "SSE stays open and receives ': ping'" ;; *) fail "SSE: no ': ping' within 40s (something buffers or closes the stream)" ;; esac
  fi
fi

echo
if [ $fails = 0 ]; then echo "ALL CHECKS PASSED"; else echo "$fails CHECK(S) FAILED"; exit 1; fi

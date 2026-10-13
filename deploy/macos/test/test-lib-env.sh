#!/bin/bash
# Unit test for lib-env.sh (env parsing and DATABASE_URL splitting).
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=../lib-env.sh
. "$HERE/../lib-env.sh"
T=$(mktemp -d); trap 'rm -r "$T"' EXIT
printf 'A=b$c&d;e\nB='"'"'q r'"'"'\n# comment\nTUNNEL_TOKEN=tok\nDATABASE_URL=postgres://cribbager:ab12@/cribbager?host=/s/r&sslmode=disable\n' >"$T/e"
fails=0; chk() { if [ "$2" = "$3" ]; then echo "  ok   $1"; else echo "  FAIL $1: got '$2' want '$3'"; fails=$((fails+1)); fi; }
load_env "$T/e"
chk "shell metachars literal" "$A" 'b$c&d;e'
chk "quotes stripped" "$B" 'q r'
pg_env_from_url "$DATABASE_URL"
chk "PGUSER" "$PGUSER" cribbager; chk "PGPASSWORD" "$PGPASSWORD" ab12
chk "PGDATABASE" "$PGDATABASE" cribbager; chk "PGHOST socket" "$PGHOST" /s/r
unset A TUNNEL_TOKEN
load_env "$T/e" TUNNEL_TOKEN
chk "only requested keys" "${A:-none}/$TUNNEL_TOKEN" none/tok
[ $fails -eq 0 ] && echo ALL PASSED || echo "$fails FAILED"; exit $((fails>0))

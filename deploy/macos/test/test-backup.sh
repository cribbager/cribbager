#!/bin/bash
# Tests backup.sh with a stub pg_dump and a real `age`. Needs age + age-keygen on PATH
# (or AGE / AGE_KEYGEN set). Scratch dir only.
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
AGE=${AGE:-$(command -v age || true)}; KEYGEN=${AGE_KEYGEN:-$(command -v age-keygen || true)}
[ -x "$AGE" ] && [ -x "$KEYGEN" ] || { echo "SKIP: age/age-keygen not found"; exit 0; }
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
fails=0; chk() { local d=$1; shift; if "$@"; then echo "  ok   $d"; else echo "  FAIL $d"; fails=$((fails+1)); fi; }
"$KEYGEN" -o "$T/key.txt" 2>"$T/pub.txt"; PUB=$(sed -n 's/^Public key: //p' "$T/pub.txt")
printf '#!/bin/sh\n[ -n "$PGPASSWORD" ] && [ "$PGHOST" = /sock ] && echo "CREATE TABLE t(); -- $PGUSER $PGDATABASE"\n' >"$T/pg_dump"; chmod +x "$T/pg_dump"
{ echo "DATABASE_URL=postgres://cribbager:ab12cd@/cribbager?host=/sock&sslmode=disable"
  echo "AGE_RECIPIENT=$PUB"; echo "BACKUP_POST_CMD='echo \$BACKUP_FILE > $T/post'"; } >"$T/env"
export CRIBBAGER_ENV=$T/env CRIBBAGER_PREFIX=$T PG_DUMP=$T/pg_dump AGE
"$HERE/../backup.sh" >"$T/out" 2>&1; chk "backup exit 0" test $? -eq 0
f=$(ls "$T"/backups/cribbager-*.sql.gz.age 2>/dev/null | head -1)
chk "file written" test -n "$f"
chk "restore round-trip" test "$("$AGE" -d -i "$T/key.txt" "$f" | gunzip)" = "CREATE TABLE t(); -- cribbager cribbager"
chk "mode 600" test "$(stat -f %Lp "$f")" = 600
chk "post cmd ran" test -s "$T/post"
chk "no password in output" test -z "$(grep ab12cd "$T/out")"
# retention: 10 old files; newest KEEP_MIN=7 kept regardless, older than 30d beyond that pruned
for i in 01 02 03 04 05 06 07 08 09 10; do touch -t 202001${i}0000 "$T/backups/cribbager-202001${i}T000000Z.sql.gz.age"; done
"$HERE/../backup.sh" >/dev/null 2>&1
n=$(ls "$T"/backups/cribbager-*.age | wc -l | tr -d ' ')
chk "retention keeps newest 7 ($n)" test "$n" -eq 7
# failure paths
printf '#!/bin/sh\nexit 3\n' >"$T/pg_dump"; "$HERE/../backup.sh" >"$T/out2" 2>&1
chk "pg_dump failure -> non-zero" test $? -ne 0
chk "no .partial left" test -z "$(ls "$T"/backups/*.partial 2>/dev/null)"
sed -i '' 's/^AGE_RECIPIENT=.*/AGE_RECIPIENT=/' "$T/env"; "$HERE/../backup.sh" >/dev/null 2>&1
chk "missing recipient -> non-zero" test $? -ne 0
[ $fails -eq 0 ] && echo ALL PASSED || echo "$fails FAILED"; exit $((fails>0))

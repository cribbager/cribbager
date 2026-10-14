#!/bin/bash
# End-to-end test of deploy.sh in a scratch prefix. Needs git, go, curl. Touches nothing
# outside a mktemp directory; no sudo, no launchd.   Usage: deploy/macos/test/test-deploy.sh
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
DEPLOY=$HERE/../deploy.sh
T=$(mktemp -d "${TMPDIR:-/tmp}/cribbager-deploytest.XXXXXX")
SUP_PID=
cleanup() { [ -n "$SUP_PID" ] && { kill "$SUP_PID"; wait "$SUP_PID"; } 2>/dev/null; pkill -f "^$T/.*/cribbager-server\$" 2>/dev/null; chmod -R u+w "$T" 2>/dev/null; rm -rf "$T"; }
trap cleanup EXIT

fails=0
ok() { echo "  ok   $*"; }
bad() { echo "  FAIL $*"; fails=$((fails + 1)); }
check() { # check <desc> <cmd...>
  local d=$1; shift
  if "$@"; then ok "$d"; else bad "$d"; fi
}
eq() { [ "$1" = "$2" ]; }

G() { git -c user.name=test -c user.email=test@example.invalid -C "$T/origin" "$@"; }
commit() { # commit <file-content-tag>  -> new commit on origin main that changes the app
  echo "$1" >"$T/origin/web/public/MARK"; G add web/public/MARK >/dev/null; G commit -qm "$1"; G rev-parse HEAD
}
commit_docs() { # a commit that touches only docs
  echo "$1" >"$T/origin/docs/MARK.md"; G add docs/MARK.md >/dev/null; G commit -qm "$1"; G rev-parse HEAD
}
commit_testfile() { # a commit that touches only a Go test file inside internal/
  echo "// $1" >>"$T/origin/internal/server/ratelimit_test.go"; G add internal/server/ratelimit_test.go >/dev/null; G commit -qm "$1"; G rev-parse HEAD
}
commit_deploy() { # a commit that touches only deploy/ and CI
  echo "# $1" >>"$T/origin/deploy/macos/README.md"; echo "# $1" >>"$T/origin/.github/workflows/ci.yml"
  G add deploy/macos/README.md .github/workflows/ci.yml >/dev/null; G commit -qm "$1"; G rev-parse HEAD
}

# throwaway origin: a clone of this repo's main
git clone -q -b main "$ROOT" "$T/origin" || { echo "clone failed"; exit 1; }
C1=$(G rev-parse HEAD)

P=$T/prefix
export CRIBBAGER_PREFIX=$P CRIBBAGER_REPO_URL=$T/origin CRIBBAGER_GIT_PROTOCOLS=file
export CRIBBAGER_READY_TRIES=3 CRIBBAGER_READY_INTERVAL=0
export CRIBBAGER_RESTART_CMD="echo restart >>$T/restarts"
export CRIBBAGER_READY_CMD="[ \"\$(cat $P/current/REVISION)\" != \"\$(cat $T/bad 2>/dev/null)\" ]"
: >"$T/restarts"
restarts() { wc -l <"$T/restarts" | tr -d ' '; }
cur() { readlink "$P/current" | sed 's|.*/||'; }
st() { cat "$P/state/$1" 2>/dev/null | head -n1; }

echo "1. first deploy"
"$DEPLOY" >"$T/out1" 2>&1; rc=$?
check "exit 0" eq $rc 0
check "current -> C1" eq "$(cur)" "$C1"
check "binary executable" test -x "$P/current/cribbager-server"
check "web/public copied" test -f "$P/current/web/public/index.html"
check "REVISION written" eq "$(cat "$P/current/REVISION")" "$C1"
check "restarted once" eq "$(restarts)" 1

echo "2. no change is a quiet no-op"
"$DEPLOY" >"$T/out2" 2>&1
check "no restart" eq "$(restarts)" 1
check "no output" test ! -s "$T/out2"

echo "3. bad release rolls back, is not retried"
C2=$(commit two); echo "$C2" >"$T/bad"
"$DEPLOY" >"$T/out3" 2>&1; rc=$?
check "exit non-zero" test $rc -ne 0
check "current back on C1" eq "$(cur)" "$C1"
check "failed recorded" eq "$(st failed)" "$C2"
check "restarted twice more (flip + rollback)" eq "$(restarts)" 3
check "loud rollback message" grep -q 'ROLLING BACK' "$T/out3"
"$DEPLOY" >"$T/out3b" 2>&1
check "failed sha not retried" eq "$(restarts)" 3

echo "4. next good commit deploys"
C3=$(commit three); : >"$T/bad"
"$DEPLOY" >"$T/out4" 2>&1; rc=$?
check "exit 0" eq $rc 0
check "current -> C3" eq "$(cur)" "$C3"
check "previous = C1" eq "$(st previous)" "$C1"
check "failed cleared" test ! -e "$P/state/failed"

echo "5. build failure leaves current untouched"
echo 'package main; this is not go' >"$T/origin/cmd/cribbager-server/zz_broken.go"
G add -A >/dev/null; G commit -qm broken; C4=$(G rev-parse HEAD)
n=$(restarts)
"$DEPLOY" >"$T/out5" 2>&1; rc=$?
check "exit non-zero" test $rc -ne 0
check "current still C3" eq "$(cur)" "$C3"
check "no restart attempted" eq "$(restarts)" "$n"
check "failed = C4" eq "$(st failed)" "$C4"
G rm -q -f cmd/cribbager-server/zz_broken.go; G commit -qm unbreak

echo "6. manual rollback holds, resume releases"
"$DEPLOY" rollback "$C1" >"$T/out6" 2>&1; rc=$?
check "rollback exit 0" eq $rc 0
check "current -> C1" eq "$(cur)" "$C1"
check "hold set" test -s "$P/state/hold"
n=$(restarts)
"$DEPLOY" >/dev/null 2>&1
check "deployer holds while rolled back" eq "$(restarts)" "$n"
"$DEPLOY" resume >/dev/null 2>&1
"$DEPLOY" >"$T/out6b" 2>&1; rc=$?
check "after resume follows main" eq $rc 0
check "current == origin main" eq "$(cur)" "$(G rev-parse HEAD)"

echo "7. locking"
mkdir "$P/state/lock"; sleep 30 & LP=$!; echo $LP >"$P/state/lock/pid"
commit seven >/dev/null; n=$(restarts)
"$DEPLOY" >"$T/out7" 2>&1
check "live lock: skipped" eq "$(restarts)" "$n"
kill $LP; wait $LP 2>/dev/null
"$DEPLOY" >"$T/out7b" 2>&1
check "stale lock stolen, deploy ran" eq "$(cur)" "$(G rev-parse HEAD)"
check "lock released" test ! -e "$P/state/lock"

echo "8. pruning"
export CRIBBAGER_KEEP_RELEASES=2
for i in a b c d; do commit "p$i" >/dev/null; "$DEPLOY" >/dev/null 2>&1; done
count=$(ls -1 "$P/releases" | grep -cE '^[0-9a-f]{40}$')
check "at most KEEP(+previous) releases kept ($count)" test "$count" -le 3
check "current present" test -x "$P/current/cribbager-server"
unset CRIBBAGER_KEEP_RELEASES

echo "8b. commits that do not touch the app are not built or restarted"
before=$(cur); n=$(restarts)
DOCS=$(commit_docs "docs only")
"$DEPLOY" >"$T/out8b" 2>&1; rc=$?
check "docs-only: exit 0" eq $rc 0
check "docs-only: current unchanged" eq "$(cur)" "$before"
check "docs-only: no restart" eq "$(restarts)" "$n"
check "docs-only: skip is logged" grep -q 'only docs/CI/deploy files changed' "$T/out8b"
check "docs-only: skipped recorded" eq "$(st skipped)" "$DOCS"
"$DEPLOY" >"$T/out8c" 2>&1
check "docs-only: seen again is a quiet no-op" test ! -s "$T/out8c"
TF=$(commit_testfile "test file only")
"$DEPLOY" >"$T/out8d" 2>&1
check "test-file-only (internal/**/_test.go): not rebuilt" eq "$(cur)" "$before"
check "test-file-only: no restart" eq "$(restarts)" "$n"
check "test-file-only: skipped recorded" eq "$(st skipped)" "$TF"
DP=$(commit_deploy "deploy and ci only")
"$DEPLOY" >"$T/out8e" 2>&1
check "deploy/ + .github only: not rebuilt" eq "$(cur)" "$before"
check "deploy/ + .github only: no restart" eq "$(restarts)" "$n"
APP=$(commit "app change after skipped commits")
"$DEPLOY" >"$T/out8f" 2>&1; rc=$?
check "app commit after skipped ones: exit 0" eq $rc 0
check "app commit deploys the tip (carries the skipped commits)" eq "$(cur)" "$APP"
check "app commit restarts exactly once" eq "$(restarts)" "$((n + 1))"
check "skipped cleared after a real deploy" test ! -e "$P/state/skipped"
"$DEPLOY" status >"$T/status8" 2>&1
check "status shows the skipped line" grep -q '^skipped:' "$T/status8"

echo "9. history rewrite is refused (fast-forward only)"
before=$(cur)
G reset -q --hard "$C1"; commit rewritten >/dev/null
"$DEPLOY" >"$T/out9" 2>&1; rc=$?
check "exit non-zero" test $rc -ne 0
check "current unchanged" eq "$(cur)" "$before"
check "refusal logged" grep -q 'fetch failed' "$T/out9"

echo "10. live: default restart (SIGTERM) + real /readyz, real binary, launchd-style supervisor"
unset CRIBBAGER_RESTART_CMD CRIBBAGER_READY_CMD
chmod -R u+w "$P" 2>/dev/null; rm -rf "$P"; mkdir -p "$P"
G reset -q --hard "$C1"
PORT=$((20000 + RANDOM % 20000))
export CRIBBAGER_READY_URL=http://127.0.0.1:$PORT/readyz
: >"$T/bad"
# stands in for launchd KeepAlive: relaunch from `current`, but "crash" on the bad sha
(
  while :; do
    if [ -x "$P/current/cribbager-server" ] && [ "$(cat "$P/current/REVISION")" != "$(cat "$T/bad")" ]; then
      ADDR=127.0.0.1:$PORT WEB="$P/current/web/public" "$P/current/cribbager-server" >/dev/null 2>&1
    fi
    sleep 1
  done
) &
SUP_PID=$!
export CRIBBAGER_READY_TRIES=15 CRIBBAGER_READY_INTERVAL=1
"$DEPLOY" >"$T/out10" 2>&1; rc=$?
check "first deploy healthy" eq $rc 0
check "readyz serves" curl -fsS -m 3 -o /dev/null "http://127.0.0.1:$PORT/readyz"
pid1=$(pgrep -f "^$P/current/cribbager-server\$")
D2=$(commit live2)
"$DEPLOY" >"$T/out10b" 2>&1; rc=$?
check "second deploy healthy" eq $rc 0
pid2=$(pgrep -f "^$P/current/cribbager-server\$")
check "app process was replaced ($pid1 -> $pid2)" test -n "$pid2" -a "$pid1" != "$pid2"
D3=$(commit live3); echo "$D3" >"$T/bad"
"$DEPLOY" >"$T/out10c" 2>&1; rc=$?
check "crashing release: non-zero" test $rc -ne 0
check "rolled back to D2" eq "$(cur)" "$D2"
check "readyz serves again after rollback" curl -fsS -m 3 -o /dev/null "http://127.0.0.1:$PORT/readyz"

echo
if [ $fails -eq 0 ]; then echo "ALL PASSED"; else echo "$fails FAILED"; echo "(logs kept until exit in $T)"; fi
exit $((fails > 0))

#!/bin/bash
# Pull-based deployer for Cribbager. Runs as the service user from launchd every ~60s.
#
#   deploy.sh [deploy]       fetch origin/main; if it moved, build, flip `current`,
#                            restart the app, verify /readyz, roll back on failure
#   deploy.sh rollback [sha] point `current` at the previous (or a named) release and
#                            HOLD automatic deploys until `resume`
#   deploy.sh resume         lift the hold so the deployer follows main again
#   deploy.sh status         show current/previous release, hold, last failure
#
# Only ever fast-forwards: the fetch refuses a non-fast-forward main, and a new
# commit must descend from the release that is running. Nothing is pushed anywhere.
#
# A commit that changes nothing the running app serves (docs, deploy/, CI, README, test
# files) is recorded as seen but NOT built or restarted; see touches_app. `current` may
# therefore sit behind main until the next commit that does change the app.
#
# Everything below is overridable from the environment so the logic can be tested in a
# scratch prefix (see deploy/macos/test/test-deploy.sh).
set -euo pipefail
umask 027

PREFIX=${CRIBBAGER_PREFIX:-/usr/local/cribbager}
REPO_URL=${CRIBBAGER_REPO_URL:-https://github.com/cribbager/cribbager.git}
BRANCH=${CRIBBAGER_BRANCH:-main}
KEEP=${CRIBBAGER_KEEP_RELEASES:-5}
READY_URL=${CRIBBAGER_READY_URL:-http://127.0.0.1:8080/readyz}
READY_TRIES=${CRIBBAGER_READY_TRIES:-30}
READY_INTERVAL=${CRIBBAGER_READY_INTERVAL:-2}
# Allowed git transports. https only in production; tests set "file".
GIT_PROTOCOLS=${CRIBBAGER_GIT_PROTOCOLS:-https}
# Hooks (eval'd). Defaults: SIGTERM the running app so launchd's KeepAlive relaunches
# it from the new `current`, and curl /readyz. Tests inject stubs.
RESTART_CMD=${CRIBBAGER_RESTART_CMD:-}
READY_CMD=${CRIBBAGER_READY_CMD:-}

export PATH="/opt/homebrew/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
GO_BIN=${CRIBBAGER_GO:-go}

REPO=$PREFIX/repo
RELEASES=$PREFIX/releases
CURRENT=$PREFIX/current
STATE=$PREFIX/state
CACHE=$PREFIX/cache

log() { printf '%s deploy: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*"; }
loud() { log "!!! $*"; }

gitx() {
  GIT_TERMINAL_PROMPT=0 GIT_ALLOW_PROTOCOL=$GIT_PROTOCOLS GIT_CONFIG_NOSYSTEM=1 \
    GIT_CONFIG_GLOBAL=/dev/null GIT_HTTP_LOW_SPEED_LIMIT=1000 GIT_HTTP_LOW_SPEED_TIME=30 \
    git -c credential.helper= -c transfer.fsckObjects=true --git-dir="$REPO" "$@"
}

is_sha() { [[ ${1:-} =~ ^[0-9a-f]{40}$ ]]; }

current_rev() {
  local t
  t=$(readlink "$CURRENT" 2>/dev/null) || return 0
  t=${t##*/}
  is_sha "$t" && printf '%s' "$t"
  return 0
}

read_state() { [ -f "$STATE/$1" ] && head -n1 "$STATE/$1" || true; }

# --- locking: mkdir is atomic; a stale lock (dead pid) is stolen ----------------
LOCK=$STATE/lock
have_lock=0
acquire_lock() {
  mkdir -p "$STATE"
  if ! mkdir "$LOCK" 2>/dev/null; then
    local pid
    pid=$(cat "$LOCK/pid" 2>/dev/null || true)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      return 1
    fi
    log "removing stale lock (pid ${pid:-unknown})"
    rm -rf "$LOCK"
    mkdir "$LOCK" 2>/dev/null || return 1
  fi
  echo $$ >"$LOCK/pid"
  have_lock=1
}
cleanup() {
  [ -n "${BUILD_TMP:-}" ] && rm -rf "$BUILD_TMP"
  [ "$have_lock" = 1 ] && rm -rf "$LOCK"
  return 0
}
trap cleanup EXIT

# --- restart + readiness --------------------------------------------------------
restart_app() {
  if [ -n "$RESTART_CMD" ]; then
    eval "$RESTART_CMD"
    return
  fi
  # No sudo needed: the app runs as this same user, so SIGTERM it (graceful drain,
  # exits 0) and launchd's KeepAlive starts it again from the new `current`.
  local pids p i
  pids=$(pgrep -u "$(id -u)" -f "^$PREFIX/current/cribbager-server\$" || true)
  if [ -z "$pids" ]; then
    log "restart: no running app process; relying on launchd KeepAlive to start it"
    return 0
  fi
  # shellcheck disable=SC2086
  kill -TERM $pids 2>/dev/null || true
  for i in $(seq 1 30); do
    local alive=0
    for p in $pids; do kill -0 "$p" 2>/dev/null && alive=1; done
    [ "$alive" = 0 ] && return 0
    sleep 1
  done
  loud "old app process did not exit in 30s; sending SIGKILL"
  # shellcheck disable=SC2086
  kill -KILL $pids 2>/dev/null || true
  sleep 1
}

ready_once() {
  if [ -n "$READY_CMD" ]; then
    eval "$READY_CMD"
  else
    curl -fsS -m 5 -o /dev/null "$READY_URL"
  fi
}

wait_ready() {
  local i
  for i in $(seq 1 "$READY_TRIES"); do
    if ready_once >/dev/null 2>&1; then
      log "ready after attempt $i/$READY_TRIES"
      return 0
    fi
    sleep "$READY_INTERVAL"
  done
  return 1
}

# --- release management ---------------------------------------------------------
set_current() { # atomically repoint `current` at releases/<sha>
  ln -sfn "$RELEASES/$1" "$CURRENT.new"
  mv -fh "$CURRENT.new" "$CURRENT" 2>/dev/null || { rm -f "$CURRENT.new"; ln -sfn "$RELEASES/$1" "$CURRENT"; }
  touch "$RELEASES/$1"
}

build_release() { # build_release <sha>
  local sha=$1 rel=$RELEASES/$1
  if [ -x "$rel/cribbager-server" ] && [ -d "$rel/web/public" ]; then
    log "release $sha already built"
    return 0
  fi
  log "building $sha"
  mkdir -p "$RELEASES" "$CACHE"
  BUILD_TMP=$RELEASES/.build-$sha.$$
  rm -rf "$BUILD_TMP"
  mkdir -p "$BUILD_TMP/src"
  gitx archive "$sha" | tar -x -C "$BUILD_TMP/src" || { loud "git archive failed for $sha"; return 1; }
  [ -d "$BUILD_TMP/src/web/public" ] || { loud "web/public missing in $sha"; return 1; }
  (
    cd "$BUILD_TMP/src"
    CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS="-trimpath -modcacherw" GOPATH=$CACHE/gopath \
      GOCACHE=$CACHE/go-build GOTELEMETRY=off \
      "$GO_BIN" build -ldflags='-s -w' -o ../cribbager-server ./cmd/cribbager-server
  ) || { loud "go build failed for $sha"; return 1; }
  { mkdir -p "$BUILD_TMP/web" &&
    cp -R "$BUILD_TMP/src/web/public" "$BUILD_TMP/web/public" &&
    printf '%s\n' "$sha" >"$BUILD_TMP/REVISION" &&
    rm -rf "$BUILD_TMP/src" "$rel" &&
    mv "$BUILD_TMP" "$rel"; } || { loud "could not assemble release $sha"; return 1; }
  BUILD_TMP=
}

prune_releases() {
  local cur prev n=0 d name
  cur=$(current_rev)
  prev=$(read_state previous)
  # newest first by mtime; set_current touches the release it activates
  for d in $(ls -1t "$RELEASES" 2>/dev/null); do
    is_sha "$d" || continue
    n=$((n + 1))
    [ "$n" -le "$KEEP" ] && continue
    name=$d
    [ "$name" = "$cur" ] || [ "$name" = "$prev" ] && continue
    log "pruning old release $name"
    rm -rf "${RELEASES:?}/$name"
  done
}

activate() { # activate <sha> : flip, restart, verify. returns 0 healthy.
  set_current "$1"
  restart_app
  wait_ready
}

# Does the diff old..new change what the running app does? The server and its packages,
# the web client it serves, and the module files count; test files and testdata do not.
# Anything else (docs, deploy/, .github/, README) moves main without a rebuild. If git
# cannot tell (a missing object, say) assume yes: a needless build beats a missed one.
touches_app() { # touches_app <old> <new> : succeeds if the app must be rebuilt
  gitx diff --quiet "$1" "$2" -- cmd internal web/public go.mod go.sum \
    ':(exclude,glob)**/*_test.go' ':(exclude,glob)**/testdata/**' && return 1
  return 0
}

# --- commands -------------------------------------------------------------------
cmd_deploy() {
  [ -f "$STATE/hold" ] && return 0 # manual rollback in effect; `resume` lifts it
  mkdir -p "$RELEASES" "$STATE"
  [ -d "$REPO/objects" ] || { log "initialising repo cache at $REPO"; git init --bare -q "$REPO"; }

  # No '+' in the refspec: git refuses anything that is not a fast-forward.
  if ! gitx fetch --quiet --no-tags "$REPO_URL" "refs/heads/$BRANCH:refs/heads/$BRANCH" 2>"$STATE/fetch.err"; then
    loud "git fetch failed (network down, or $BRANCH is not a fast-forward of what we have): $(tr '\n' ' ' <"$STATE/fetch.err")"
    return 1
  fi
  local new deployed
  new=$(gitx rev-parse --verify "refs/heads/$BRANCH^{commit}")
  is_sha "$new" || { loud "unexpected rev '$new'"; return 1; }
  deployed=$(current_rev)

  [ "$new" = "$deployed" ] && return 0
  [ "$new" = "$(read_state failed)" ] && return 0 # already tried and failed; wait for a newer commit
  [ "$new" = "$(read_state skipped)" ] && return 0 # already judged not to touch the app

  if [ -n "$deployed" ]; then
    if ! gitx cat-file -e "$deployed^{commit}" 2>/dev/null ||
      ! gitx merge-base --is-ancestor "$deployed" "$new"; then
      loud "refusing: $new does not descend from running release $deployed (history rewritten?)"
      return 1
    fi
  fi

  if [ -n "$deployed" ] && ! touches_app "$deployed" "$new"; then
    printf '%s\n' "$new" >"$STATE/skipped"
    log "origin/$BRANCH moved ${deployed:0:12} -> ${new:0:12}: only docs/CI/deploy files changed; app not rebuilt or restarted"
    return 0
  fi

  log "origin/$BRANCH moved: ${deployed:-none} -> $new"
  if ! build_release "$new"; then
    printf '%s\n' "$new" >"$STATE/failed"
    loud "build of $new failed; staying on ${deployed:-nothing}"
    return 1
  fi

  [ -n "$deployed" ] && printf '%s\n' "$deployed" >"$STATE/previous"
  if activate "$new"; then
    rm -f "$STATE/failed" "$STATE/skipped"
    log "deployed $new"
    prune_releases
    return 0
  fi

  printf '%s\n' "$new" >"$STATE/failed"
  loud "$new did NOT become ready; ROLLING BACK to ${deployed:-nothing}"
  if [ -z "$deployed" ]; then
    loud "no previous release to roll back to; the app is down until a fixed commit lands"
    return 1
  fi
  if activate "$deployed"; then
    loud "rollback to $deployed succeeded; $new is marked failed and will not be retried"
  else
    loud "rollback to $deployed ALSO failed to become ready; MANUAL INTERVENTION REQUIRED"
  fi
  return 1
}

cmd_rollback() {
  local target=${1:-} cur
  cur=$(current_rev)
  [ -n "$target" ] || target=$(read_state previous)
  is_sha "$target" || { echo "no rollback target (give a full sha; see: deploy.sh status)" >&2; return 2; }
  [ -x "$RELEASES/$target/cribbager-server" ] || { echo "release $target is not on disk" >&2; return 2; }
  [ "$target" != "$cur" ] || { echo "already on $target" >&2; return 2; }
  log "manual rollback $cur -> $target (automatic deploys are now HELD)"
  printf '%s\n' "$cur" >"$STATE/previous"
  printf '%s rolled back from %s to %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$cur" "$target" >"$STATE/hold"
  if activate "$target"; then
    log "rollback complete; run 'deploy.sh resume' to follow $BRANCH again"
  else
    loud "rollback target $target did not become ready"
    return 1
  fi
}

cmd_resume() {
  rm -f "$STATE/hold"
  log "hold lifted; the next run follows $BRANCH"
}

cmd_status() {
  echo "current:  $(current_rev)"
  echo "previous: $(read_state previous)"
  echo "failed:   $(read_state failed)"
  echo "skipped:  $(read_state skipped)   (newest main commit that did not touch the app)"
  echo "hold:     $(read_state hold)"
  echo "main:     $(gitx rev-parse --verify "refs/heads/$BRANCH" 2>/dev/null || true)"
  echo "releases:"
  ls -1t "$RELEASES" 2>/dev/null | grep -E '^[0-9a-f]{40}$' | sed 's/^/  /' || true
}

main() {
  local cmd=${1:-deploy}
  shift || true
  case "$cmd" in
    status) cmd_status; return ;;
    deploy | rollback | resume) ;;
    *) echo "usage: $0 [deploy|rollback [sha]|resume|status]" >&2; return 64 ;;
  esac
  if ! acquire_lock; then
    log "another deploy.sh is running; exiting"
    return 0
  fi
  "cmd_$cmd" "$@"
}

main "$@"

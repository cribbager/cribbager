#!/usr/bin/env bash
# Static, no-cache dev server for the web client (web/public) — handy for iterating
# on the board/designer without running the Go server. Sends `Cache-Control:
# no-store` so browsers never serve stale modules.
# Usage: serve.sh [start|stop] [port]   (defaults: start 8755)
set -euo pipefail
cmd="${1:-start}"
port="${2:-8755}"
root="$(cd "$(dirname "$0")/../public" && pwd)"

case "$cmd" in
  stop)
    pkill -f "cribbage-devserver" 2>/dev/null || true
    pkill -f "http.server $port" 2>/dev/null || true   # legacy caching server, if any
    echo "stopped server on $port"
    ;;
  start)
    if pgrep -f "cribbage-devserver" >/dev/null 2>&1 && curl -s -o /dev/null "http://localhost:$port/"; then
      echo "already running (no-cache): http://localhost:$port/"
    else
      pkill -f "http.server $port" 2>/dev/null || true   # replace any old caching server
      cd "$root"
      nohup python3 -c '
import http.server, socketserver, sys  # cribbage-devserver
class H(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cache-Control", "no-store, max-age=0")
        http.server.SimpleHTTPRequestHandler.end_headers(self)
socketserver.TCPServer(("", int(sys.argv[1])), H).serve_forever()
' "$port" >"/tmp/cribbage-server-$port.log" 2>&1 &
      disown
      sleep 0.6
      echo "started (no-cache): http://localhost:$port/  (root: $root)"
    fi
    ;;
  *)
    echo "usage: serve.sh [start|stop] [port]" >&2
    exit 1
    ;;
esac

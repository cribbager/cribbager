#!/usr/bin/env bash
# Headless Chrome screenshot of a board page.
# Usage: shot.sh <url|path> <out.png> [WxH] [scale]
#   path is resolved against http://localhost:8755/  (e.g. "examples/designer.html")
set -euo pipefail
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
url="${1:?usage: shot.sh <url|path> <out.png> [WxH] [scale]}"
out="${2:?missing output path}"
size="${3:-760x250}"
scale="${4:-2}"

case "$url" in
  http*) ;;
  *) url="http://localhost:8755/$url" ;;
esac
w="${size%x*}"
h="${size#*x}"

"$CHROME" --headless=new --disable-gpu --no-sandbox --virtual-time-budget=3000 \
  --force-device-scale-factor="$scale" --window-size="$w,$h" \
  --screenshot="$out" "$url" >/dev/null 2>&1
echo "wrote $out  ($url)"

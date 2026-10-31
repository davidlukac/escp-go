#!/bin/sh
# Renders the package documentation the way pkg.go.dev does, from the working tree, and saves
# each page as HTML and as plain text under OUT (default build/pkgdoc, gitignored), so it can be
# read, diffed and reviewed without publishing anything.
#
#   scripts/pkgdoc.sh [OUT]          render and save
#   scripts/pkgdoc.sh --serve        serve on http://localhost:8080 until interrupted
#
# Uses the pkgsite version pinned in devtools/go.mod (`go tool -modfile=devtools/go.mod pkgsite`).
set -eu
cd "$(git rev-parse --show-toplevel)"
MODULE="$(go list -m)"
PORT="${PKGDOC_PORT:-8089}"

if [ "${1:-}" = "--serve" ]; then
  exec go tool -modfile=devtools/go.mod pkgsite -http "localhost:${PORT}" .
fi

OUT="${1:-build/pkgdoc}"
rm -rf "$OUT"
mkdir -p "$OUT"
LOG="$(mktemp)"
go tool -modfile=devtools/go.mod pkgsite -http "localhost:${PORT}" . >"$LOG" 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null || true; rm -f "$LOG"' EXIT INT TERM

i=0
until curl -fs "http://localhost:${PORT}/${MODULE}" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 120 ] || { echo "pkgsite did not start:"; cat "$LOG"; exit 1; }
  kill -0 $PID 2>/dev/null || { echo "pkgsite exited:"; cat "$LOG"; exit 1; }
  sleep 1
done

# every package of the module, internal ones included (pkg.go.dev lists them under "Internal")
for pkg in $(go list ./...); do
  name="$(printf '%s' "$pkg" | sed "s|^${MODULE}||; s|^/||; s|/|_|g")"
  [ -n "$name" ] || name=escp
  curl -fs "http://localhost:${PORT}/${pkg}" -o "$OUT/${name}.html"
  python3 - "$OUT/${name}.html" "$OUT/${name}.txt" <<'PY'
import html.parser, sys
class T(html.parser.HTMLParser):
    skip = {"script", "style", "svg", "nav", "header", "footer"}
    block = {"p", "div", "li", "h1", "h2", "h3", "h4", "pre", "tr", "section", "details", "summary", "br", "dt", "dd"}
    def __init__(s):
        super().__init__(); s.out = []; s.depth = 0
    def handle_starttag(s, t, a):
        if t in s.skip: s.depth += 1
        if t in s.block: s.out.append("\n")
        if t in ("h1", "h2", "h3"): s.out.append("#" * int(t[1]) + " ")
    def handle_endtag(s, t):
        if t in s.skip: s.depth = max(0, s.depth - 1)
        if t in s.block: s.out.append("\n")
    def handle_data(s, d):
        if not s.depth: s.out.append(d)
p = T(); p.feed(open(sys.argv[1], encoding="utf-8").read())
text = "".join(p.out)
lines = [l.rstrip() for l in text.splitlines()]
res = []
for l in lines:
    if l.strip() or (res and res[-1].strip()): res.append(l)
open(sys.argv[2], "w", encoding="utf-8").write("\n".join(res) + "\n")
PY
  echo "saved $OUT/${name}.html and .txt"
done

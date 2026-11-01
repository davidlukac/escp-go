#!/bin/sh
# Usage: scripts/release.sh X.Y.Z "Short changelog line"     (on a feature branch)
#        scripts/release.sh --tag X.Y.Z                      (on main, after the squash merge)
#
# First form: adds the CHANGELOG.md entry, runs the checks of RELEASING.md (tests with the
# coverage policy, lint, vulnerabilities) and commits ONLY CHANGELOG.md as `chore: release X.Y.Z`.
# It refuses to run on main, on a dirty tree, and for a version that is not newer than the latest tag.
# Second form: tags the current main commit locally. It never pushes: pushing a tag and publishing
# need a maintainer's decision (RELEASING.md).
#
# Versions: while the module is 0.x, a change that breaks the exported API (a removed or renamed
# name, a changed signature, a changed error or log attribute contract) or adds to it (a new
# setting, an exported name) is a minor version, a fix a patch. From 1.0.0 a break is a major
# version. `make apidiff` lists the API changes.
set -eu
cd "$(git rev-parse --show-toplevel)"
die() { echo "release.sh: $*" >&2; exit 1; }
valid_version() { printf '%s' "$1" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; }
BRANCH="$(git rev-parse --abbrev-ref HEAD)"

if [ "${1:-}" = "--tag" ]; then
  VER="${2:-}"
  valid_version "$VER" || die "usage: --tag X.Y.Z"
  [ "$BRANCH" = main ] || die "tag from main after the squash merge (on $BRANCH)"
  grep -q "^## $VER\$" CHANGELOG.md || die "CHANGELOG.md has no '## $VER' entry; is the release PR merged and pulled?"
  git tag -a "v$VER" -m "v$VER"
  echo "tagged v$VER locally (not pushed; pushing a tag is what publishes, a maintainer decides)"
  exit 0
fi

VER="${1:-}"; MSG="${2:-}"
valid_version "$VER" && [ -n "$MSG" ] || die "usage: X.Y.Z \"changelog line\""
[ "$BRANCH" != main ] || die "refusing to commit on main: create a branch first"
[ -z "$(git status --porcelain)" ] || die "the working tree is not clean; commit or stash first"

LATEST="$(git tag --list 'v*' --sort=-v:refname | head -1 | sed 's/^v//')"
if [ -n "$LATEST" ]; then
  python3 - "$LATEST" "$VER" <<'PY' || die "version $VER is not newer than the latest tag v$LATEST"
import sys
cur, new = (tuple(int(x) for x in v.split('.')) for v in sys.argv[1:3])
sys.exit(0 if new > cur else 1)
PY
fi
grep -q "^## $VER\$" CHANGELOG.md && die "CHANGELOG.md already has '## $VER'"

python3 - "$VER" "$MSG" <<'PY'
import sys
ver, msg = sys.argv[1:3]
p = 'CHANGELOG.md'
s = open(p).read()
# the new entry goes above the newest one, below the title and the introduction
i = s.find('\n## ')
entry = f'\n## {ver}\n\n- {msg}\n'
open(p, 'w').write(s.rstrip('\n') + '\n' + entry if i < 0 else s[:i] + entry + s[i:])
PY

# The checks of RELEASING.md step 1. The pre-commit hook then runs them again on the commit.
make cover lint vuln

git add CHANGELOG.md
[ -z "$(git status --porcelain | grep -v '^M  CHANGELOG.md$')" ] || die "unexpected changes besides CHANGELOG.md; look at git status"
git commit -q -F - <<MSGEOF
chore: release $VER

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>
MSGEOF
echo "committed the $VER entry on $BRANCH; push the branch, open a PR, squash merge, then: scripts/release.sh --tag $VER"

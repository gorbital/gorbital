#!/usr/bin/env bash
# Builds the Dev Portal UI (gorbital-dashboards/apps/devtools, a Next.js
# static export) and copies it into cli/internal/portal/ui/dist, where orb
# embeds it (ADR-0066). Run it before building a release of orb, or whenever
# you want the UI you are working on inside your local orb:
#
#   scripts/sync-portal.sh                  # ../gorbital-dashboards next to this checkout
#   scripts/sync-portal.sh /path/to/gorbital-dashboards
#   DASHBOARDS_REF=v0.3.0 scripts/sync-portal.sh   # check out a tag or branch first
#
# Needs Node 22 and pnpm 10. Commit the copied files with the release that
# should carry them: go install and the release binaries embed what is
# committed, and dist/BUILD records the gorbital-dashboards commit.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dashboards="${1:-$here/../gorbital-dashboards}"
dist="$here/cli/internal/portal/ui/dist"

if [ ! -f "$dashboards/apps/devtools/package.json" ]; then
  echo "sync-portal: no gorbital-dashboards checkout at $dashboards" >&2
  echo "  clone it: git clone https://github.com/gorbital/gorbital-dashboards.git $dashboards" >&2
  exit 1
fi
if [ -n "${DASHBOARDS_REF:-}" ]; then
  git -C "$dashboards" fetch --quiet origin "$DASHBOARDS_REF"
  git -C "$dashboards" checkout --quiet "$DASHBOARDS_REF"
fi

echo "sync-portal: building apps/devtools in $dashboards"
(cd "$dashboards" && pnpm install --frozen-lockfile --silent && NEXT_PUBLIC_DEVTOOLS_DATA=live pnpm --filter devtools build >/dev/null)

out="$dashboards/apps/devtools/out"
if [ ! -f "$out/index.html" ]; then
  echo "sync-portal: the build produced no $out/index.html" >&2
  exit 1
fi

find "$dist" -mindepth 1 -not -name .gitkeep -delete
cp -R "$out"/. "$dist"/
rev="$(git -C "$dashboards" rev-parse --short HEAD 2>/dev/null || echo unknown)"
printf '%s\n' "$rev" > "$dist/BUILD"
echo "sync-portal: copied $(find "$dist" -type f | wc -l | tr -d ' ') files (gorbital-dashboards $rev) into cli/internal/portal/ui/dist"
echo "sync-portal: commit cli/internal/portal/ui/dist to ship it; to try it now: cd cli && go install ./cmd/orb"

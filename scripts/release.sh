#!/usr/bin/env bash
# release.sh tags a gorbital release: the root module, every library module
# under modules/ and the CLI, all at one version on the current commit.
#
#   scripts/release.sh v0.1.0          check, then create the tags locally
#   scripts/release.sh v0.1.0 --check  check only; create nothing
#   scripts/release.sh v0.1.0 --push   also push them to origin
#
# It refuses a dirty tree, a commit that isn't on origin/main, a version
# that isn't vX.Y.Z (or vX.Y.Z-pre), an existing tag, and a module whose
# go.mod requires another gorbital module at a different version. Tags pushed
# to a public repository reach the Go module proxy, which keeps them for good,
# so check the list before --push.
#
# --push pushes in batches of three: GitHub creates no push event at all when
# more than three tags arrive in one push, and the release workflows are
# triggered by those events.
set -euo pipefail

version=${1:-}
push=${2:-}
if [[ ! $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "usage: scripts/release.sh vX.Y.Z [--push]" >&2
  exit 2
fi
if [[ -n $push && $push != --push && $push != --check ]]; then
  echo "unknown option $push" >&2
  exit 2
fi

cd "$(git rev-parse --show-toplevel)"

if [[ -n $(git status --porcelain) ]]; then
  echo "the working tree has changes; commit or remove them first" >&2
  exit 1
fi
git fetch --quiet origin main
commit=$(git rev-parse HEAD)
if ! git merge-base --is-ancestor "$commit" origin/main; then
  echo "HEAD ($commit) isn't on origin/main; release from main" >&2
  exit 1
fi

# Library modules: the root, every go.mod under modules/, and gorbital/.
dirs=(.)
while IFS= read -r mod; do
  dirs+=("$(dirname "$mod")")
done < <(find modules -name go.mod -not -path '*/testdata/*' | sort)
# The composition module (ADR-0081).
[[ -f gorbital/go.mod ]] && dirs+=(gorbital)

tags=()
for dir in "${dirs[@]}"; do
  prefix=""
  [[ $dir != . ]] && prefix="$dir/"
  tags+=("${prefix}${version}")
done
tags+=("cli/${version}")

# Every gorbital module a module requires must be at the release version, or
# the tag ships a combination this repository never built: the replace
# directives here hide a stale requirement, and a consumer has none. v0.3.0
# shipped that way and did not build for anyone who downloaded it. Fix a
# failure with scripts/set-requirements.sh "$version", then commit.
if ! scripts/set-requirements.sh "$version" --check; then
  echo "run: scripts/set-requirements.sh $version" >&2
  exit 1
fi

# The Dev Portal UI is a build of gorbital-dashboards committed here
# (ADR-0066), and the binary serves whatever is committed. Nothing rebuilds it
# on the way past, so a release cut without running scripts/sync-portal.sh
# ships whatever the last person synced: v0.3.3 went out with a UI from six
# days earlier — the sign-in endpoint with no page to type a token into.
#
# So take the latest rather than trust anyone to remember: compare what is
# committed against the tip of the dashboards repository, rebuild it when it is
# behind, and stop so the rebuild can be committed. A release can then only be
# cut from a UI that matches.
dashboards="${DASHBOARDS_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/../gorbital-dashboards}"
portal_build="cli/internal/portal/ui/dist/BUILD"
if [[ ! -f $portal_build ]]; then
  echo "no $portal_build: run scripts/sync-portal.sh and commit it" >&2
  exit 1
fi
built="$(tr -d '[:space:]' < "$portal_build")"
if [[ -d "$dashboards/.git" ]]; then
  git -C "$dashboards" fetch --quiet origin main 2>/dev/null || true
  tip="$(git -C "$dashboards" rev-parse --short origin/main 2>/dev/null || true)"
  if [[ -n $tip && $tip != "$built" ]]; then
    echo "the Dev Portal UI is behind gorbital-dashboards: committed $built, origin/main is $tip"
    echo "rebuilding it with scripts/sync-portal.sh"
    scripts/sync-portal.sh "$dashboards"
    echo "the Dev Portal UI has been rebuilt: commit cli/internal/portal/ui/dist, then run this again" >&2
    exit 1
  fi
  echo "Dev Portal UI matches gorbital-dashboards $built"
else
  # Nothing to compare against: say so rather than pass quietly, because
  # passing quietly is how a stale UI shipped in the first place.
  echo "warning: no gorbital-dashboards checkout at $dashboards, so the Dev Portal UI ($built) could not be checked against it" >&2
fi

for tag in "${tags[@]}"; do
  if git rev-parse --quiet --verify "refs/tags/$tag" >/dev/null || git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null; then
    echo "tag $tag already exists" >&2
    exit 1
  fi
done

echo "Release $version at $(git log -1 --format='%h %s')"
printf '  %s\n' "${tags[@]}"

if [[ $push == --check ]]; then
  echo "Checked ${#tags[@]} tags. Nothing created."
  exit 0
fi

for tag in "${tags[@]}"; do
  git tag -a "$tag" -m "gorbital $version" "$commit"
done
echo "Created ${#tags[@]} tags."

if [[ $push == --push ]]; then
  # Three at a time: GitHub creates no push event when a single push carries
  # more than three tags, and the release workflows run on those events.
  for ((i = 0; i < ${#tags[@]}; i += 3)); do
    batch=("${tags[@]:i:3}")
    echo "Pushing ${batch[*]}"
    git push origin "${batch[@]/#/refs/tags/}"
  done
  echo "Pushed. Ask the proxy for each module so pkg.go.dev picks it up:"
  for tag in "${tags[@]}"; do
    dir=${tag%/"$version"}
    [[ $dir == "$tag" ]] && dir=""
    module="gorbital.dev${dir:+/$dir}"
    echo "  GOPROXY=https://proxy.golang.org GOFLAGS=-mod=mod go list -m $module@$version"
  done
else
  echo "Not pushed. Push with: git push origin ${tags[*]/#/refs/tags/}"
fi

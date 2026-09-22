#!/usr/bin/env bash
# set-requirements.sh points every gorbital.dev requirement in this
# repository at one version, and checks that they all already do.
#
#   scripts/set-requirements.sh v0.3.2           rewrite them
#   scripts/set-requirements.sh v0.3.2 --check   check only; write nothing
#
# A release tags the root module, every module under modules/, gorbital/ and
# the CLI at one version, so a module that requires a sibling at any other
# version ships a combination the repository never built: here the replace
# directives hide it, and a consumer, who has no replace directives, gets
# whatever the requirement names. v0.3.0 shipped that way and did not build
# for anyone who downloaded it.
#
# scripts/release.sh calls --check before it creates any tag. Run the rewrite
# yourself when preparing a release, and commit the result: a tag cannot be
# moved or deleted once it reaches the module proxy.
set -euo pipefail

version=${1:-}
mode=${2:-}
if [[ ! $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "usage: scripts/set-requirements.sh vX.Y.Z [--check]" >&2
  exit 2
fi
if [[ -n $mode && $mode != --check ]]; then
  echo "unknown option $mode" >&2
  exit 2
fi

cd "$(git rev-parse --show-toplevel)"

# The modules a release tags (ADR-0081): the root, every module under
# modules/, the composition module and the CLI.
dirs=(.)
while IFS= read -r mod; do
  dirs+=("$(dirname "$mod")")
done < <(find modules -name go.mod -not -path '*/testdata/*' | sort)
[[ -f gorbital/go.mod ]] && dirs+=(gorbital)
[[ -f cli/go.mod ]] && dirs+=(cli)

# requirements prints "<module path> <version>" for each gorbital.dev module
# the go.mod in $1 requires. A replace directive names no version, so it does
# not match. This is the reader scripts/release.sh has always used.
requirements() {
  sed -nE 's/^[[:space:]]*(require[[:space:]]+)?(gorbital\.dev(\/[^[:space:]]*)?)[[:space:]]+(v[^[:space:]]+).*/\2 \4/p' \
    "$1/go.mod" | grep -v '=>' || true
}

status=0
changed=0
for dir in "${dirs[@]}"; do
  while read -r path req; do
    [[ -z $path ]] && continue
    if [[ $req == "$version" ]]; then
      continue
    fi
    if [[ $mode == --check ]]; then
      echo "$dir/go.mod requires $path $req, want $version" >&2
      status=1
      continue
    fi
    go mod edit -require="$path@$version" "$dir/go.mod"
    echo "$dir/go.mod: $path $req -> $version"
    changed=$((changed + 1))
  done < <(requirements "$dir")
done

if [[ $mode == --check ]]; then
  [[ $status -eq 0 ]] || exit 1
  echo "Every gorbital.dev requirement in ${#dirs[@]} modules names $version."
  exit 0
fi

if [[ $changed -eq 0 ]]; then
  echo "Nothing to change: ${#dirs[@]} modules already name $version."
else
  echo "Rewrote $changed requirement(s) across ${#dirs[@]} modules."
  echo "Review the diff, then run: scripts/set-requirements.sh $version --check"
fi

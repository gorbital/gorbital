#!/usr/bin/env bash
# consumer-check.sh builds a module the way somebody who downloads it does:
# in an empty module, with no replace directives and no workspace. It is the
# check that a release needs and that building in this repository cannot
# give, because the replace directives here hide a stale requirement.
#
#   scripts/consumer-check.sh gorbital.dev/gorbital v0.3.1
#   scripts/consumer-check.sh gorbital.dev/gorbital           # the current tag
#
# Run it for every module a release tags, BEFORE pushing the tag: a tag
# cannot be moved or deleted, so a module that does not resolve stays
# broken until the next version.
set -euo pipefail

module=${1:?usage: consumer-check.sh <module path> [version]}
version=${2:-latest}

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
cd "$dir"

cat > go.mod <<GOMOD
module consumercheck

go 1.26.0
GOMOD

echo "== $module@$version in $dir" >&2
GOFLAGS=-mod=mod GOWORK=off go get "$module@$version"
printf 'package main\n\nimport _ "%s"\n\nfunc main() {}\n' "$module" > main.go
GOFLAGS=-mod=mod GOWORK=off go build ./...
echo "== $module@$version builds for a consumer" >&2

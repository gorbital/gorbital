#!/usr/bin/env bash
# Fetches the showcase applications' repository at the ref docs/examples.json
# pins, into .examples (gitignored), so that an <!-- include examples/apps/...
# --> marker resolves the same way here and in CI (ADR-0093).
#
# Usage: scripts/examples.sh
#
# Environment, all optional and all read by docscheck too where they overlap:
#   GORBITAL_EXAMPLES_REPO  clone this instead of the pinned repository
#                           (a path works, for a checkout you are editing)
#   GORBITAL_EXAMPLES_REF   fetch this ref instead of the pinned one
#   GORBITAL_EXAMPLES_DIR   put the checkout here instead of .examples
#
# The script writes the ref it fetched to <dir>/.ref. docscheck compares that
# with the pin and fails when they differ, so documentation is never checked
# against the wrong code.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
PIN="$ROOT/docs/examples.json"

if [ ! -f "$PIN" ]; then
  echo "scripts/examples.sh: $PIN is missing; it pins the examples repository" >&2
  exit 1
fi

# The pin is three flat string fields, so sed reads it and nothing needs jq.
field() {
  sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$PIN" | head -1
}

REPO=${GORBITAL_EXAMPLES_REPO:-$(field repository)}
REF=${GORBITAL_EXAMPLES_REF:-$(field ref)}
DIR=${GORBITAL_EXAMPLES_DIR:-$ROOT/.examples}

if [ -z "$REPO" ] || [ -z "$REF" ]; then
  echo "scripts/examples.sh: docs/examples.json needs both \"repository\" and \"ref\"" >&2
  exit 1
fi

echo "examples: $REPO at $REF -> $DIR"

if [ -d "$DIR/.git" ]; then
  git -C "$DIR" remote set-url origin "$REPO"
  if ! git -C "$DIR" fetch --quiet --depth 1 --force origin "$REF"; then
    echo "scripts/examples.sh: $REPO has no ref $REF; docs/examples.json pins a ref that doesn't exist" >&2
    exit 1
  fi
  git -C "$DIR" checkout --quiet --detach FETCH_HEAD
else
  rm -rf "$DIR"
  if ! git clone --quiet --depth 1 --branch "$REF" "$REPO" "$DIR"; then
    echo "scripts/examples.sh: couldn't clone $REPO at $REF; docs/examples.json may pin a ref that doesn't exist" >&2
    exit 1
  fi
fi

printf '%s\n' "$REF" >"$DIR/.ref"
echo "examples: $(git -C "$DIR" rev-parse --short HEAD) checked out"

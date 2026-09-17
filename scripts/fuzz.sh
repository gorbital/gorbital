#!/usr/bin/env bash
# fuzz.sh runs every Go fuzz test (func FuzzXxx(f *testing.F)) in every module
# of the repository, one target at a time, for a fixed time each.
#
#   scripts/fuzz.sh                  15s per target (pull requests)
#   FUZZTIME=5m scripts/fuzz.sh      longer (the nightly run)
#   scripts/fuzz.sh -list            print the targets without running them
#
# A failing target doesn't stop the others. Go saves each input that failed
# under the package's testdata/fuzz/<FuzzXxx>/; the script copies the new ones
# to $FUZZ_FAILURES (default fuzz-failures/) with their paths, for CI to
# upload, and exits 1. To reproduce, copy an input back and run
# go test -run '^FuzzXxx$' in the package; commit it as a regression seed with
# the fix.
set -euo pipefail

fuzztime=${FUZZTIME:-15s}
failures=${FUZZ_FAILURES:-fuzz-failures}
list_only=""
case "${1:-}" in
  "") ;;
  -list) list_only=1 ;;
  *) echo "usage: [FUZZTIME=15s] scripts/fuzz.sh [-list]" >&2; exit 2 ;;
esac

root=$(git rev-parse --show-toplevel)
cd "$root"

# Modules: every go.mod except the throwaway spikes and test data.
modules=$(find . -name go.mod -not -path './spikes/*' -not -path '*/testdata/*' -not -path '*/node_modules/*' -exec dirname {} \; | sort)

targets=0
status=0
for module in $modules; do
  # Each package of the module (nested modules excluded by go list) with its
  # test files.
  packages=$(cd "$module" && go list -f '{{.Dir}}{{range .TestGoFiles}} {{.}}{{end}}{{range .XTestGoFiles}} {{.}}{{end}}' ./...)
  while read -r dir files; do
    [[ -n $files ]] || continue
    names=$(cd "$dir" && sed -nE 's/^func (Fuzz[A-Za-z0-9_]*)\(.*\*testing\.F\).*/\1/p' $files | sort -u)
    for name in $names; do
      targets=$((targets + 1))
      rel=.
      [[ $dir != "$root" ]] && rel=${dir#"$root"/}
      if [[ -n $list_only ]]; then
        echo "$rel $name"
        continue
      fi
      echo "== $rel $name ($fuzztime)"
      if ! (cd "$dir" && go test -run '^$' -fuzz "^${name}\$" -fuzztime "$fuzztime" .); then
        echo "FAIL: $rel $name" >&2
        status=1
        while IFS= read -r input; do
          mkdir -p "$failures/$(dirname "$input")"
          cp "$input" "$failures/$input"
          echo "failing input saved: $input" >&2
        done < <(git ls-files --others --exclude-standard -- "$rel/testdata/fuzz/$name")
      fi
    done
  done <<< "$packages"
done

echo "$targets fuzz targets"
exit $status

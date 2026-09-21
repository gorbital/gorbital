#!/usr/bin/env bash
# bench.sh runs every Go benchmark in the repository that contains the current
# directory and prints the results in the format benchstat reads.
#
#   scripts/bench.sh > new.txt                   6 runs of each benchmark
#   BENCHCOUNT=10 scripts/bench.sh > new.txt
#   go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
#
# Only packages with a func BenchmarkXxx(b *testing.B) run. Benchmarks on
# PostgreSQL need the test database (GORBITAL_TEST_DATABASE_URL), as tests do.
# See docs/benchmarks.md.
set -euo pipefail

count=${BENCHCOUNT:-6}
root=$(git rev-parse --show-toplevel)
cd "$root"

modules=$(find . -name go.mod -not -path './spikes/*' -not -path './.examples/*' -not -path '*/testdata/*' -not -path '*/node_modules/*' -exec dirname {} \; | sort)
for module in $modules; do
  # A module whose packages won't load is skipped with a warning rather than
  # ending the run: this script measures, and the build and test jobs are what
  # fail a tree that doesn't load. It also lets the comparison against a base
  # commit work when the base has a module this one has since fixed.
  if ! packages=$(cd "$module" && go list -f '{{.Dir}}{{range .TestGoFiles}} {{.}}{{end}}{{range .XTestGoFiles}} {{.}}{{end}}' ./... 2>&1); then
    echo "== $module: skipped, its packages don't load" >&2
    echo "$packages" | sed 's/^/   /' | head -3 >&2
    continue
  fi
  while read -r dir files; do
    [[ -n $files ]] || continue
    if (cd "$dir" && grep -qE '^func Benchmark[A-Za-z0-9_]*\(.*\*testing\.B\)' $files); then
      echo "== $dir" >&2
      (cd "$dir" && go test -run '^$' -bench . -benchmem -count "$count" .)
    fi
  done <<< "$packages"
done

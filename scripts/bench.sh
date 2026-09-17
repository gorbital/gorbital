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

modules=$(find . -name go.mod -not -path './spikes/*' -not -path '*/testdata/*' -not -path '*/node_modules/*' -exec dirname {} \; | sort)
for module in $modules; do
  packages=$(cd "$module" && go list -f '{{.Dir}}{{range .TestGoFiles}} {{.}}{{end}}{{range .XTestGoFiles}} {{.}}{{end}}' ./...)
  while read -r dir files; do
    [[ -n $files ]] || continue
    if (cd "$dir" && grep -qE '^func Benchmark[A-Za-z0-9_]*\(.*\*testing\.B\)' $files); then
      echo "== $dir" >&2
      (cd "$dir" && go test -run '^$' -bench . -benchmem -count "$count" .)
    fi
  done <<< "$packages"
done

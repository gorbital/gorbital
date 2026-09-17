#!/usr/bin/env bash
# bench-baseline.sh measures a golden app for docs/benchmarks.md: dependency
# counts, stripped binary size, and the time and memory it takes to start
# until GET /readyz answers 200.
#
#   scripts/bench-baseline.sh examples/full-single        5 starts
#   RUNS=9 scripts/bench-baseline.sh examples/minimal
#
# The app listens on a free port on 127.0.0.1.
#
# Full apps need PostgreSQL: GORBITAL_TEST_DATABASE_URL names a server where
# the script may create and drop a database called bench_<app>; it creates it
# with psql, or with psql from the postgres:18 Docker image when psql isn't
# installed. The script migrates it with the app's own migrate command.
#
# Startup time is from starting the process to the first 200 from /readyz,
# polled every 10 ms; memory is the process's RSS at that moment (ps). One
# warm-up start isn't counted (the first start of a new binary pays for the
# operating system's checks). The medians of RUNS starts are printed last as
# a Markdown table.
set -euo pipefail

app=${1:-}
runs=${RUNS:-5}
if [[ -z $app || ! -f $app/go.mod ]]; then
  echo "usage: [RUNS=5] scripts/bench-baseline.sh examples/<app>" >&2
  exit 2
fi
app=$(cd "$app" && pwd)
name=$(basename "$app")
work=$(mktemp -d)
pid=""
cleanup() {
  [[ -n $pid ]] && kill "$pid" 2>/dev/null && wait "$pid" 2>/dev/null
  rm -rf "$work"
}
trap cleanup EXIT

now_ms() { perl -MTime::HiRes=time -e 'printf "%d\n", time() * 1000'; }
median() { sort -n | awk '{ v[NR] = $1 } END { if (NR % 2) print v[(NR + 1) / 2]; else print (v[NR / 2] + v[NR / 2 + 1]) / 2 }'; }

cd "$app"
echo "== $name: go $(go env GOVERSION), $(uname -s) $(uname -m)" >&2
deps=$(go list -deps ./cmd/api | wc -l | tr -d ' ')
mods=$(go list -m all | wc -l | tr -d ' ')
go build -trimpath -ldflags='-s -w' -o "$work/api" ./cmd/api
size=$(wc -c < "$work/api" | tr -d ' ')

# A free port, so /readyz can't be answered by another process.
port=$(perl -MIO::Socket::INET -e 'print IO::Socket::INET->new(Listen => 1, LocalAddr => "127.0.0.1:0")->sockport')
env=(APP_ENV=development "APP_ADDR=127.0.0.1:$port" APP_LOG_LEVEL=warn
  "LOG_ARCHIVE_DIR=$work/logs" "STORAGE_LOCAL_DIR=$work/storage")
if [[ -d cmd/migrate ]]; then
  : "${GORBITAL_TEST_DATABASE_URL:?Full apps need GORBITAL_TEST_DATABASE_URL}"
  db="bench_${name//-/_}"
  sql() {
    if command -v psql >/dev/null; then
      psql -X -q -v ON_ERROR_STOP=1 "$GORBITAL_TEST_DATABASE_URL" -c "$1"
    else
      docker run --rm --add-host=host.docker.internal:host-gateway postgres:18 \
        psql -X -q -v ON_ERROR_STOP=1 "${GORBITAL_TEST_DATABASE_URL/127.0.0.1/host.docker.internal}" -c "$1"
    fi
  }
  sql "DROP DATABASE IF EXISTS $db WITH (FORCE)"
  sql "CREATE DATABASE $db"
  url=$(echo "$GORBITAL_TEST_DATABASE_URL" | sed -E "s#/[^/?]+(\?|$)#/$db\1#")
  env+=("DATABASE_URL=$url")
  go build -o "$work/migrate" ./cmd/migrate
  (cd "$work" && env "${env[@]}" ./migrate >/dev/null)
fi

cd "$work"

times=()
rsss=()
for i in $(seq 0 "$runs"); do
  start=$(now_ms)
  env "${env[@]}" ./api >api.log 2>&1 &
  pid=$!
  until [[ $(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/readyz" || true) == 200 ]]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "the app exited:" >&2
      cat api.log >&2
      exit 1
    fi
    if (( $(now_ms) - start > 30000 )); then
      echo "no 200 from /readyz within 30s:" >&2
      cat api.log >&2
      exit 1
    fi
    sleep 0.01
  done
  ms=$(( $(now_ms) - start ))
  rss=$(ps -o rss= -p "$pid" | tr -d ' ')
  kill "$pid"
  wait "$pid" 2>/dev/null || true
  pid=""
  if (( i == 0 )); then
    echo "warm-up: ${ms} ms, ${rss} KiB RSS" >&2
    continue
  fi
  echo "run $i: ${ms} ms, ${rss} KiB RSS" >&2
  times+=("$ms")
  rsss+=("$rss")
done

if [[ -n ${db:-} ]]; then
  sql "DROP DATABASE IF EXISTS $db WITH (FORCE)"
fi

startup=$(printf '%s\n' "${times[@]}" | median)
rss=$(printf '%s\n' "${rsss[@]}" | median)
echo "| app | packages (go list -deps ./cmd/api) | modules (go list -m all) | stripped binary (bytes) | startup to /readyz 200 (median ms) | RSS at ready (median KiB) |"
echo "| $name | $deps | $mods | $size | $startup | $rss |"

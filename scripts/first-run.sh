#!/usr/bin/env bash
# Measures the v0.1 first run with the real orb CLI and checks the running app:
#   build orb → orb new (against this checkout) → build app → /readyz ready
#
# Usage: scripts/first-run.sh [cold|warm] [workdir]
#   cold  empty Go module and build caches (a clean machine; downloads dependencies)
#   warm  the caches already on this machine
#
# The app listens on PORT (default 18081), never on 8080.
set -euo pipefail

MODE=${1:-cold}
WORK=${2:-$(mktemp -d)}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
PORT=${PORT:-18081}

rm -rf "$WORK" && mkdir -p "$WORK"
if [ "$MODE" = cold ]; then
  export GOMODCACHE="$WORK/gomodcache" GOCACHE="$WORK/gocache" GOFLAGS=-modcacherw
fi

now() { perl -MTime::HiRes=time -e 'printf "%.2f", time'; }
ROWS=()
T0=$(now)

step() { # name, command...
  local name=$1 start
  shift
  start=$(now)
  if ! "$@" >"$WORK/step.log" 2>&1; then
    echo "FAILED: $name"
    tail -30 "$WORK/step.log"
    exit 1
  fi
  ROWS+=("$(printf '%-30s %6.2fs' "$name" "$(echo "$(now) - $start" | bc)")")
}

step "build orb"                bash -c "cd '$ROOT/cli' && go build -o '$WORK/orb' ./cmd/orb"
step "orb new demo-api --local" bash -c "cd '$WORK' && ./orb new demo-api --local '$ROOT' --no-git"
step "build app"                bash -c "cd '$WORK/demo-api' && go build -o .orb/api ./cmd/api"

start=$(now)
(cd "$WORK/demo-api" && APP_ADDR="127.0.0.1:$PORT" ./.orb/api >"$WORK/server.log" 2>&1) &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
for _ in $(seq 1 300); do
  curl -fsS "http://127.0.0.1:$PORT/readyz" >/dev/null 2>&1 && break
  sleep 0.1
done
ROWS+=("$(printf '%-30s %6.2fs' "start → /readyz 200" "$(echo "$(now) - $start" | bc)")")
TOTAL=$(echo "$(now) - $T0" | bc)

echo "== $MODE first run"
printf '%s\n' "${ROWS[@]}"
printf '%-30s %6.2fs\n' "TOTAL" "$TOTAL"

B="http://127.0.0.1:$PORT"
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
echo "== checks"
printf '%-32s %s\n' "GET /livez"                 "$(code $B/livez)"
printf '%-32s %s\n' "GET /readyz"                "$(code $B/readyz)"
printf '%-32s %s\n' "GET /version"               "$(curl -s $B/version)"
printf '%-32s %s\n' "GET /v1/ping"               "$(curl -s $B/v1/ping)"
printf '%-32s %s\n' "POST /v1/echo (+unknown field)" "$(curl -s -w ' [%{http_code}]' -H 'Content-Type: application/json' -d '{"message":"hi","client":"ios"}' $B/v1/echo)"
printf '%-32s %s\n' "POST /v1/echo (blank)"      "$(curl -s -w ' [%{http_code}]' -H 'Content-Type: application/json' -d '{"message":"  "}' $B/v1/echo)"
printf '%-32s %s\n' "GET /v1/nope"               "$(curl -s -w ' [%{http_code}]' $B/v1/nope)"
printf '%-32s %s\n' "GET /openapi.json"          "$(code $B/openapi.json) ($(curl -s $B/openapi.json | wc -c | tr -d ' ') bytes)"
printf '%-32s %s\n' "GET /docs"                  "$(code $B/docs)"
printf '%-32s %s\n' "docs CSP"                   "$(curl -sI $B/docs | grep -i '^content-security-policy' | cut -c1-70)…"
printf '%-32s %s\n' "docs search index"          "$(curl -s -o /dev/null -w '%{http_code} %{size_download} bytes' $B/docs/search.json)"
printf '%-32s %s\n' "security headers"           "$(curl -sI $B/v1/ping | grep -ciE '^(x-content-type-options|x-frame-options|referrer-policy|x-request-id):') of 4"
echo "== first server log lines"
head -3 "$WORK/server.log"
echo "== generated files: $(cd "$WORK/demo-api" && find . -path ./.orb -prune -o -type f -print | wc -l | tr -d ' ')"
if [ "$MODE" = cold ]; then echo "== module cache downloaded: $(du -sh "$GOMODCACHE" | cut -f1)"; fi

#!/usr/bin/env bash
# Measures the Minimal first run: build aps → aps new → build app → /docs ready.
# Usage: ./measure.sh cold|warm WORKDIR
#   cold: empty Go module and build caches (simulates a clean machine, network downloads)
#   warm: uses the caches already on this machine
set -euo pipefail

MODE=${1:?cold|warm}
WORK=${2:?work directory}
ROOT=$(cd "$(dirname "$0")" && pwd)
PORT=18080
export GOTOOLCHAIN=local

rm -rf "$WORK" && mkdir -p "$WORK"
if [ "$MODE" = cold ]; then
  export GOMODCACHE="$WORK/gomodcache" GOCACHE="$WORK/gocache" GOFLAGS=-modcacherw
fi

now() { perl -MTime::HiRes=time -e 'printf "%.2f", time'; }
T0=$(now)
declare -a ROWS

step() { # name, command...
  local name=$1; shift
  local s; s=$(now)
  "$@" >"$WORK/$name.log" 2>&1 || { echo "FAILED: $name (see $WORK/$name.log)"; tail -20 "$WORK/$name.log"; exit 1; }
  ROWS+=("$(printf '%-28s %6.2fs' "$name" "$(echo "$(now) - $s" | bc)")")
}

step "build aps (go install)" bash -c "cd '$ROOT' && go build -o '$WORK/aps' ./cmd/aps"
step "aps new my-api"          bash -c "cd '$WORK' && ./aps new my-api"
step "build app (aps dev)"     bash -c "cd '$WORK/my-api' && go build -o .aps/api ./cmd/api"

s=$(now)
(cd "$WORK/my-api" && APP_ADDR=127.0.0.1:$PORT ./.aps/api >"$WORK/server.log" 2>&1) &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
for _ in $(seq 1 300); do
  curl -fsS "http://127.0.0.1:$PORT/docs" >/dev/null 2>&1 && break
  sleep 0.1
done
ROWS+=("$(printf '%-28s %6.2fs' "start → /docs 200" "$(echo "$(now) - $s" | bc)")")
TOTAL=$(echo "$(now) - $T0" | bc)

echo "== $MODE run"
printf '%s\n' "${ROWS[@]}"
printf '%-28s %6.2fs\n' "TOTAL" "$TOTAL"

B="http://127.0.0.1:$PORT"
echo "== checks"
printf 'GET /livez            %s\n' "$(curl -s -o /dev/null -w '%{http_code}' $B/livez)"
printf 'GET /readyz           %s\n' "$(curl -s -o /dev/null -w '%{http_code}' $B/readyz)"
printf 'GET /version          %s %s\n' "$(curl -s -o /dev/null -w '%{http_code}' $B/version)" "$(curl -s $B/version)"
printf 'GET /v1/ping          %s %s\n' "$(curl -s -o /dev/null -w '%{http_code}' $B/v1/ping)" "$(curl -s $B/v1/ping)"
printf 'POST /v1/echo +extra  %s\n' "$(curl -s -w ' %{http_code}' -H 'Content-Type: application/json' -d '{"message":"hi","extra":1}' $B/v1/echo)"
printf 'POST /v1/echo blank   %s\n' "$(curl -s -w ' %{http_code}' -H 'Content-Type: application/json' -d '{"message":"  "}' $B/v1/echo)"
printf 'GET /openapi.json     %s bytes\n' "$(curl -s $B/openapi.json | wc -c | tr -d ' ')"
printf 'GET /docs             %s (scalar tag: %s)\n' "$(curl -s -o /dev/null -w '%{http_code}' $B/docs)" "$(curl -s $B/docs | grep -c 'api-reference')"
printf 'Scalar asset (CDN)    %s\n' "$(curl -s -o /dev/null -w '%{http_code} %{size_download}B' https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.44.20/dist/browser/standalone.js)"
echo "== generated files"
(cd "$WORK/my-api" && find . -path ./.aps -prune -o -type f -print | sort)
echo "== app binary: $(ls -lh "$WORK/my-api/.aps/api" | awk '{print $5}')"
if [ "$MODE" = cold ]; then echo "== module cache downloaded: $(du -sh "$GOMODCACHE" | cut -f1)"; fi

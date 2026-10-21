#!/usr/bin/env bash
# Compares round_robin and least_connections when one of three backends is slow.
# Requires: Go, curl, and hey (go install github.com/rakyll/hey@latest)
set -euo pipefail
cd "$(dirname "$0")/.."

DURATION=${DURATION:-20s}
CONCURRENCY=${CONCURRENCY:-50}
FAST_DELAY=${FAST_DELAY:-5ms}
SLOW_DELAY=${SLOW_DELAY:-200ms}

command -v hey >/dev/null || { echo "hey not found. Install it with: go install github.com/rakyll/hey@latest"; exit 1; }

mkdir -p bin bench-results
go build -o bin/lb ./cmd/lb
go build -o bin/backend ./examples/backend

pids=()
cleanup() { kill "${pids[@]}" 2>/dev/null || true; }
trap cleanup EXIT

bin/backend -addr :9001 -name backend-1 -delay "$FAST_DELAY" > /dev/null 2>&1 & pids+=($!)
bin/backend -addr :9002 -name backend-2 -delay "$FAST_DELAY" > /dev/null 2>&1 & pids+=($!)
bin/backend -addr :9003 -name backend-3 -delay "$SLOW_DELAY" > /dev/null 2>&1 & pids+=($!)
sleep 1

for algo in round_robin least_connections; do
  bin/lb -config configs/l7.json -algorithm "$algo" > "bench-results/lb-$algo.log" 2>&1 &
  lb=$!
  sleep 2
  echo "=== $algo: ${CONCURRENCY} concurrent clients for ${DURATION}, backend-3 delayed ${SLOW_DELAY} ==="
  hey -z "$DURATION" -c "$CONCURRENCY" http://localhost:8080/ | tee "bench-results/$algo.txt"
  echo "--- requests per backend ---"
  curl -s localhost:9090/metrics | grep '^lb_requests_total' | tee "bench-results/$algo-distribution.txt" || true
  kill "$lb"
  wait "$lb" 2>/dev/null || true
done

echo
echo "Done. Raw output is in bench-results/. Copy the summary numbers into the README."

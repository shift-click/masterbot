#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
MIN_COVERAGE=${MIN_COVERAGE:-35}
COVERPROFILE=${COVERPROFILE:-"$ROOT_DIR/.dist/coverage.out"}

echo "[verify] go test -count=1 ./..."
cd "$ROOT_DIR"
go test -count=1 ./...

echo "[verify] go test -race ./..."
go test -race ./...

echo "[verify] go test -shuffle=on ./..."
go test -shuffle=on ./...

echo "[verify] go test -covermode=atomic -coverprofile=$COVERPROFILE ./..."
mkdir -p "$(dirname "$COVERPROFILE")"
go test -covermode=atomic -coverprofile="$COVERPROFILE" ./...

total=$(go tool cover -func="$COVERPROFILE" | awk '/^total:/ {gsub("%","",$3); print $3}')
if [ -z "$total" ]; then
  echo "[verify] failed: could not parse total coverage" >&2
  exit 1
fi

if ! awk -v total="$total" -v min="$MIN_COVERAGE" 'BEGIN { exit(total+0 >= min+0 ? 0 : 1) }'; then
  echo "[verify] failed: total coverage ${total}% is below threshold ${MIN_COVERAGE}%" >&2
  exit 1
fi

echo "[verify] passed: total coverage ${total}% (threshold ${MIN_COVERAGE}%)"

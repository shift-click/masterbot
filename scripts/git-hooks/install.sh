#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
HOOKS_DIR=${HOOKS_DIR:-$(cd "$ROOT_DIR" && git rev-parse --git-path hooks)}

mkdir -p "$HOOKS_DIR"
install -m 0755 "$SCRIPT_DIR/pre-push" "$HOOKS_DIR/pre-push"

echo "installed pre-push hook: $HOOKS_DIR/pre-push"

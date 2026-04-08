#!/bin/sh
set -eu

RELEASE_ID=${1:-}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SHARED_DIR=${SHARED_DIR:-$REMOTE_ROOT/shared}

if [ -z "$RELEASE_ID" ]; then
  echo "usage: $0 <release-id>" >&2
  exit 1
fi

release_dir="$REMOTE_ROOT/releases/$RELEASE_ID"
current_target=""

if [ ! -d "$release_dir" ]; then
  echo "release not found: $release_dir" >&2
  exit 1
fi

if [ -L "$REMOTE_ROOT/current" ]; then
  current_target=$(readlink "$REMOTE_ROOT/current" || true)
fi

if [ -n "$current_target" ]; then
  ln -sfn "$current_target" "$REMOTE_ROOT/previous"
fi
ln -sfn "$release_dir" "$REMOTE_ROOT/current"
printf '%s\n' "$RELEASE_ID" > "$SHARED_DIR/current-release"
"$release_dir/scripts/deploy/up-jucobot.sh"

#!/bin/sh
set -eu

REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
AUTO_ROLLBACK_ON_FAILURE=${AUTO_ROLLBACK_ON_FAILURE:-1}

cd "$REMOTE_ROOT/current"
if ./scripts/deploy/up-jucobot.sh; then
  exit 0
fi

if [ "$AUTO_ROLLBACK_ON_FAILURE" != "1" ]; then
  echo "deployment failed and auto rollback disabled" >&2
  exit 1
fi

if [ ! -L "$REMOTE_ROOT/previous" ]; then
  echo "deployment failed and previous release is not available" >&2
  exit 1
fi

previous_target=$(readlink "$REMOTE_ROOT/previous" || true)
previous_release_id=$(basename "$previous_target")
if [ -z "$previous_release_id" ]; then
  echo "deployment failed and previous release id is empty" >&2
  exit 1
fi

echo "deployment failed; rolling back to $previous_release_id" >&2
REMOTE_ROOT="$REMOTE_ROOT" SHARED_DIR="${SHARED_DIR:-$REMOTE_ROOT/shared}" SKIP_IRIS_CHECK="${SKIP_IRIS_CHECK:-}" "$REMOTE_ROOT/current/scripts/deploy/rollback.sh" "$previous_release_id"
echo "deployment rolled back to $previous_release_id" >&2
exit 1

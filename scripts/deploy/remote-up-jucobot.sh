#!/bin/sh
set -eu

TARGET=${DEPLOY_TARGET:-${1:-}}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no"}
AUTO_ROLLBACK_ON_FAILURE=${AUTO_ROLLBACK_ON_FAILURE:-1}

if [ -z "$TARGET" ]; then
  echo "usage: DEPLOY_TARGET=user@host $0" >&2
  exit 1
fi

ssh -tt $SSH_OPTS "$TARGET" \
  "REMOTE_ROOT='$REMOTE_ROOT' SKIP_IRIS_CHECK='${SKIP_IRIS_CHECK:-}' AUTO_ROLLBACK_ON_FAILURE='$AUTO_ROLLBACK_ON_FAILURE' sh -s" <<'EOF'
set -eu

cd "$REMOTE_ROOT/current"
if ./scripts/deploy/up-jucobot.sh; then
  exit 0
fi

if [ "${AUTO_ROLLBACK_ON_FAILURE:-1}" != "1" ]; then
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
REMOTE_ROOT="$REMOTE_ROOT" SKIP_IRIS_CHECK="${SKIP_IRIS_CHECK:-}" "$REMOTE_ROOT/current/scripts/deploy/rollback.sh" "$previous_release_id"
EOF

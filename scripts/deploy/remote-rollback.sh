#!/bin/sh
set -eu

TARGET=${DEPLOY_TARGET:-${1:-}}
RELEASE_ID=${RELEASE_ID:-${2:-}}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no"}

if [ -z "$TARGET" ] || [ -z "$RELEASE_ID" ]; then
  echo "usage: DEPLOY_TARGET=user@host RELEASE_ID=<release-id> $0" >&2
  exit 1
fi

ssh -tt $SSH_OPTS "$TARGET" "REMOTE_ROOT='$REMOTE_ROOT' SKIP_IRIS_CHECK='${SKIP_IRIS_CHECK:-}' '$REMOTE_ROOT/current/scripts/deploy/rollback.sh' '$RELEASE_ID'"

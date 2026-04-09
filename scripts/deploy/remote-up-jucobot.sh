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
REMOTE_ROOT="$REMOTE_ROOT" AUTO_ROLLBACK_ON_FAILURE="${AUTO_ROLLBACK_ON_FAILURE:-1}" SKIP_IRIS_CHECK="${SKIP_IRIS_CHECK:-}" ./scripts/deploy/activate-jucobot.sh
EOF

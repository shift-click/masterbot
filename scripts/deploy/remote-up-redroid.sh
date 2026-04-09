#!/bin/sh
set -eu

TARGET=${DEPLOY_TARGET:-${1:-}}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no"}

if [ -z "$TARGET" ]; then
  echo "usage: DEPLOY_TARGET=user@host $0" >&2
  exit 1
fi

ssh -tt $SSH_OPTS "$TARGET" "cd '$REMOTE_ROOT/current' && ./scripts/deploy/up-redroid.sh"

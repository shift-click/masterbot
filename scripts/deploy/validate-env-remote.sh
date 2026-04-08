#!/bin/sh
# validate-env-remote.sh — 원격 서버에서 validate-env.sh 실행
set -eu

TARGET=${DEPLOY_TARGET:-${1:-}}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no"}

if [ -z "$TARGET" ]; then
  echo "usage: DEPLOY_TARGET=user@host $0" >&2
  exit 1
fi

ssh $SSH_OPTS "$TARGET" \
  "SHARED_DIR='$REMOTE_ROOT/shared' CHECK_NETWORK='${CHECK_NETWORK:-0}' sh '$REMOTE_ROOT/current/scripts/deploy/validate-env.sh'"

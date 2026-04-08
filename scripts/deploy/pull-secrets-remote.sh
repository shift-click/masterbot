#!/bin/sh
# pull-secrets-remote.sh — 원격 서버에서 pull-secrets.sh 실행
set -eu

TARGET=${DEPLOY_TARGET:-${1:-}}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no"}

if [ -z "$TARGET" ]; then
  echo "usage: DEPLOY_TARGET=user@host $0" >&2
  exit 1
fi

ssh $SSH_OPTS "$TARGET" \
  "SHARED_DIR='$REMOTE_ROOT/shared' sh '$REMOTE_ROOT/current/scripts/deploy/pull-secrets.sh'"

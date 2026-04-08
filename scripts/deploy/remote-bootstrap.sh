#!/bin/sh
set -eu

TARGET=${DEPLOY_TARGET:-${1:-}}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no"}
SUDO_PASS_B64=""

if [ -z "$TARGET" ]; then
  echo "usage: DEPLOY_TARGET=user@host $0" >&2
  exit 1
fi

if [ -n "${SUDO_PASS:-}" ]; then
  SUDO_PASS_B64=$(printf '%s' "$SUDO_PASS" | base64 | tr -d '\n')
fi

ssh $SSH_OPTS "$TARGET" "cd '$REMOTE_ROOT/current' && SUDO_PASS_B64='$SUDO_PASS_B64' ./scripts/deploy/bootstrap-host.sh"

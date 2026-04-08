#!/bin/sh
# deploy-history.sh — 원격 서버의 최근 배포 이력 조회
set -eu

TARGET=${DEPLOY_TARGET:-${1:-}}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no"}
LIMIT=${LIMIT:-10}

if [ -z "$TARGET" ]; then
  echo "usage: DEPLOY_TARGET=user@host $0" >&2
  exit 1
fi

HISTORY_FILE="$REMOTE_ROOT/shared/deploy-history.jsonl"

ssh $SSH_OPTS "$TARGET" "
  if [ ! -f '$HISTORY_FILE' ]; then
    echo 'No deploy history found.'
    exit 0
  fi
  echo 'Recent deployments (latest first):'
  echo '──────────────────────────────────────────────────────────'
  tail -n $LIMIT '$HISTORY_FILE' | tac 2>/dev/null || tail -r -n $LIMIT '$HISTORY_FILE'
  echo '──────────────────────────────────────────────────────────'
  printf 'Total records: %s\n' \$(wc -l < '$HISTORY_FILE' | tr -d ' ')
"

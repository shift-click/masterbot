#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
SHARED_DIR=${SHARED_DIR:-/opt/jucobot/shared}
ENV_FILE="$SHARED_DIR/jucobot.env"
COMPOSE_FILE="$ROOT_DIR/deployments/compose/jucobot.yml"
SMOKE_SCRIPT="$SCRIPT_DIR/smoke-jucobot.sh"

if [ ! -f "$ENV_FILE" ]; then
  echo "missing env file: $ENV_FILE" >&2
  exit 1
fi

set -a
. "$ENV_FILE"
set +a

if [ -z "${JUCOBOT_BUILD_VERSION:-}" ] && [ -f "$SHARED_DIR/current-release" ]; then
  JUCOBOT_BUILD_VERSION=$(cat "$SHARED_DIR/current-release")
  export JUCOBOT_BUILD_VERSION
fi
if [ -z "${JUCOBOT_BUILD_REVISION:-}" ] && [ -f "$SHARED_DIR/current-revision" ]; then
  JUCOBOT_BUILD_REVISION=$(cat "$SHARED_DIR/current-revision")
  export JUCOBOT_BUILD_REVISION
fi
if [ -z "${JUCOBOT_BUILD_TIME:-}" ] && [ -f "$SHARED_DIR/current-build-time" ]; then
  JUCOBOT_BUILD_TIME=$(cat "$SHARED_DIR/current-build-time")
  export JUCOBOT_BUILD_TIME
fi

if [ "${JUCOBOT_IRIS_ENABLED:-true}" = "false" ]; then
  echo "JUCOBOT_IRIS_ENABLED=false: skipping Iris health check"
elif [ "${SKIP_IRIS_CHECK:-0}" != "1" ]; then
  IRIS_HEALTHCHECK_URL=${IRIS_HEALTHCHECK_URL:-${JUCOBOT_IRIS_HTTP_URL}/dashboard}
  if ! curl -fsS "${IRIS_HEALTHCHECK_URL}" >/dev/null; then
    echo "Iris dashboard is not reachable: ${IRIS_HEALTHCHECK_URL}" >&2
    echo "Set SKIP_IRIS_CHECK=1 to bypass this check." >&2
    exit 1
  fi
fi

set -- --env-file "$ENV_FILE" -f "$COMPOSE_FILE"
if [ -n "${JUCOBOT_CHART_RENDERER_URL:-}" ]; then
  set -- "$@" --profile chart
fi
if [ "${JUCOBOT_ALERTD_ENABLED:-false}" = "true" ]; then
  set -- "$@" --profile alerting
fi

docker compose "$@" up -d --build
docker compose "$@" ps

if [ -x "$SMOKE_SCRIPT" ]; then
  "$SMOKE_SCRIPT"
fi

#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
SHARED_DIR=${SHARED_DIR:-/opt/jucobot/shared}
ENV_FILE="$SHARED_DIR/jucobot.env"
COMPOSE_FILE="$ROOT_DIR/deployments/compose/jucobot.yml"
SMOKE_TIMEOUT_SECONDS=${SMOKE_TIMEOUT_SECONDS:-60}

if [ ! -f "$ENV_FILE" ]; then
  echo "missing env file: $ENV_FILE" >&2
  exit 1
fi

set -a
. "$ENV_FILE"
set +a

if [ -z "${JUCOBOT_BUILD_VERSION:-}" ] && [ -f "$SHARED_DIR/current-release" ]; then
  JUCOBOT_BUILD_VERSION=$(cat "$SHARED_DIR/current-release")
fi
if [ -z "${JUCOBOT_BUILD_REVISION:-}" ] && [ -f "$SHARED_DIR/current-revision" ]; then
  JUCOBOT_BUILD_REVISION=$(cat "$SHARED_DIR/current-revision")
fi
if [ -z "${JUCOBOT_BUILD_TIME:-}" ] && [ -f "$SHARED_DIR/current-build-time" ]; then
  JUCOBOT_BUILD_TIME=$(cat "$SHARED_DIR/current-build-time")
fi

container_name=${JUCOBOT_CONTAINER_NAME:-jucobot-app}
admin_base_path=${JUCOBOT_ADMIN_BASE_PATH:-/admin}
admin_listen_addr=${JUCOBOT_ADMIN_LISTEN_ADDR:-0.0.0.0:9090}
admin_port=$(printf '%s' "$admin_listen_addr" | awk -F: '{print $NF}')
auth_header=${JUCOBOT_ADMIN_AUTH_EMAIL_HEADER:-X-Auth-Request-Email}
smoke_email=${JUCOBOT_SMOKE_ADMIN_EMAIL:-}

if [ -z "$smoke_email" ] && [ -n "${JUCOBOT_ADMIN_ALLOWED_EMAILS:-}" ]; then
  smoke_email=$(printf '%s' "$JUCOBOT_ADMIN_ALLOWED_EMAILS" | cut -d, -f1 | tr -d ' ')
fi

admin_exec_wget() {
  url=$1
  docker exec \
    -e JUCOBOT_SMOKE_EMAIL="$smoke_email" \
    -e JUCOBOT_SMOKE_AUTH_HEADER="$auth_header" \
    -e JUCOBOT_SMOKE_URL="$url" \
    "$container_name" \
    /bin/sh -lc 'wget -qO- --header="${JUCOBOT_SMOKE_AUTH_HEADER}: ${JUCOBOT_SMOKE_EMAIL}" "$JUCOBOT_SMOKE_URL"'
}

now=$(date +%s)
deadline=$((now + SMOKE_TIMEOUT_SECONDS))
started=0
while [ "$(date +%s)" -lt "$deadline" ]; do
  running=$(docker inspect -f '{{.State.Running}}' "$container_name" 2>/dev/null || true)
  restarting=$(docker inspect -f '{{.State.Restarting}}' "$container_name" 2>/dev/null || true)
  if [ "$running" = "true" ] && [ "$restarting" != "true" ]; then
    started=1
    break
  fi
  sleep 2
done

if [ "$started" -ne 1 ]; then
  echo "smoke failed: container not running: $container_name" >&2
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps >&2 || true
  exit 1
fi

startup_logged=0
while [ "$(date +%s)" -lt "$deadline" ]; do
  if docker logs "$container_name" 2>&1 | tail -n 400 | grep -q "jucobot started"; then
    startup_logged=1
    break
  fi
  sleep 2
done

if [ "$startup_logged" -ne 1 ]; then
  echo "smoke failed: startup log not found for $container_name" >&2
  docker logs "$container_name" --tail 100 >&2 || true
  exit 1
fi

startup_line=$(docker logs "$container_name" 2>&1 | tail -n 400 | grep "jucobot started" | tail -n 1 || true)
if [ -n "$startup_line" ]; then
  echo "startup log: $startup_line"
fi

if [ -n "${JUCOBOT_BUILD_REVISION:-}" ] && [ "${JUCOBOT_BUILD_REVISION}" != "unknown" ]; then
  case "$startup_line" in
    *"revision=${JUCOBOT_BUILD_REVISION}"*|*"revision=\"$JUCOBOT_BUILD_REVISION\""*)
      ;;
    *)
      echo "smoke failed: startup revision mismatch (expected ${JUCOBOT_BUILD_REVISION})" >&2
      exit 1
      ;;
  esac
fi

if [ "${JUCOBOT_ADMIN_ENABLED:-false}" = "true" ] && [ -n "$smoke_email" ]; then
  endpoint="http://127.0.0.1:${admin_port}${admin_base_path}/api/overview"
  admin_ready=0
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if admin_exec_wget "$endpoint" >/dev/null 2>&1; then
      admin_ready=1
      break
    fi
    sleep 2
  done
  if [ "$admin_ready" -ne 1 ]; then
    echo "smoke failed: admin overview not reachable: $endpoint" >&2
    exit 1
  fi

  smoke_endpoint="http://127.0.0.1:${admin_port}${admin_base_path}/api/smoke/commands"
  smoke_payload=$(admin_exec_wget "$smoke_endpoint") || {
    echo "smoke failed: admin command smoke not reachable: $smoke_endpoint" >&2
    exit 1
  }
  smoke_compact=$(printf '%s' "$smoke_payload" | tr -d '[:space:]')
  case "$smoke_compact" in
    *'"ok":true'*)
      ;;
    *)
      echo "smoke failed: admin command smoke failed: $smoke_payload" >&2
      exit 1
      ;;
  esac
fi

if [ "${JUCOBOT_ALERTD_ENABLED:-false}" = "true" ]; then
  alertd_name=${JUCOBOT_ALERTD_CONTAINER_NAME:-jucobot-alertd}
  alertd_running=$(docker inspect -f '{{.State.Running}}' "$alertd_name" 2>/dev/null || true)
  if [ "$alertd_running" != "true" ]; then
    echo "smoke failed: alertd container not running: $alertd_name" >&2
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" --profile alerting ps >&2 || true
    exit 1
  fi
fi

if [ -n "${JUCOBOT_CHART_RENDERER_URL:-}" ]; then
  chart_renderer_name=${JUCOBOT_CHART_RENDERER_NAME:-jucobot-chart-renderer}
  chart_ready=0
  while [ "$(date +%s)" -lt "$deadline" ]; do
    chart_running=$(docker inspect -f '{{.State.Running}}' "$chart_renderer_name" 2>/dev/null || true)
    if [ "$chart_running" = "true" ] && docker exec "$chart_renderer_name" /bin/sh -lc 'wget -qO- http://127.0.0.1:3100/health >/dev/null'; then
      chart_ready=1
      break
    fi
    sleep 2
  done

  if [ "$chart_ready" -ne 1 ]; then
    echo "smoke failed: chart renderer not ready: $chart_renderer_name" >&2
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" --profile chart ps >&2 || true
    docker logs "$chart_renderer_name" --tail 100 >&2 || true
    exit 1
  fi
fi

echo "smoke passed"

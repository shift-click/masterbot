#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
SHARED_DIR=${SHARED_DIR:-/opt/jucobot/shared}
ENV_FILE="$SHARED_DIR/redroid.env"
COMPOSE_FILE="$ROOT_DIR/deployments/compose/redroid.yml"

if [ ! -f "$ENV_FILE" ]; then
  echo "missing env file: $ENV_FILE" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

if [ "${REDROID_HOST_ENABLED:-0}" != "1" ]; then
  echo "refusing to start Redroid on this host: REDROID_HOST_ENABLED is not set to 1 in $ENV_FILE" >&2
  echo "set REDROID_HOST_ENABLED=1 only on the designated Redroid host" >&2
  exit 1
fi

docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d

printf 'Redroid started.\n'
printf 'Next operator steps:\n'
printf '  adb connect <server>:%s\n' "$(awk -F= '/^REDROID_ADB_PORT=/{print $2}' "$ENV_FILE" | tail -n 1)"
printf '  scrcpy --tcpip=<server>:%s\n' "$(awk -F= '/^REDROID_ADB_PORT=/{print $2}' "$ENV_FILE" | tail -n 1)"
printf '  open http://<server>:%s/dashboard\n' "$(awk -F= '/^REDROID_IRIS_PORT=/{print $2}' "$ENV_FILE" | tail -n 1)"

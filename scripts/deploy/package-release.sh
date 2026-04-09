#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
DIST_DIR=${DIST_DIR:-"$ROOT_DIR/.dist"}
RELEASE_ID=${RELEASE_ID:-$(date -u +"%Y%m%d%H%M%S")}
ARCHIVE_NAME=${ARCHIVE_NAME:-"jucobot-${RELEASE_ID}.tar.gz"}
ARCHIVE_PATH="$DIST_DIR/$ARCHIVE_NAME"
BUILD_REVISION=${BUILD_REVISION:-$(cd "$ROOT_DIR" && git rev-parse HEAD 2>/dev/null || printf 'unknown')}
BUILD_TIME=${BUILD_TIME:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}
METADATA_PATH="${ARCHIVE_PATH%.tar.gz}.metadata.json"

mkdir -p "$DIST_DIR"

set -- \
  .dockerignore \
  Makefile \
  README.md \
  cmd \
  configs \
  deployments \
  docs \
  go.mod \
  go.sum \
  internal \
  pkg \
  scripts \
  services

archive_inputs=
for path in "$@"; do
  if [ -e "$ROOT_DIR/$path" ]; then
    archive_inputs="${archive_inputs}${archive_inputs:+
}$path"
  fi
done

if [ -z "$archive_inputs" ]; then
  echo "package-release: no archive inputs found" >&2
  exit 1
fi

COPYFILE_DISABLE=1 COPY_EXTENDED_ATTRIBUTES_DISABLE=1 \
  printf '%s\n' "$archive_inputs" | tar -C "$ROOT_DIR" -czf "$ARCHIVE_PATH" -T -

cat > "$METADATA_PATH" <<EOF
{
  "release_id": "$RELEASE_ID",
  "archive_name": "$ARCHIVE_NAME",
  "build_revision": "$BUILD_REVISION",
  "build_time": "$BUILD_TIME"
}
EOF

printf '%s\n' "$ARCHIVE_PATH"

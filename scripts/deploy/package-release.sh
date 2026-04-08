#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
DIST_DIR=${DIST_DIR:-"$ROOT_DIR/.dist"}
RELEASE_ID=${RELEASE_ID:-$(date -u +"%Y%m%d%H%M%S")}
ARCHIVE_NAME=${ARCHIVE_NAME:-"jucobot-${RELEASE_ID}.tar.gz"}
ARCHIVE_PATH="$DIST_DIR/$ARCHIVE_NAME"

mkdir -p "$DIST_DIR"

COPYFILE_DISABLE=1 COPY_EXTENDED_ATTRIBUTES_DISABLE=1 tar -C "$ROOT_DIR" -czf "$ARCHIVE_PATH" \
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

printf '%s\n' "$ARCHIVE_PATH"

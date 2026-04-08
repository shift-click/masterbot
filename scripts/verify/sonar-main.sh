#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

SONAR_HOST_URL=${SONAR_HOST_URL:-http://localhost:9000}
SONAR_PROJECT_KEY=${SONAR_PROJECT_KEY:-github.com.munawiki:jucobot-v2}
SONAR_PROJECT_NAME=${SONAR_PROJECT_NAME:-jucobot-v2}
MAIN_REF=${MAIN_REF:-main}
COVERPROFILE=${COVERPROFILE:-coverage.out}

if [ -z "${SONAR_TOKEN:-}" ]; then
  echo "[sonar-main] SONAR_TOKEN is required" >&2
  exit 1
fi

if ! command -v sonar-scanner >/dev/null 2>&1; then
  echo "[sonar-main] sonar-scanner not found" >&2
  exit 1
fi

main_sha=$(git -C "$ROOT_DIR" rev-parse --verify "$MAIN_REF")
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/jucobot-main-sonar-XXXXXX")

cleanup() {
  git -C "$ROOT_DIR" worktree remove --force "$tmpdir" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

echo "[sonar-main] preparing detached worktree at $tmpdir ($MAIN_REF@$main_sha)"
git -C "$ROOT_DIR" worktree add --detach "$tmpdir" "$MAIN_REF" >/dev/null

cd "$tmpdir"

echo "[sonar-main] go test -covermode=atomic -coverprofile=$COVERPROFILE ./..."
go test ./... -covermode=atomic -coverprofile="$COVERPROFILE"

echo "[sonar-main] running sonar-scanner for $SONAR_PROJECT_KEY"
sonar-scanner \
  -Dsonar.projectKey="$SONAR_PROJECT_KEY" \
  -Dsonar.projectName="$SONAR_PROJECT_NAME" \
  -Dsonar.sources=cmd,internal,pkg \
  -Dsonar.tests=. \
  '-Dsonar.test.inclusions=**/*_test.go' \
  '-Dsonar.exclusions=**/*_test.go,**/.dist/**,**/openspec/**,**/docs/**,**/deployments/**' \
  -Dsonar.go.coverage.reportPaths="$COVERPROFILE" \
  -Dsonar.scm.exclusions.disabled=true \
  -Dsonar.host.url="$SONAR_HOST_URL" \
  -Dsonar.token="$SONAR_TOKEN"

echo "[sonar-main] done"

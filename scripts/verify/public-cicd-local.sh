#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

echo "[public-cicd] go test ./..."
(cd "$ROOT_DIR" && go test ./...)

echo "[public-cicd] chart-renderer npm ci"
(cd "$ROOT_DIR/services/chart-renderer" && npm ci)

echo "[public-cicd] chart-renderer npm test"
(cd "$ROOT_DIR/services/chart-renderer" && npm test)

echo "[public-cicd] terraform init -backend=false"
terraform -chdir="$ROOT_DIR/infra/terraform" init -backend=false >/dev/null

echo "[public-cicd] terraform validate"
terraform -chdir="$ROOT_DIR/infra/terraform" validate

echo "[public-cicd] package release"
archive_path=$(
  cd "$ROOT_DIR" && \
  RELEASE_ID="verify-public-cicd" \
  BUILD_REVISION="verify-public-cicd" \
  BUILD_TIME="2026-04-08T00:00:00Z" \
  ./scripts/deploy/package-release.sh
)

echo "[public-cicd] dry-run ssm deploy payload"
(
  cd "$ROOT_DIR" && \
  DRY_RUN=1 \
  TARGET_INSTANCE_ID="i-verify" \
  DEPLOY_BUCKET="verify-bucket" \
  AWS_REGION="ap-northeast-2" \
  REMOTE_ROOT="/opt/jucobot" \
  SSM_PREFIX="/jucobot/prod" \
  RELEASE_ID="verify-public-cicd" \
  BUILD_REVISION="verify-public-cicd" \
  BUILD_TIME="2026-04-08T00:00:00Z" \
  ARCHIVE_PATH="$archive_path" \
  ./scripts/deploy/deploy-ssm.sh >/dev/null
)

echo "[public-cicd] verification passed"

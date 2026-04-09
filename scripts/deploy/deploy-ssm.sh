#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
PACKAGE_SCRIPT="$SCRIPT_DIR/package-release.sh"
REMOTE_SCRIPT="$SCRIPT_DIR/deploy-ssm-remote.sh"

TARGET_INSTANCE_ID=${TARGET_INSTANCE_ID:-${1:-}}
DEPLOY_BUCKET=${DEPLOY_BUCKET:-}
AWS_REGION=${AWS_REGION:-ap-northeast-2}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SSM_PREFIX=${SSM_PREFIX:-/jucobot/prod}
PULL_SECRETS=${PULL_SECRETS:-1}
WAIT_FOR_COMMAND=${WAIT_FOR_COMMAND:-1}
WAIT_TIMEOUT_SECONDS=${WAIT_TIMEOUT_SECONDS:-1800}
WAIT_POLL_SECONDS=${WAIT_POLL_SECONDS:-5}
DRY_RUN=${DRY_RUN:-0}
ARCHIVE_PATH=${ARCHIVE_PATH:-}
BUILD_REVISION=${BUILD_REVISION:-}
BUILD_TIME=${BUILD_TIME:-}
RELEASE_ID=${RELEASE_ID:-}

if [ -z "$TARGET_INSTANCE_ID" ]; then
  echo "usage: TARGET_INSTANCE_ID=i-xxxx DEPLOY_BUCKET=<bucket> $0" >&2
  exit 1
fi

if [ -z "$DEPLOY_BUCKET" ]; then
  echo "DEPLOY_BUCKET is required" >&2
  exit 1
fi

if [ -z "$ARCHIVE_PATH" ]; then
  ARCHIVE_PATH=$("$PACKAGE_SCRIPT")
fi

if [ ! -f "$ARCHIVE_PATH" ]; then
  echo "archive not found: $ARCHIVE_PATH" >&2
  exit 1
fi

archive_file=$(basename "$ARCHIVE_PATH")
if [ -z "$RELEASE_ID" ]; then
  RELEASE_ID=$(printf '%s' "$archive_file" | sed -e 's/^jucobot-//' -e 's/\.tar\.gz$//')
fi
if [ -z "$BUILD_REVISION" ]; then
  BUILD_REVISION=$(cd "$ROOT_DIR" && git rev-parse HEAD 2>/dev/null || printf 'unknown')
fi
if [ -z "$BUILD_TIME" ]; then
  BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
fi

s3_key="releases/$RELEASE_ID/$archive_file"
s3_uri="s3://$DEPLOY_BUCKET/$s3_key"

python3_cmd() {
  python3 - "$@"
}

remote_script_b64=$(base64 < "$REMOTE_SCRIPT" | tr -d '\n')
params_file=$(mktemp)

python3_cmd "$params_file" "$remote_script_b64" "$REMOTE_ROOT" "$AWS_REGION" "$SSM_PREFIX" "$PULL_SECRETS" "$s3_uri" "$RELEASE_ID" "$BUILD_REVISION" "$BUILD_TIME" <<'PY'
import json
import shlex
import sys

params_path, script_b64, remote_root, region, ssm_prefix, pull_secrets, s3_uri, release_id, build_revision, build_time = sys.argv[1:]
commands = [
    "set -eu",
    "tmp_script=/tmp/jucobot-ssm-deploy.sh",
    f"printf '%s' {shlex.quote(script_b64)} | base64 -d > \"$tmp_script\"",
    "chmod +x \"$tmp_script\"",
    (
        f"REMOTE_ROOT={shlex.quote(remote_root)} "
        f"AWS_REGION={shlex.quote(region)} "
        f"SSM_PREFIX={shlex.quote(ssm_prefix)} "
        f"PULL_SECRETS={shlex.quote(pull_secrets)} "
        f"S3_URI={shlex.quote(s3_uri)} "
        f"RELEASE_ID={shlex.quote(release_id)} "
        f"BUILD_REVISION={shlex.quote(build_revision)} "
        f"BUILD_TIME={shlex.quote(build_time)} "
        "DEPLOYER_NAME=github-actions "
        "\"$tmp_script\""
    ),
]
with open(params_path, "w", encoding="utf-8") as f:
    json.dump({"commands": commands, "executionTimeout": ["3600"]}, f)
PY

if [ "$DRY_RUN" = "1" ]; then
  printf 'archive=%s\n' "$ARCHIVE_PATH"
  printf 's3_uri=%s\n' "$s3_uri"
  printf 'release_id=%s\n' "$RELEASE_ID"
  printf 'build_revision=%s\n' "$BUILD_REVISION"
  printf 'build_time=%s\n' "$BUILD_TIME"
  printf 'target_instance_id=%s\n' "$TARGET_INSTANCE_ID"
  cat "$params_file"
  rm -f "$params_file"
  exit 0
fi

if ! command -v aws >/dev/null 2>&1; then
  echo "AWS CLI not found" >&2
  rm -f "$params_file"
  exit 1
fi

aws s3 cp "$ARCHIVE_PATH" "$s3_uri" --region "$AWS_REGION"

command_id=$(aws ssm send-command \
  --document-name "AWS-RunShellScript" \
  --comment "JucoBot production deploy $RELEASE_ID" \
  --instance-ids "$TARGET_INSTANCE_ID" \
  --parameters "file://$params_file" \
  --region "$AWS_REGION" \
  --query "Command.CommandId" \
  --output text)

rm -f "$params_file"

printf 'command_id=%s\n' "$command_id"
printf 'release_id=%s\n' "$RELEASE_ID"
printf 's3_uri=%s\n' "$s3_uri"

if [ "$WAIT_FOR_COMMAND" != "1" ]; then
  exit 0
fi

deadline=$(( $(date +%s) + WAIT_TIMEOUT_SECONDS ))
while :; do
  status=$(aws ssm get-command-invocation \
    --command-id "$command_id" \
    --instance-id "$TARGET_INSTANCE_ID" \
    --region "$AWS_REGION" \
    --query "Status" \
    --output text 2>/dev/null || printf 'Pending')

  case "$status" in
    Success)
      aws ssm get-command-invocation \
        --command-id "$command_id" \
        --instance-id "$TARGET_INSTANCE_ID" \
        --region "$AWS_REGION" \
        --query "StandardOutputContent" \
        --output text
      exit 0
      ;;
    Cancelled|Cancelling|Failed|TimedOut)
      aws ssm get-command-invocation \
        --command-id "$command_id" \
        --instance-id "$TARGET_INSTANCE_ID" \
        --region "$AWS_REGION" \
        --query "StandardOutputContent" \
        --output text || true
      aws ssm get-command-invocation \
        --command-id "$command_id" \
        --instance-id "$TARGET_INSTANCE_ID" \
        --region "$AWS_REGION" \
        --query "StandardErrorContent" \
        --output text || true
      echo "SSM deploy failed with status: $status" >&2
      exit 1
      ;;
  esac

  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "SSM deploy timed out waiting for command: $command_id" >&2
    exit 1
  fi

  sleep "$WAIT_POLL_SECONDS"
done

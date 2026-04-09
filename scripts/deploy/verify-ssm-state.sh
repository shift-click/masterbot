#!/bin/sh
set -eu

TARGET_INSTANCE_ID=${TARGET_INSTANCE_ID:-${1:-}}
AWS_REGION=${AWS_REGION:-ap-northeast-2}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
EXPECTED_RELEASE_ID=${EXPECTED_RELEASE_ID:-}
EXPECTED_REVISION=${EXPECTED_REVISION:-}
WAIT_TIMEOUT_SECONDS=${WAIT_TIMEOUT_SECONDS:-300}
WAIT_POLL_SECONDS=${WAIT_POLL_SECONDS:-5}

if [ -z "$TARGET_INSTANCE_ID" ]; then
  echo "usage: TARGET_INSTANCE_ID=i-xxxx EXPECTED_REVISION=<sha> $0" >&2
  exit 1
fi

if [ -z "$EXPECTED_REVISION" ]; then
  echo "EXPECTED_REVISION is required" >&2
  exit 1
fi

params_file=$(mktemp)
python3 - "$params_file" "$REMOTE_ROOT" "$EXPECTED_RELEASE_ID" "$EXPECTED_REVISION" <<'PY'
import json
import shlex
import sys

params_path, remote_root, expected_release, expected_revision = sys.argv[1:]
commands = [
    "set -eu",
    f"shared_dir={shlex.quote(remote_root)}/shared",
    "release=$(cat \"$shared_dir/current-release\")",
    "revision=$(cat \"$shared_dir/current-revision\")",
    "build_time=$(cat \"$shared_dir/current-build-time\")",
]
if expected_release:
    commands.append(f"[ \"$release\" = {shlex.quote(expected_release)} ]")
commands.append(f"[ \"$revision\" = {shlex.quote(expected_revision)} ]")
commands.extend([
    "printf 'release=%s\n' \"$release\"",
    "printf 'revision=%s\n' \"$revision\"",
    "printf 'build_time=%s\n' \"$build_time\"",
])
with open(params_path, "w", encoding="utf-8") as f:
    json.dump({"commands": commands, "executionTimeout": ["600"]}, f)
PY

command_id=$(aws ssm send-command \
  --document-name "AWS-RunShellScript" \
  --comment "Verify JucoBot production revision" \
  --instance-ids "$TARGET_INSTANCE_ID" \
  --parameters "file://$params_file" \
  --region "$AWS_REGION" \
  --query "Command.CommandId" \
  --output text)

rm -f "$params_file"

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
        --query "StandardErrorContent" \
        --output text || true
      echo "remote revision verification failed with status: $status" >&2
      exit 1
      ;;
  esac

  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "SSM verification timed out waiting for command: $command_id" >&2
    exit 1
  fi

  sleep "$WAIT_POLL_SECONDS"
done

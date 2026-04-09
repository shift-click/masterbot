#!/bin/sh
set -eu

REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SHARED_DIR=${SHARED_DIR:-$REMOTE_ROOT/shared}
APP_USER=${APP_USER:-ubuntu}
APP_GROUP=${APP_GROUP:-$APP_USER}
AWS_REGION=${AWS_REGION:-ap-northeast-2}
S3_URI=${S3_URI:-}
RELEASE_ID=${RELEASE_ID:-}
BUILD_REVISION=${BUILD_REVISION:-unknown}
BUILD_TIME=${BUILD_TIME:-unknown}
SSM_PREFIX=${SSM_PREFIX:-/jucobot/prod}
PULL_SECRETS=${PULL_SECRETS:-1}

if [ -z "$S3_URI" ] || [ -z "$RELEASE_ID" ]; then
  echo "S3_URI and RELEASE_ID are required" >&2
  exit 1
fi

ensure_aws_cli() {
  if command -v aws >/dev/null 2>&1; then
    return
  fi

  export DEBIAN_FRONTEND=noninteractive

  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    apt-get install -y awscli
    return
  fi

  if command -v dnf >/dev/null 2>&1; then
    dnf install -y awscli
    return
  fi

  if command -v yum >/dev/null 2>&1; then
    yum install -y awscli
    return
  fi

  echo "AWS CLI not found and no supported package manager is available" >&2
  exit 1
}

reset_aws_cli_env() {
  unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
  unset AWS_PROFILE AWS_DEFAULT_PROFILE AWS_SHARED_CREDENTIALS_FILE AWS_CONFIG_FILE
}

ensure_aws_cli
reset_aws_cli_env

release_dir="$REMOTE_ROOT/releases/$RELEASE_ID"
current_link="$REMOTE_ROOT/current"
previous_link="$REMOTE_ROOT/previous"
archive_path="/tmp/$(basename "$S3_URI")"
previous_target=""

mkdir -p "$REMOTE_ROOT/releases" "$SHARED_DIR"

if [ -L "$current_link" ]; then
  previous_target=$(readlink "$current_link" || true)
fi

cleanup() {
  status=$?
  rm -f "$archive_path"
  if [ $status -ne 0 ]; then
    rm -rf "$release_dir"
    if [ -n "$previous_target" ]; then
      ln -sfn "$previous_target" "$current_link"
    fi
  fi
  exit $status
}

trap cleanup EXIT INT TERM

if [ -e "$release_dir" ]; then
  echo "release already exists: $release_dir" >&2
  exit 1
fi

aws s3 cp "$S3_URI" "$archive_path" --region "$AWS_REGION"
mkdir -p "$release_dir"
tar -xzf "$archive_path" -C "$release_dir"

[ -f "$SHARED_DIR/config.yaml" ] || cp "$release_dir/deployments/shared/config.yaml.example" "$SHARED_DIR/config.yaml"
[ -f "$SHARED_DIR/jucobot.env" ] || cp "$release_dir/deployments/shared/jucobot.env.example" "$SHARED_DIR/jucobot.env"
[ -f "$SHARED_DIR/redroid.env" ] || cp "$release_dir/deployments/shared/redroid.env.example" "$SHARED_DIR/redroid.env"

if [ "$PULL_SECRETS" = "1" ]; then
  SHARED_DIR="$SHARED_DIR" AWS_REGION="$AWS_REGION" SSM_PREFIX="$SSM_PREFIX" "$release_dir/scripts/deploy/pull-secrets.sh"
fi

if [ -n "$previous_target" ]; then
  ln -sfn "$previous_target" "$previous_link"
fi
ln -sfn "$release_dir" "$current_link"

chown -R "$APP_USER:$APP_GROUP" "$REMOTE_ROOT"

su -s /bin/sh "$APP_USER" -c "REMOTE_ROOT='$REMOTE_ROOT' SHARED_DIR='$SHARED_DIR' AUTO_ROLLBACK_ON_FAILURE='1' SKIP_IRIS_CHECK='${SKIP_IRIS_CHECK:-}' JUCOBOT_BUILD_VERSION='$RELEASE_ID' JUCOBOT_BUILD_REVISION='$BUILD_REVISION' JUCOBOT_BUILD_TIME='$BUILD_TIME' '$REMOTE_ROOT/current/scripts/deploy/activate-jucobot.sh'"

printf '%s\n' "$RELEASE_ID" > "$SHARED_DIR/current-release"
printf '%s\n' "$BUILD_REVISION" > "$SHARED_DIR/current-revision"
printf '%s\n' "$BUILD_TIME" > "$SHARED_DIR/current-build-time"

trap - EXIT INT TERM
rm -f "$archive_path"

deployer=${DEPLOYER_NAME:-github-actions}
ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
printf '{"ts":"%s","release":"%s","revision":"%s","deployer":"%s","result":"ok","mode":"ssm"}\n' \
  "$ts" "$RELEASE_ID" "$BUILD_REVISION" "$deployer" >> "$SHARED_DIR/deploy-history.jsonl"

printf 'release=%s\n' "$RELEASE_ID"
printf 'revision=%s\n' "$BUILD_REVISION"
printf 'build_time=%s\n' "$BUILD_TIME"

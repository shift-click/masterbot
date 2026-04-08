#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
PACKAGE_SCRIPT="$SCRIPT_DIR/package-release.sh"
VERIFY_SCRIPT="$SCRIPT_DIR/../verify/local-verify.sh"

TARGET=${DEPLOY_TARGET:-${1:-}}
REMOTE_ROOT=${REMOTE_ROOT:-/opt/jucobot}
SSH_OPTS=${SSH_OPTS:-"-o StrictHostKeyChecking=no"}
SUDO_PASS_B64=""

if [ -z "$TARGET" ]; then
  echo "usage: DEPLOY_TARGET=user@host $0" >&2
  exit 1
fi

if [ -n "${SUDO_PASS:-}" ]; then
  SUDO_PASS_B64=$(printf '%s' "$SUDO_PASS" | base64 | tr -d '\n')
fi

if [ "${SKIP_LOCAL_VERIFY:-0}" != "1" ]; then
  "$VERIFY_SCRIPT"
fi

if [ "${SKIP_E2E:-0}" != "1" ]; then
  echo "Running E2E tests..."
  (cd "$ROOT_DIR" && go test ./tests/e2e/ -v -count=1 -timeout=120s)
fi

if [ "${SKIP_VALIDATE:-0}" != "1" ]; then
  if ssh $SSH_OPTS "$TARGET" "test -f '$REMOTE_ROOT/current/scripts/deploy/validate-env.sh'" 2>/dev/null; then
    echo "Running remote environment validation..."
    ssh $SSH_OPTS "$TARGET" "SHARED_DIR='$REMOTE_ROOT/shared' sh '$REMOTE_ROOT/current/scripts/deploy/validate-env.sh'" || {
      echo "Remote validation failed. Fix issues or use SKIP_VALIDATE=1 to skip." >&2
      exit 1
    }
  fi
fi

ARCHIVE_PATH=${ARCHIVE_PATH:-$("$PACKAGE_SCRIPT")}
ARCHIVE_FILE=$(basename "$ARCHIVE_PATH")
RELEASE_ID=${RELEASE_ID:-$(printf '%s' "$ARCHIVE_FILE" | sed -e 's/^jucobot-//' -e 's/\.tar\.gz$//')}
REMOTE_ARCHIVE="/tmp/$ARCHIVE_FILE"
BUILD_REVISION=${JUCOBOT_BUILD_REVISION:-}
BUILD_TIME=${JUCOBOT_BUILD_TIME:-}

if [ -z "$BUILD_REVISION" ]; then
  BUILD_REVISION=$(cd "$ROOT_DIR" && git rev-parse HEAD 2>/dev/null || printf 'unknown')
fi
if [ -z "$BUILD_TIME" ]; then
  BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
fi

scp $SSH_OPTS "$ARCHIVE_PATH" "$TARGET:$REMOTE_ARCHIVE"

ssh $SSH_OPTS "$TARGET" "SUDO_PASS_B64='$SUDO_PASS_B64' REMOTE_ROOT='$REMOTE_ROOT' RELEASE_ID='$RELEASE_ID' REMOTE_ARCHIVE='$REMOTE_ARCHIVE' BUILD_REVISION='$BUILD_REVISION' BUILD_TIME='$BUILD_TIME' sh -s" <<'EOF'
set -eu

sudo_run() {
  if sudo -n true 2>/dev/null; then
    sudo "$@"
    return
  fi

  if [ -n "${SUDO_PASS_B64:-}" ]; then
    printf '%s' "$SUDO_PASS_B64" | base64 -d | sudo -S -p '' "$@"
    return
  fi

  sudo "$@"
}

release_dir="$REMOTE_ROOT/releases/$RELEASE_ID"
shared_dir="$REMOTE_ROOT/shared"
current_link="$REMOTE_ROOT/current"
previous_link="$REMOTE_ROOT/previous"
previous_target=""

if [ -L "$current_link" ]; then
  previous_target=$(readlink "$current_link" || true)
fi

cleanup() {
  status=$?
  if [ $status -ne 0 ]; then
    rm -rf "$release_dir"
    if [ -n "$previous_target" ]; then
      ln -sfn "$previous_target" "$current_link"
    fi
  fi
  exit $status
}

trap cleanup EXIT INT TERM

sudo_run mkdir -p "$REMOTE_ROOT" "$REMOTE_ROOT/releases" "$shared_dir"
sudo_run chown -R "$(id -un):$(id -gn)" "$REMOTE_ROOT"

if [ -e "$release_dir" ]; then
  echo "release already exists: $release_dir" >&2
  exit 1
fi

mkdir -p "$release_dir"
tar -xzf "$REMOTE_ARCHIVE" -C "$release_dir"
rm -f "$REMOTE_ARCHIVE"

[ -f "$shared_dir/config.yaml" ] || cp "$release_dir/deployments/shared/config.yaml.example" "$shared_dir/config.yaml"
[ -f "$shared_dir/jucobot.env" ] || cp "$release_dir/deployments/shared/jucobot.env.example" "$shared_dir/jucobot.env"
[ -f "$shared_dir/redroid.env" ] || cp "$release_dir/deployments/shared/redroid.env.example" "$shared_dir/redroid.env"

if [ -n "$previous_target" ]; then
  ln -sfn "$previous_target" "$previous_link"
fi
ln -sfn "$release_dir" "$current_link"
printf '%s\n' "$RELEASE_ID" > "$shared_dir/current-release"
printf '%s\n' "${BUILD_REVISION:-unknown}" > "$shared_dir/current-revision"
printf '%s\n' "${BUILD_TIME:-unknown}" > "$shared_dir/current-build-time"

trap - EXIT INT TERM

printf 'release_dir=%s\n' "$release_dir"
printf 'current_link=%s\n' "$current_link"
EOF

deploy_result="ok"
printf 'Published release %s to %s\n' "$RELEASE_ID" "$TARGET"

# record deploy history
deployer=$(whoami)@$(hostname -s 2>/dev/null || echo "local")
ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
ssh $SSH_OPTS "$TARGET" "printf '{\"ts\":\"%s\",\"release\":\"%s\",\"deployer\":\"%s\",\"result\":\"%s\"}\n' '$ts' '$RELEASE_ID' '$deployer' '$deploy_result' >> '$REMOTE_ROOT/shared/deploy-history.jsonl'" 2>/dev/null || true

printf 'Next steps:\n'
printf '  DEPLOY_TARGET=%s ./scripts/deploy/remote-bootstrap.sh\n' "$TARGET"
printf '  DEPLOY_TARGET=%s ./scripts/deploy/remote-up-redroid.sh\n' "$TARGET"

#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

PUBLIC_REMOTE_URL=${PUBLIC_REMOTE_URL:-"https://github.com/shift-click/masterbot.git"}
PUBLIC_MODULE_PATH=${PUBLIC_MODULE_PATH:-"github.com/shift-click/masterbot"}
PUBLIC_BRANCH=${PUBLIC_BRANCH:-"main"}
SOURCE_MODULE_PATH=${SOURCE_MODULE_PATH:-$(sed -n 's/^module //p' "$ROOT_DIR/go.mod" | head -n 1)}
VERIFY=${VERIFY:-1}
KEEP_EXPORT_DIR=${KEEP_EXPORT_DIR:-0}
FORCE_PUSH=${FORCE_PUSH:-1}

PUSH=0
EXPORT_DIR=${EXPORT_DIR:-}

usage() {
  cat <<'EOF'
Usage: ./scripts/export-public.sh [--push] [--no-verify] [--keep-export-dir] [--export-dir PATH]

Options:
  --push             공개 remote에 새 히스토리로 push
  --no-verify        공개 스냅샷에서 go test 생략
  --keep-export-dir  완료 후 export 디렉터리 유지
  --export-dir PATH  임시 디렉터리 대신 지정 경로 사용
  --help             도움말 출력

Environment:
  PUBLIC_REMOTE_URL    기본값: https://github.com/shift-click/masterbot.git
  PUBLIC_MODULE_PATH   기본값: github.com/shift-click/masterbot
  PUBLIC_BRANCH        기본값: main
  SOURCE_MODULE_PATH   기본값: 현재 go.mod의 module 값
  VERIFY               기본값: 1
  KEEP_EXPORT_DIR      기본값: 0
  FORCE_PUSH           기본값: 1 (push 시 remote HEAD를 확인한 뒤 --force-with-lease 사용)
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --push)
      PUSH=1
      ;;
    --no-verify)
      VERIFY=0
      ;;
    --keep-export-dir)
      KEEP_EXPORT_DIR=1
      ;;
    --export-dir)
      shift
      if [ "$#" -eq 0 ]; then
        echo "[public-export] --export-dir requires a path" >&2
        exit 1
      fi
      EXPORT_DIR=$1
      ;;
    --help)
      usage
      exit 0
      ;;
    *)
      echo "[public-export] unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
  shift
done

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[public-export] missing required command: $1" >&2
    exit 1
  fi
}

need_cmd rsync
need_cmd git
need_cmd python3
need_cmd rg

if [ "$VERIFY" = "1" ]; then
  need_cmd go
fi

cleanup() {
  if [ "$KEEP_EXPORT_DIR" = "1" ]; then
    echo "[public-export] kept export dir: $EXPORT_DIR"
    return
  fi
  if [ -n "${EXPORT_DIR:-}" ] && [ -d "$EXPORT_DIR" ]; then
    rm -rf "$EXPORT_DIR"
  fi
}

if [ -z "$EXPORT_DIR" ]; then
  EXPORT_DIR=$(mktemp -d /tmp/masterbot-public-XXXXXX)
else
  mkdir -p "$EXPORT_DIR"
fi

trap cleanup EXIT INT TERM

echo "[public-export] source: $ROOT_DIR"
echo "[public-export] export: $EXPORT_DIR"
echo "[public-export] module: $SOURCE_MODULE_PATH -> $PUBLIC_MODULE_PATH"

rm -rf "$EXPORT_DIR/.git"

EXCLUDE_FILE="$EXPORT_DIR/.public-export-excludes"
cat >"$EXCLUDE_FILE" <<'EOF'
.git/
/.agent/
/.claude/
/.codex/
/.dist/
/openspec/
/docs/
/CLAUDE.md
/CONTRIBUTING.md
/alertd
/jucobot
/bin/
/data/*.db
/data/*.db-shm
/data/*.db-wal
/services/chart-renderer/node_modules/
EOF

echo "[public-export] syncing public snapshot"
rsync -a --delete --exclude-from="$EXCLUDE_FILE" "$ROOT_DIR/" "$EXPORT_DIR/"
rm -f "$EXCLUDE_FILE"

if [ "$SOURCE_MODULE_PATH" != "$PUBLIC_MODULE_PATH" ]; then
  echo "[public-export] rewriting module path references"
  python3 - "$EXPORT_DIR" "$SOURCE_MODULE_PATH" "$PUBLIC_MODULE_PATH" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
old = sys.argv[2]
new = sys.argv[3]

for path in root.rglob("*"):
    if not path.is_file() or ".git" in path.parts:
        continue
    try:
        data = path.read_text()
    except Exception:
        continue
    if old not in data:
        continue
    path.write_text(data.replace(old, new))
PY
fi

if [ "$VERIFY" = "1" ]; then
  echo "[public-export] verifying snapshot with go test ./..."
  (
    cd "$EXPORT_DIR"
    go test ./...
  )
fi

if [ "$PUSH" != "1" ]; then
  echo "[public-export] snapshot ready: $EXPORT_DIR"
  echo "[public-export] use --push to publish"
  KEEP_EXPORT_DIR=1
  exit 0
fi

echo "[public-export] initializing fresh git history"
(
  cd "$EXPORT_DIR"
  git init -b "$PUBLIC_BRANCH" >/dev/null
  git add .
  git commit -m "Initial public import" >/dev/null
  git remote add origin "$PUBLIC_REMOTE_URL"
  if [ "$FORCE_PUSH" = "1" ]; then
    current_remote_head=$(git ls-remote --heads origin "$PUBLIC_BRANCH" | awk '{print $1}')
    if [ -n "$current_remote_head" ]; then
      echo "[public-export] pushing to $PUBLIC_REMOTE_URL ($PUBLIC_BRANCH, force-with-lease against $current_remote_head)"
      git push --force-with-lease="refs/heads/$PUBLIC_BRANCH:$current_remote_head" -u origin "$PUBLIC_BRANCH"
    else
      echo "[public-export] pushing to $PUBLIC_REMOTE_URL ($PUBLIC_BRANCH, force)"
      git push --force -u origin "$PUBLIC_BRANCH"
    fi
  else
    echo "[public-export] pushing to $PUBLIC_REMOTE_URL ($PUBLIC_BRANCH)"
    git push -u origin "$PUBLIC_BRANCH"
  fi
)

echo "[public-export] published to $PUBLIC_REMOTE_URL"

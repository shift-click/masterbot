#!/bin/sh
# full-deploy.sh — 통합 배포 오케스트레이션
# deploy-remote → bootstrap → up-redroid → [android pause] → up-jucobot
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

TARGET=${DEPLOY_TARGET:-${1:-}}
SKIP_ANDROID=${SKIP_ANDROID:-0}

if [ -z "$TARGET" ]; then
  echo "usage: DEPLOY_TARGET=user@host $0" >&2
  echo "  SKIP_ANDROID=1  Android already initialized (re-deploy)" >&2
  exit 1
fi

step() {
  step_num=$1
  step_name=$2
  shift 2
  printf '\n\033[1m[Step %s/5] %s\033[0m\n' "$step_num" "$step_name"
  if ! "$@"; then
    printf '\n\033[31mFailed at step %s: %s\033[0m\n' "$step_num" "$step_name" >&2
    printf 'To resume from this step, run:\n' >&2
    printf '  %s\n' "$*" >&2
    exit 1
  fi
}

# Step 1: Deploy release
step 1 "릴리스 배포" \
  "$SCRIPT_DIR/deploy-remote.sh"

# Step 2: Host bootstrap
step 2 "호스트 초기화 (binder, binderfs)" \
  "$SCRIPT_DIR/remote-bootstrap.sh"

# Step 3: Start Redroid
step 3 "Redroid 컨테이너 시작" \
  "$SCRIPT_DIR/remote-up-redroid.sh"

# Step 4: Android initialization (interactive or skip)
if [ "$SKIP_ANDROID" = "1" ]; then
  printf '\n\033[33m[Step 4/5] Android 초기화 건너뜀 (SKIP_ANDROID=1)\033[0m\n'
else
  printf '\n\033[1m[Step 4/5] Android 수동 초기화\033[0m\n'
  printf '┌─────────────────────────────────────────────────────────┐\n'
  printf '│ 다음 작업을 완료하세요:                                   │\n'
  printf '│  1. adb connect <server>:5555                           │\n'
  printf '│  2. scrcpy --tcpip=<server>:5555                        │\n'
  printf '│  3. KakaoTalk 설치 및 로그인                              │\n'
  printf '│  4. Iris APK 설치 및 실행                                │\n'
  printf '│  5. Iris 대시��드 확인 (http://<server>:3000/dashboard)  │\n'
  printf '└─────────────────────────────────────────────────────────┘\n'
  printf '\n완료했으면 Enter를 누르세요... '
  read _dummy
fi

# Step 5: Start JucoBot
step 5 "JucoBot 컨테이너 시작 (스모크 테스트 포함)" \
  "$SCRIPT_DIR/remote-up-jucobot.sh"

printf '\n\033[32m배포 완료!\033[0m\n'

#!/bin/sh
# validate-env.sh — 배포 환경변수 및 설정 사전검증
# 원격 서버의 /opt/jucobot/shared/ 디렉토리에서 실행
set -eu

SHARED_DIR=${SHARED_DIR:-/opt/jucobot/shared}
ERRORS=""
WARNINGS=""

error() { ERRORS="${ERRORS}  - $1\n"; }
warn()  { WARNINGS="${WARNINGS}  - $1\n"; }

# ─── 1. 필수 파일 존재 확인 ───

for f in jucobot.env config.yaml redroid.env; do
  if [ ! -f "$SHARED_DIR/$f" ]; then
    error "missing file: $SHARED_DIR/$f"
  fi
done

# ─── 2. 필수 환경변수 검증 ───

if [ -f "$SHARED_DIR/jucobot.env" ]; then
  . "$SHARED_DIR/jucobot.env"

  check_required() {
    var_name=$1
    description=$2
    eval "val=\${$var_name:-}"
    if [ -z "$val" ] || echo "$val" | grep -q '^replace-with-'; then
      error "$var_name is not set ($description)"
    fi
  }

  check_conditional() {
    condition_var=$1
    condition_val=$2
    var_name=$3
    description=$4
    eval "cond=\${$condition_var:-}"
    if [ "$cond" = "$condition_val" ]; then
      check_required "$var_name" "$description"
    fi
  }

  # Core
  check_required "JUCOBOT_COMPOSE_PROJECT_NAME" "Docker Compose project name"
  check_required "JUCOBOT_CONTAINER_NAME" "Container name"
  check_required "JUCOBOT_STACK_NETWORK" "Docker network name"

  # Iris (conditional)
  check_conditional "JUCOBOT_IRIS_ENABLED" "true" "JUCOBOT_IRIS_WS_URL" "Iris WebSocket URL"
  check_conditional "JUCOBOT_IRIS_ENABLED" "true" "JUCOBOT_IRIS_HTTP_URL" "Iris HTTP URL"
  check_conditional "JUCOBOT_IRIS_ENABLED" "true" "IRIS_HEALTHCHECK_URL" "Iris healthcheck URL"

  # Admin metrics (conditional)
  check_conditional "JUCOBOT_ADMIN_METRICS_ENABLED" "true" "JUCOBOT_ADMIN_PSEUDONYM_SECRET" "HMAC pseudonym secret"

  # Admin dashboard (conditional)
  check_conditional "JUCOBOT_ADMIN_ENABLED" "true" "JUCOBOT_ADMIN_ALLOWED_EMAILS" "Dashboard allowed emails"

  # Alerts
  check_required "JUCOBOT_ALERT_TELEGRAM_BOT_TOKEN" "Telegram bot token"
  check_required "JUCOBOT_ALERT_TELEGRAM_CHAT_ID" "Telegram chat ID"

  # Alertd (conditional)
  check_conditional "JUCOBOT_ALERTD_ENABLED" "true" "ALERTD_TELEGRAM_BOT_TOKEN" "Alertd Telegram bot token"
  check_conditional "JUCOBOT_ALERTD_ENABLED" "true" "ALERTD_TELEGRAM_CHAT_ID" "Alertd Telegram chat ID"
fi

# ─── 3. YAML 문법 검증 ───

if [ -f "$SHARED_DIR/config.yaml" ]; then
  if command -v python3 >/dev/null 2>&1; then
    if ! python3 -c "import yaml; yaml.safe_load(open('$SHARED_DIR/config.yaml'))" 2>/dev/null; then
      error "config.yaml has YAML syntax errors"
    fi
  elif command -v yq >/dev/null 2>&1; then
    if ! yq '.' "$SHARED_DIR/config.yaml" >/dev/null 2>&1; then
      error "config.yaml has YAML syntax errors"
    fi
  else
    warn "no YAML validator available (python3 or yq). Skipping YAML syntax check."
  fi
fi

# ─── 4. 네트워크 검증 (선택적) ───

if [ "${CHECK_NETWORK:-0}" = "1" ]; then
  if [ -n "${IRIS_HEALTHCHECK_URL:-}" ] && [ "${JUCOBOT_IRIS_ENABLED:-}" = "true" ]; then
    if ! curl -sf --max-time 5 "$IRIS_HEALTHCHECK_URL" >/dev/null 2>&1; then
      warn "Iris dashboard not reachable at $IRIS_HEALTHCHECK_URL"
    fi
  fi
fi

# ─── 결과 출력 ───

if [ -n "$WARNINGS" ]; then
  printf '\033[33mWarnings:\033[0m\n%b' "$WARNINGS"
fi

if [ -n "$ERRORS" ]; then
  printf '\033[31mValidation FAILED:\033[0m\n%b' "$ERRORS"
  exit 1
fi

printf '\033[32mValidation passed.\033[0m\n'

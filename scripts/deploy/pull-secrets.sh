#!/bin/sh
# pull-secrets.sh — AWS SSM Parameter Store에서 시크릿을 가져와 env 파일에 주입
set -eu

SHARED_DIR=${SHARED_DIR:-/opt/jucobot/shared}
SSM_PREFIX=${SSM_PREFIX:-/jucobot/prod}
ENV_FILE="$SHARED_DIR/jucobot.env"
AWS_REGION=${AWS_REGION:-ap-northeast-2}

if ! command -v aws >/dev/null 2>&1; then
  echo "AWS CLI not found. Install: https://docs.aws.amazon.com/cli/latest/userguide/install-cliv2.html" >&2
  echo "Fallback: manually edit $ENV_FILE" >&2
  exit 1
fi

if ! aws sts get-caller-identity >/dev/null 2>&1; then
  echo "AWS credentials not configured. Run: aws configure" >&2
  echo "Fallback: manually edit $ENV_FILE" >&2
  exit 1
fi

if [ ! -f "$ENV_FILE" ]; then
  echo "env file not found: $ENV_FILE" >&2
  echo "Run deploy-remote first to create the env file from template." >&2
  exit 1
fi

# SSM 파라미터 이름 → 환경변수 매핑
apply_secret() {
  ssm_name=$1
  env_var=$2

  value=$(aws ssm get-parameter \
    --name "$SSM_PREFIX/$ssm_name" \
    --with-decryption \
    --query "Parameter.Value" \
    --output text \
    --region "$AWS_REGION" 2>/dev/null) || {
    echo "  WARNING: SSM parameter $SSM_PREFIX/$ssm_name not found, skipping $env_var" >&2
    return 0
  }

  if grep -q "^${env_var}=" "$ENV_FILE"; then
    sed -i.bak "s|^${env_var}=.*|${env_var}=${value}|" "$ENV_FILE"
  else
    echo "${env_var}=${value}" >> "$ENV_FILE"
  fi
  echo "  OK: $env_var"
}

echo "Pulling secrets from AWS SSM ($SSM_PREFIX)..."

apply_secret "admin-pseudonym-secret"  "JUCOBOT_ADMIN_PSEUDONYM_SECRET"
apply_secret "telegram-bot-token"      "JUCOBOT_ALERT_TELEGRAM_BOT_TOKEN"
apply_secret "telegram-chat-id"        "JUCOBOT_ALERT_TELEGRAM_CHAT_ID"
apply_secret "telegram-bot-token"      "ALERTD_TELEGRAM_BOT_TOKEN"
apply_secret "telegram-chat-id"        "ALERTD_TELEGRAM_CHAT_ID"
apply_secret "oauth2-client-id"        "OAUTH2_PROXY_CLIENT_ID"
apply_secret "oauth2-client-secret"    "OAUTH2_PROXY_CLIENT_SECRET"
apply_secret "oauth2-cookie-secret"    "OAUTH2_PROXY_COOKIE_SECRET"
apply_secret "gemini-api-key"          "JUCOBOT_GEMINI_API_KEY"
apply_secret "coupang-proxy-url"       "JUCOBOT_COUPANG_SCRAPER_PROXY_URL"

rm -f "$ENV_FILE.bak"
echo "Done. Secrets injected into $ENV_FILE"

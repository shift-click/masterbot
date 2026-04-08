# ssm.tf — AWS SSM Parameter Store for JucoBot secrets
# 사용법: terraform.tfvars에 시크릿 값을 설정하고 terraform apply

locals {
  ssm_prefix = "/${var.project_name}/prod"

  # 필수 시크릿 (반드시 설정해야 함)
  required_secrets = {
    "admin-pseudonym-secret"  = var.admin_pseudonym_secret
    "telegram-bot-token"      = var.telegram_bot_token
    "telegram-chat-id"        = var.telegram_chat_id
  }

  # 선택 시크릿 (비어있으면 생성하지 않음)
  optional_secrets_raw = {
    "oauth2-client-id"     = var.oauth2_client_id
    "oauth2-client-secret" = var.oauth2_client_secret
    "oauth2-cookie-secret" = var.oauth2_cookie_secret
    "gemini-api-key"       = var.gemini_api_key
    "coupang-proxy-url"    = var.coupang_proxy_url
  }

  optional_secrets = {
    for k, v in local.optional_secrets_raw : k => v if v != ""
  }

  all_secrets = merge(local.required_secrets, local.optional_secrets)
}

resource "aws_ssm_parameter" "secrets" {
  for_each = local.all_secrets

  name  = "${local.ssm_prefix}/${each.key}"
  type  = "SecureString"
  value = each.value

  tags = {
    Project = var.project_name
    ManagedBy = "terraform"
  }

  lifecycle {
    ignore_changes = [value]
  }
}

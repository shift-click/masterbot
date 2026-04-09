variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "ap-northeast-2"
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.small"
}

variable "volume_size" {
  description = "Root EBS volume size in GB"
  type        = number
  default     = 20
}

variable "ssh_public_key_path" {
  description = "Path to SSH public key for EC2 access"
  type        = string
  default     = "~/.ssh/id_ed25519.pub"
}

variable "admin_cidr_blocks" {
  description = "CIDR blocks allowed for SSH access"
  type        = list(string)
}

variable "enable_admin_dashboard" {
  description = "Whether to expose admin dashboard port (8080)"
  type        = bool
  default     = false
}

variable "tailscale_auth_key" {
  description = "Tailscale pre-auth key for automatic VPN enrollment"
  type        = string
  sensitive   = true
}

variable "project_name" {
  description = "Project name used for resource naming and tags"
  type        = string
  default     = "jucobot"
}

variable "github_repository" {
  description = "GitHub repository allowed to assume the deploy role"
  type        = string
  default     = "shift-click/masterbot"
}

variable "github_deploy_branch" {
  description = "GitHub branch allowed for production deploy"
  type        = string
  default     = "main"
}

variable "github_deploy_environment" {
  description = "GitHub environment name allowed for production deploy"
  type        = string
  default     = "production"
}

variable "github_oidc_provider_arn" {
  description = "Existing GitHub OIDC provider ARN. Leave empty to create one."
  type        = string
  default     = ""
}

variable "github_oidc_thumbprints" {
  description = "Thumbprints for the GitHub Actions OIDC provider"
  type        = list(string)
  default     = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

variable "deploy_artifact_bucket_name" {
  description = "Optional override for the deploy artifact S3 bucket name"
  type        = string
  default     = ""
}

# --- SSM Secrets ---

variable "admin_pseudonym_secret" {
  description = "HMAC secret for admin pseudonym hashing"
  type        = string
  sensitive   = true
  default     = ""
}

variable "telegram_bot_token" {
  description = "Telegram bot API token for alerts"
  type        = string
  sensitive   = true
  default     = ""
}

variable "telegram_chat_id" {
  description = "Telegram chat ID for alert delivery"
  type        = string
  sensitive   = true
  default     = ""
}

variable "oauth2_client_id" {
  description = "Google OAuth2 client ID (admin dashboard)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "oauth2_client_secret" {
  description = "Google OAuth2 client secret (admin dashboard)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "oauth2_cookie_secret" {
  description = "OAuth2 proxy cookie encryption secret (32-byte base64)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "gemini_api_key" {
  description = "Google Gemini API key (YouTube summarization)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "coupang_proxy_url" {
  description = "Coupang scraper residential proxy URL"
  type        = string
  sensitive   = true
  default     = ""
}

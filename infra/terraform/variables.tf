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

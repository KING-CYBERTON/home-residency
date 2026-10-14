variable "environment" {
  description = "staging or production. Also the GitHub environment name the deploy role trusts."
  type        = string
}

variable "github_repository" {
  type    = string
  default = "KING-CYBERTON/home-residency"
}

variable "ecr_repository_name" {
  type    = string
  default = "homend-api"
}

# ---- Network

variable "vpc_cidr" {
  type    = string
  default = "10.20.0.0/16"
}

# ---- Application

variable "app_env" {
  description = "APP_ENV for the container. \"prod\" turns on the production guards in internal/config."
  type        = string
}

variable "public_base_url" {
  description = "Public origin of the API, e.g. https://api.example.co.ke. Leave empty to use the load balancer's DNS name over HTTP."
  type        = string
  default     = ""
}

variable "allowed_origins" {
  description = "Browser origins, besides public_base_url, allowed to call the API and to upload to and download from the document vault with presigned URLs (the public site if it is on another host, the console)."
  type        = list(string)
  default     = []
}

variable "sms_provider" {
  type    = string
  default = "log"
}

variable "mpesa_mode" {
  description = "fake, sandbox or production. sandbox and production need the M-Pesa secret filled in first."
  type        = string
  default     = "fake"
}

variable "mpesa_shortcode" {
  type    = string
  default = ""
}

variable "mpesa_b2c_shortcode" {
  type    = string
  default = ""
}

variable "webhook_allowed_cidrs" {
  description = "Source ranges allowed to call the webhooks (Safaricom egress ranges). Empty allows any source with the token."
  type        = list(string)
  default     = []
}

variable "vapid_subject" {
  description = "Contact for web push services (VAPID \"sub\"), e.g. mailto:ops@example.co.ke."
  type        = string
  default     = ""
}

variable "extra_environment" {
  description = "Any other plain environment variables for the container."
  type        = map(string)
  default     = {}
}

# ---- Compute

variable "task_cpu" {
  type    = number
  default = 512
}

variable "task_memory" {
  type    = number
  default = 1024
}

variable "desired_count" {
  type    = number
  default = 2
}

variable "max_count" {
  description = "Upper bound for CPU-based autoscaling."
  type        = number
  default     = 4
}

variable "log_retention_days" {
  type    = number
  default = 90
}

# ---- Load balancer

variable "certificate_arn" {
  description = "ACM certificate for HTTPS. Without one the load balancer only listens on HTTP (acceptable for a first staging bring-up only)."
  type        = string
  default     = ""
}

variable "waf_rate_limit" {
  description = "Requests per IP per 5 minutes before WAF blocks it."
  type        = number
  default     = 1000
}

# ---- Database

variable "db_instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "db_allocated_storage" {
  type    = number
  default = 20
}

variable "db_multi_az" {
  type    = bool
  default = false
}

variable "db_backup_retention_days" {
  type    = number
  default = 7
}

variable "deletion_protection" {
  description = "Protects the database, keeps a final snapshot and a 30-day secret recovery window. Turn on for production."
  type        = bool
  default     = false
}

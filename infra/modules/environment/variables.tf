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
  description = "CIDR for the VPC this module creates. Ignored when existing_vpc_id is set."
  type        = string
  default     = "10.20.0.0/16"
}

variable "existing_vpc_id" {
  description = "Use this VPC instead of creating one. Empty creates the VPC, subnets and NAT gateway."
  type        = string
  default     = ""
}

variable "existing_public_subnet_ids" {
  description = "With existing_vpc_id: public subnets for the load balancer, in at least two AZs."
  type        = list(string)
  default     = []
  validation {
    condition     = var.existing_vpc_id == "" || length(var.existing_public_subnet_ids) >= 2
    error_message = "existing_vpc_id needs at least two public subnets, in different AZs."
  }
}

variable "existing_private_subnet_ids" {
  description = "With existing_vpc_id: private subnets with outbound HTTPS for the tasks and the database, in at least two AZs."
  type        = list(string)
  default     = []
  validation {
    condition     = var.existing_vpc_id == "" || length(var.existing_private_subnet_ids) >= 2
    error_message = "existing_vpc_id needs at least two private subnets, in different AZs."
  }
}

variable "tasks_in_public_subnets" {
  description = <<-EOT
    Run the API and migration tasks in the public subnets with public IPs instead of the
    private subnets, so they reach ECR, Secrets Manager and M-Pesa without a NAT gateway.
    The app security group still only accepts traffic from the load balancer. Set this
    back to false once the private subnets have a NAT gateway.
  EOT
  type        = bool
  default     = false
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
  description = "log (codes in the task log; not allowed with app_env prod) or none (no SMS: households and providers sign in with email and password)."
  type        = string
  default     = "log"
}

variable "email_from" {
  description = "Address password reset emails come from, e.g. no-reply@example.co.ke. Its domain is created as an SES identity; empty logs emails instead of sending them (not allowed with app_env prod)."
  type        = string
  default     = ""
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

variable "console_domain" {
  description = "Host name for the staff console, e.g. console.example.co.ke. Empty uses the CloudFront domain."
  type        = string
  default     = ""
}

variable "console_certificate_arn" {
  description = "ACM certificate for console_domain. CloudFront requires it to be in us-east-1."
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

variable "db_storage_autoscaling" {
  description = "Let RDS grow storage up to five times db_allocated_storage."
  type        = bool
  default     = true
}

variable "deletion_protection" {
  description = "Protects the database, keeps a final snapshot and a 30-day secret recovery window. Turn on for production."
  type        = bool
  default     = false
}

variable "github_oidc_subject_prefix" {
  type    = string
  default = "repo:KING-CYBERTON@82710182/home-residency@1418283878"
}

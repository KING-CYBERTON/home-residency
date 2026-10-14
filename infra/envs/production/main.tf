variable "region" {
  type    = string
  default = "af-south-1"
}

variable "public_base_url" {
  description = "e.g. https://api.example.co.ke"
  type        = string
}

variable "certificate_arn" {
  description = "ACM certificate for public_base_url's host, in the same region."
  type        = string
}

variable "allowed_origins" {
  type    = list(string)
  default = []
}

variable "sms_provider" {
  description = "Must not be \"log\" (refused when APP_ENV=prod). See the note in infra/README.md."
  type        = string
}

variable "mpesa_shortcode" {
  type = string
}

variable "mpesa_b2c_shortcode" {
  type    = string
  default = ""
}

variable "webhook_allowed_cidrs" {
  description = "Safaricom's published callback source ranges."
  type        = list(string)
  default     = []
}

module "env" {
  source = "../../modules/environment"

  environment     = "production"
  vpc_cidr        = "10.30.0.0/16"
  app_env         = "prod"
  public_base_url = var.public_base_url
  certificate_arn = var.certificate_arn
  allowed_origins = var.allowed_origins

  sms_provider          = var.sms_provider
  mpesa_mode            = "production"
  mpesa_shortcode       = var.mpesa_shortcode
  mpesa_b2c_shortcode   = var.mpesa_b2c_shortcode
  webhook_allowed_cidrs = var.webhook_allowed_cidrs

  desired_count            = 2
  max_count                = 6
  db_instance_class        = "db.t4g.small"
  db_allocated_storage     = 50
  db_multi_az              = true
  db_backup_retention_days = 30
  deletion_protection      = true
  log_retention_days       = 365
}

output "github_environment_variables" {
  value = module.env.github_environment_variables
}

output "load_balancer_dns_name" {
  value = module.env.load_balancer_dns_name
}

output "mpesa_secret_name" {
  value = module.env.mpesa_secret_name
}

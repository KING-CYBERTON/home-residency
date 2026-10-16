variable "region" {
  type    = string
  default = "eu-central-1"
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
  description = "Other browser origins, e.g. the public site if it is not on public_base_url's host, and the console."
  type        = list(string)
  default     = []
}

variable "console_domain" {
  description = "e.g. console.example.co.ke. Empty uses the CloudFront domain."
  type        = string
  default     = ""
}

variable "console_certificate_arn" {
  description = "ACM certificate for console_domain, in us-east-1."
  type        = string
  default     = ""
}

variable "vapid_subject" {
  description = "Web push contact, e.g. mailto:ops@example.co.ke"
  type        = string
  default     = ""
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

variable "existing_vpc_id" {
  description = "Use this VPC instead of creating one (see modules/environment/network.tf)."
  type        = string
  default     = ""
}

variable "existing_public_subnet_ids" {
  type    = list(string)
  default = []
}

variable "existing_private_subnet_ids" {
  type    = list(string)
  default = []
}

variable "tasks_in_public_subnets" {
  description = "Run the tasks in the public subnets instead of behind a NAT gateway (see modules/environment/variables.tf)."
  type        = bool
  default     = false
}

variable "db_backup_retention_days" {
  description = "Days of database backups. 1 fits an AWS Free plan account; raise it (for example to 30) once on a paid plan."
  type        = number
  default     = 1
}

module "env" {
  source = "../../modules/environment"
  providers = {
    aws           = aws
    aws.us_east_1 = aws.us_east_1
  }

  environment                 = "production"
  vpc_cidr                    = "10.30.0.0/16"
  existing_vpc_id             = var.existing_vpc_id
  existing_public_subnet_ids  = var.existing_public_subnet_ids
  existing_private_subnet_ids = var.existing_private_subnet_ids
  tasks_in_public_subnets     = var.tasks_in_public_subnets
  app_env                     = "prod"
  public_base_url             = var.public_base_url
  certificate_arn             = var.certificate_arn
  allowed_origins             = var.allowed_origins
  vapid_subject               = var.vapid_subject

  console_domain          = var.console_domain
  console_certificate_arn = var.console_certificate_arn

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
  db_backup_retention_days = var.db_backup_retention_days
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

output "push_secret_name" {
  value = module.env.push_secret_name
}

output "console_url" {
  value = module.env.console_url
}

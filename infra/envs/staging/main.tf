variable "region" {
  type    = string
  default = "eu-central-1"
}

variable "public_base_url" {
  description = "e.g. https://api.staging.example.co.ke. Empty uses the load balancer's DNS name over HTTP."
  type        = string
  default     = ""
}

variable "certificate_arn" {
  type    = string
  default = ""
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

module "env" {
  source = "../../modules/environment"
  providers = {
    aws           = aws
    aws.us_east_1 = aws.us_east_1
  }

  environment                 = "staging"
  vpc_cidr                    = "10.20.0.0/16"
  existing_vpc_id             = var.existing_vpc_id
  existing_public_subnet_ids  = var.existing_public_subnet_ids
  existing_private_subnet_ids = var.existing_private_subnet_ids
  tasks_in_public_subnets     = var.tasks_in_public_subnets
  app_env                     = "staging"
  public_base_url             = var.public_base_url
  certificate_arn             = var.certificate_arn
  allowed_origins             = var.allowed_origins
  vapid_subject               = var.vapid_subject

  console_domain          = var.console_domain
  console_certificate_arn = var.console_certificate_arn

  # Staging keeps the fake M-Pesa client and log SMS until sandbox keys are in the
  # homend/staging/mpesa secret; then set mpesa_mode = "sandbox".
  mpesa_mode   = "fake"
  sms_provider = "log"

  desired_count       = 1
  max_count           = 2
  db_instance_class   = "db.t4g.micro"
  db_multi_az         = false
  deletion_protection = false
  log_retention_days  = 30
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

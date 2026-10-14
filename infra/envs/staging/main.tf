variable "region" {
  type    = string
  default = "af-south-1"
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
  type    = list(string)
  default = []
}

module "env" {
  source = "../../modules/environment"

  environment     = "staging"
  vpc_cidr        = "10.20.0.0/16"
  app_env         = "staging"
  public_base_url = var.public_base_url
  certificate_arn = var.certificate_arn
  allowed_origins = var.allowed_origins

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

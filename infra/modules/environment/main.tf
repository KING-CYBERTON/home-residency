data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

data "aws_ecr_repository" "api" {
  name = var.ecr_repository_name
}

data "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"
}

locals {
  name       = "homend-${var.environment}"
  account_id = data.aws_caller_identity.current.account_id
  region     = data.aws_region.current.region

  public_base_url = var.public_base_url != "" ? var.public_base_url : "http://${aws_lb.api.dns_name}"
}

# One key per environment for the database, the document vault and the secrets.
resource "aws_kms_key" "main" {
  description             = "${local.name}: RDS, document vault and application secrets"
  enable_key_rotation     = true
  deletion_window_in_days = 30
}

resource "aws_kms_alias" "main" {
  name          = "alias/${local.name}"
  target_key_id = aws_kms_key.main.key_id
}

output "ecr_repository_url" {
  value = aws_ecr_repository.api.repository_url
}

output "github_oidc_provider_arn" {
  value = aws_iam_openid_connect_provider.github.arn
}

output "github_repository_variables" {
  description = "Set these as repository variables in GitHub (Settings > Secrets and variables > Actions)."
  value = {
    AWS_REGION            = var.region
    AWS_ECR_PUSH_ROLE_ARN = aws_iam_role.ecr_push.arn
    ECR_REPOSITORY        = aws_ecr_repository.api.name
  }
}

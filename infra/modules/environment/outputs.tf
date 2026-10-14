output "public_base_url" {
  value = local.public_base_url
}

output "load_balancer_dns_name" {
  description = "Point the API's DNS record (CNAME or alias) here."
  value       = aws_lb.api.dns_name
}

output "mpesa_secret_name" {
  description = "Fill these values in Secrets Manager before using MPESA_MODE sandbox or production."
  value       = aws_secretsmanager_secret.mpesa.name
}

output "push_secret_name" {
  description = "Put the VAPID key pair here (VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY)."
  value       = aws_secretsmanager_secret.push.name
}

output "github_environment_variables" {
  description = "Set these on the matching GitHub environment (Settings > Environments)."
  value = {
    AWS_DEPLOY_ROLE_ARN = aws_iam_role.deploy.arn
    ECS_CLUSTER         = aws_ecs_cluster.main.name
    ECS_SERVICE         = aws_ecs_service.api.name
    ECS_TASK_FAMILY     = aws_ecs_task_definition.api.family
    ECS_CONTAINER_NAME  = "api"
    ECS_SUBNETS         = join(",", aws_subnet.private[*].id)
    ECS_SECURITY_GROUPS = aws_security_group.app.id
    PUBLIC_BASE_URL     = local.public_base_url
  }
}

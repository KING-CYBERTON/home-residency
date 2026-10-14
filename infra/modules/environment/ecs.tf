resource "aws_ecs_cluster" "main" {
  name = local.name
  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

resource "aws_cloudwatch_log_group" "api" {
  name              = "/ecs/${local.name}/api"
  retention_in_days = var.log_retention_days
}

locals {
  container_environment = merge({
    APP_ENV               = var.app_env
    HTTP_ADDR             = ":8080"
    COOKIE_SECURE         = local.https ? "true" : "false"
    PUBLIC_BASE_URL       = local.public_base_url
    ALLOWED_ORIGINS       = join(",", var.allowed_origins)
    STORAGE_DRIVER        = "s3"
    STORAGE_BUCKET        = aws_s3_bucket.vault.bucket
    STORAGE_KMS_KEY_ID    = aws_kms_key.main.arn
    SMS_PROVIDER          = var.sms_provider
    MPESA_MODE            = var.mpesa_mode
    MPESA_SHORTCODE       = var.mpesa_shortcode
    MPESA_B2C_SHORTCODE   = var.mpesa_b2c_shortcode
    WEBHOOK_ALLOWED_CIDRS = join(",", var.webhook_allowed_cidrs)
    VAPID_SUBJECT         = var.vapid_subject
  }, var.extra_environment)

  container_secrets = concat(
    [for k in local.app_secret_keys : { name = k, valueFrom = "${aws_secretsmanager_secret.app.arn}:${k}::" }],
    [for k in local.mpesa_secret_keys : { name = k, valueFrom = "${aws_secretsmanager_secret.mpesa.arn}:${k}::" }],
    [for k in local.push_secret_keys : { name = k, valueFrom = "${aws_secretsmanager_secret.push.arn}:${k}::" }],
  )
}

# Terraform owns the configuration (environment, secrets, sizing, roles). The deploy
# workflow owns the image: it copies the latest revision of this family and swaps the
# image, so configuration changes applied here go live with the next deploy.
# Until the first deploy, the service points at a placeholder tag that does not exist.
resource "aws_ecs_task_definition" "api" {
  family                   = "homend-api-${var.environment}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn
  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "X86_64"
  }
  container_definitions = jsonencode([{
    name         = "api"
    image        = "${data.aws_ecr_repository.api.repository_url}:not-deployed-yet"
    essential    = true
    portMappings = [{ containerPort = 8080, protocol = "tcp" }]
    environment  = [for k, v in local.container_environment : { name = k, value = v }]
    secrets      = local.container_secrets
    stopTimeout  = 30
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.api.name
        awslogs-region        = local.region
        awslogs-stream-prefix = "api"
      }
    }
  }])
}

resource "aws_ecs_service" "api" {
  name                              = "homend-api"
  cluster                           = aws_ecs_cluster.main.id
  task_definition                   = aws_ecs_task_definition.api.arn
  desired_count                     = var.desired_count
  launch_type                       = "FARGATE"
  health_check_grace_period_seconds = 60
  propagate_tags                    = "SERVICE"

  deployment_minimum_healthy_percent = 100
  deployment_maximum_percent         = 200
  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.app.id]
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.api.arn
    container_name   = "api"
    container_port   = 8080
  }

  # The deploy workflow moves task_definition; autoscaling moves desired_count.
  lifecycle {
    ignore_changes = [task_definition, desired_count]
  }

  depends_on = [aws_lb_listener.http]
}

resource "aws_appautoscaling_target" "api" {
  service_namespace  = "ecs"
  resource_id        = "service/${aws_ecs_cluster.main.name}/${aws_ecs_service.api.name}"
  scalable_dimension = "ecs:service:DesiredCount"
  min_capacity       = var.desired_count
  max_capacity       = var.max_count
}

resource "aws_appautoscaling_policy" "api_cpu" {
  name               = "${local.name}-cpu"
  service_namespace  = aws_appautoscaling_target.api.service_namespace
  resource_id        = aws_appautoscaling_target.api.resource_id
  scalable_dimension = aws_appautoscaling_target.api.scalable_dimension
  policy_type        = "TargetTrackingScaling"
  target_tracking_scaling_policy_configuration {
    target_value = 60
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
  }
}

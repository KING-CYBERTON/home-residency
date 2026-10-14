# Three secrets per environment, all read by the task's execution role:
#   homend/<env>/app    generated here: DATABASE_URL, FIELD_ENCRYPTION_KEY, WEBHOOK_TOKEN
#   homend/<env>/mpesa  created with empty values; fill them in the console. Terraform
#                       never overwrites them.
#   homend/<env>/push   VAPID key pair for web push, created empty the same way.
#                       Terraform cannot produce the raw P-256 keys web push expects;
#                       generate them with `npx web-push generate-vapid-keys`.

# FIELD_ENCRYPTION_KEY encrypts the *_enc columns. Replacing it makes existing data
# unreadable, so never taint or replace this resource. (The KMS-wrapped key in
# ADR-0002's "known gaps" will replace it.)
resource "random_bytes" "field_key" {
  length = 32
}

resource "random_password" "webhook_token" {
  length  = 48
  special = false
}

resource "aws_secretsmanager_secret" "app" {
  name                    = "homend/${var.environment}/app"
  kms_key_id              = aws_kms_key.main.arn
  recovery_window_in_days = var.deletion_protection ? 30 : 0
}

resource "aws_secretsmanager_secret_version" "app" {
  secret_id = aws_secretsmanager_secret.app.id
  secret_string = jsonencode({
    DATABASE_URL         = "postgres://${aws_db_instance.main.username}:${random_password.db.result}@${aws_db_instance.main.address}:${aws_db_instance.main.port}/${aws_db_instance.main.db_name}?sslmode=require"
    FIELD_ENCRYPTION_KEY = random_bytes.field_key.base64
    WEBHOOK_TOKEN        = random_password.webhook_token.result
  })
}

locals {
  mpesa_secret_keys = ["MPESA_CONSUMER_KEY", "MPESA_CONSUMER_SECRET", "MPESA_PASSKEY", "MPESA_INITIATOR", "MPESA_SECURITY_CREDENTIAL"]
  app_secret_keys   = ["DATABASE_URL", "FIELD_ENCRYPTION_KEY", "WEBHOOK_TOKEN"]
  push_secret_keys  = ["VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY"]
}

resource "aws_secretsmanager_secret" "mpesa" {
  name                    = "homend/${var.environment}/mpesa"
  kms_key_id              = aws_kms_key.main.arn
  recovery_window_in_days = var.deletion_protection ? 30 : 0
}

resource "aws_secretsmanager_secret_version" "mpesa" {
  secret_id     = aws_secretsmanager_secret.mpesa.id
  secret_string = jsonencode({ for k in local.mpesa_secret_keys : k => "" })
  lifecycle {
    ignore_changes = [secret_string]
  }
}

resource "aws_secretsmanager_secret" "push" {
  name                    = "homend/${var.environment}/push"
  kms_key_id              = aws_kms_key.main.arn
  recovery_window_in_days = var.deletion_protection ? 30 : 0
}

resource "aws_secretsmanager_secret_version" "push" {
  secret_id     = aws_secretsmanager_secret.push.id
  secret_string = jsonencode({ for k in local.push_secret_keys : k => "" })
  lifecycle {
    ignore_changes = [secret_string]
  }
}

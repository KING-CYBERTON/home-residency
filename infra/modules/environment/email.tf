# Password reset emails go out through Amazon SES from var.email_from. The sending domain is an
# SES identity verified by the DKIM records in the ses_dns_records output. A new SES account is in
# the sandbox (it can only send to verified addresses) until production access is requested:
# see "Email" in infra/README.md.

locals {
  email_domain = var.email_from == "" ? "" : split("@", var.email_from)[1]
}

resource "aws_sesv2_email_identity" "mail" {
  count          = local.email_domain == "" ? 0 : 1
  email_identity = local.email_domain
}

resource "aws_iam_role_policy" "task_email" {
  count = local.email_domain == "" ? 0 : 1
  name  = "send-email"
  role  = aws_iam_role.task.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "ses:SendEmail"
      Resource  = "*"
      Condition = { StringEquals = { "ses:FromAddress" = var.email_from } }
    }]
  })
}

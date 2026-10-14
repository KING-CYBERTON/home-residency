# Per-IP rate limit in front of the API (ADR-0002 known gap: staff login and per-IP OTP limits).
# Requests from the console's CloudFront distribution all come from CloudFront's
# addresses, so they are left to the CloudFront WAF (console.tf), which sees the
# viewer's IP. They are recognised by the secret X-Origin-Verify header.
resource "aws_wafv2_web_acl" "api" {
  name  = local.name
  scope = "REGIONAL"
  default_action {
    allow {}
  }
  rule {
    name     = "rate-limit-per-ip"
    priority = 1
    action {
      block {}
    }
    statement {
      rate_based_statement {
        limit              = var.waf_rate_limit
        aggregate_key_type = "IP"
        scope_down_statement {
          not_statement {
            statement {
              byte_match_statement {
                search_string         = random_password.origin_verify.result
                positional_constraint = "EXACTLY"
                field_to_match {
                  single_header {
                    name = "x-origin-verify"
                  }
                }
                text_transformation {
                  priority = 0
                  type     = "NONE"
                }
              }
            }
          }
        }
      }
    }
    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-rate-limit"
      sampled_requests_enabled   = true
    }
  }
  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = local.name
    sampled_requests_enabled   = true
  }
}

resource "aws_wafv2_web_acl_association" "api" {
  resource_arn = aws_lb.api.arn
  web_acl_arn  = aws_wafv2_web_acl.api.arn
}

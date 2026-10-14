# The staff console (console/, ADR-0001): static files in a private bucket behind
# CloudFront, on the same origin as the API. CloudFront sends /api/* to the load
# balancer and everything else to S3, so the console needs no CORS.

locals {
  api_host    = var.public_base_url != "" ? regex("^https?://([^/:]+)", var.public_base_url)[0] : aws_lb.api.dns_name
  console_url = var.console_domain != "" ? "https://${var.console_domain}" : "https://${aws_cloudfront_distribution.console.domain_name}"
}

resource "aws_s3_bucket" "console" {
  bucket = "${local.name}-console-${local.account_id}"
}

resource "aws_s3_bucket_ownership_controls" "console" {
  bucket = aws_s3_bucket.console.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_public_access_block" "console" {
  bucket                  = aws_s3_bucket.console.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "console" {
  bucket = aws_s3_bucket.console.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_policy" "console" {
  bucket = aws_s3_bucket.console.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "CloudFrontReadOnly"
      Effect    = "Allow"
      Principal = { Service = "cloudfront.amazonaws.com" }
      Action    = "s3:GetObject"
      Resource  = "${aws_s3_bucket.console.arn}/*"
      Condition = { StringEquals = { "AWS:SourceArn" = aws_cloudfront_distribution.console.arn } }
    }]
  })
  depends_on = [aws_s3_bucket_public_access_block.console]
}

resource "aws_cloudfront_origin_access_control" "console" {
  name                              = "${local.name}-console"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# Sent by CloudFront to the load balancer so the load balancer's WAF can tell console
# traffic (already rate-limited per viewer IP at CloudFront) from direct traffic.
resource "random_password" "origin_verify" {
  length  = 48
  special = false
}

# Single-page app fallback: paths without a file extension get index.html. Done here,
# on the S3 behaviour only, rather than with custom error responses, which apply to
# the whole distribution and would turn the API's own 403 and 404 answers into
# index.html with status 200.
resource "aws_cloudfront_function" "spa_rewrite" {
  name    = "${local.name}-console-spa"
  runtime = "cloudfront-js-2.0"
  publish = true
  code    = <<-JS
    function handler(event) {
      var request = event.request;
      var last = request.uri.split("/").pop();
      if (last.indexOf(".") === -1) {
        request.uri = "/index.html";
      }
      return request;
    }
  JS
}

data "aws_cloudfront_cache_policy" "optimized" {
  name = "Managed-CachingOptimized"
}

data "aws_cloudfront_cache_policy" "disabled" {
  name = "Managed-CachingDisabled"
}

data "aws_cloudfront_origin_request_policy" "all_viewer_except_host" {
  name = "Managed-AllViewerExceptHostHeader"
}

# The managed SecurityHeadersPolicy plus a Content-Security-Policy. The console bundle
# has no inline scripts and makes no cross-origin calls (documents open in a new tab),
# so scripts and connections are 'self' only. Inline styles stay allowed for the style
# attributes Radix sets.
resource "aws_cloudfront_response_headers_policy" "console" {
  name = "${local.name}-console-security"
  security_headers_config {
    content_security_policy {
      content_security_policy = join("; ", [
        "default-src 'self'",
        "script-src 'self'",
        "style-src 'self' 'unsafe-inline'",
        "img-src 'self' data: blob:",
        "font-src 'self' data:",
        "connect-src 'self'",
        "object-src 'none'",
        "base-uri 'self'",
        "form-action 'self'",
        "frame-ancestors 'none'",
      ])
      override = true
    }
    content_type_options {
      override = true
    }
    frame_options {
      frame_option = "DENY"
      override     = true
    }
    referrer_policy {
      referrer_policy = "strict-origin-when-cross-origin"
      override        = true
    }
    strict_transport_security {
      access_control_max_age_sec = 31536000
      include_subdomains         = true
      override                   = true
    }
  }
}

resource "aws_cloudfront_distribution" "console" {
  enabled             = true
  comment             = "${local.name} console"
  default_root_object = "index.html"
  aliases             = var.console_domain != "" ? [var.console_domain] : []
  price_class         = "PriceClass_200" # includes the African edge locations
  http_version        = "http2and3"
  web_acl_id          = aws_wafv2_web_acl.console.arn

  origin {
    origin_id                = "console-s3"
    domain_name              = aws_s3_bucket.console.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.console.id
  }

  origin {
    origin_id   = "api-alb"
    domain_name = local.api_host
    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = local.https ? "https-only" : "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
    custom_header {
      name  = "X-Origin-Verify"
      value = random_password.origin_verify.result
    }
  }

  default_cache_behavior {
    target_origin_id           = "console-s3"
    viewer_protocol_policy     = "redirect-to-https"
    allowed_methods            = ["GET", "HEAD", "OPTIONS"]
    cached_methods             = ["GET", "HEAD"]
    compress                   = true
    cache_policy_id            = data.aws_cloudfront_cache_policy.optimized.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.console.id
    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.spa_rewrite.arn
    }
  }

  ordered_cache_behavior {
    path_pattern             = "/api/*"
    target_origin_id         = "api-alb"
    viewer_protocol_policy   = "redirect-to-https"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    compress                 = true
    cache_policy_id          = data.aws_cloudfront_cache_policy.disabled.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer_except_host.id
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = var.console_certificate_arn == ""
    acm_certificate_arn            = var.console_certificate_arn != "" ? var.console_certificate_arn : null
    ssl_support_method             = var.console_certificate_arn != "" ? "sni-only" : null
    minimum_protocol_version       = var.console_certificate_arn != "" ? "TLSv1.2_2021" : null
  }

  lifecycle {
    precondition {
      condition     = !local.https || var.public_base_url != ""
      error_message = "certificate_arn needs public_base_url, so CloudFront can reach the load balancer by the name on its certificate."
    }
    precondition {
      condition     = (var.console_domain == "") == (var.console_certificate_arn == "")
      error_message = "Set console_domain and console_certificate_arn together (the certificate must be in us-east-1)."
    }
  }
}

# CloudFront WAFs live in us-east-1. Rate limit per viewer IP, which the load
# balancer's WAF cannot see for console traffic.
resource "aws_wafv2_web_acl" "console" {
  provider = aws.us_east_1
  name     = "${local.name}-console"
  scope    = "CLOUDFRONT"
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
      }
    }
    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-console-rate-limit"
      sampled_requests_enabled   = true
    }
  }
  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "${local.name}-console"
    sampled_requests_enabled   = true
  }
}

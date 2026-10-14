terraform {
  required_version = ">= 1.10"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
  # Bucket and region come from `terraform init -backend-config=...` (see infra/README.md).
  backend "s3" {
    key          = "global/terraform.tfstate"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  region = var.region
  default_tags {
    tags = { Project = "homend", Environment = "global", ManagedBy = "terraform" }
  }
}

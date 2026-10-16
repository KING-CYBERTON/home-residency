variable "region" {
  type    = string
  default = "eu-central-1"
}

variable "github_repository" {
  description = "owner/repo allowed to push images through GitHub OIDC."
  type        = string
  default     = "KING-CYBERTON/home-residency"
}

variable "ecr_repository_name" {
  type    = string
  default = "homend-api"
}

variable "pull_account_ids" {
  description = "Other AWS account ids allowed to pull images, if production runs in its own account."
  type        = list(string)
  default     = []
}

variable "images_to_keep" {
  description = "How many tagged images to keep in ECR. Older ones can no longer be rolled back to."
  type        = number
  default     = 100
}

variable "github_oidc_subject_prefix" {
  description = "Immutable OIDC subject prefix GitHub issues for this repo (repo:owner@id/name@id)."
  type        = string
  default     = "repo:KING-CYBERTON@82710182/home-residency@1418283878"
}

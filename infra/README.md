# Homend Residence infrastructure (Terraform)

Everything the backend needs on AWS (ADR-0001), as Terraform, in `af-south-1` by default. The deploy workflows in `.github/workflows/backend-deploy*.yml` ship new images onto this; Terraform does not deploy code.

```
infra/
  bootstrap/            S3 bucket for Terraform state (run once per account, local state)
  global/               GitHub OIDC provider, ECR repository, role the build job pushes with
  modules/environment/  one environment: VPC, ALB + WAF, ECS Fargate, RDS PostgreSQL 16,
                        S3 document vault, KMS key, Secrets Manager, IAM, deploy role
  envs/staging/         staging settings (small, fake M-Pesa, no deletion protection)
  envs/production/      production settings (Multi-AZ, deletion protection, HTTPS required)
```

## What each environment gets

| Piece | Staging | Production |
|---|---|---|
| Network | VPC `10.20.0.0/16`, 2 AZs, public + private subnets, 1 NAT gateway | VPC `10.30.0.0/16`, same layout |
| Load balancer | ALB, HTTP until a certificate is given; WAF rate limit per IP | ALB, HTTPS (TLS 1.2+), HTTP redirects; WAF rate limit per IP |
| API | ECS Fargate service `homend-api`, 1 task (scales to 2 on CPU) | 2 tasks (scales to 6 on CPU) |
| Database | RDS PostgreSQL 16, `db.t4g.micro`, single AZ, 7-day backups | `db.t4g.small`, Multi-AZ, 30-day backups, deletion protection, final snapshot |
| Document vault | S3, SSE-KMS, versioned, TLS only, CORS for the app's origins | same |
| Secrets | `homend/staging/app` (generated), `homend/staging/mpesa` (you fill in) | `homend/production/app`, `homend/production/mpesa` |
| Logs | CloudWatch, 30 days | CloudWatch, 365 days |

The tasks, the migration task and the database run in private subnets. Only the load balancer is public.

## First-time setup

You need AWS credentials for an admin in the target account, Terraform 1.10 or newer, and, for production, a domain and an ACM certificate in the same region. `af-south-1` is an opt-in region: enable it in the account first.

1. **State bucket** (once per account):
   ```bash
   cd infra/bootstrap && terraform init && terraform apply
   ```
   Note the `state_bucket` output. Keep `bootstrap/terraform.tfstate` somewhere safe or import the bucket later; it only describes this one bucket.

2. **Shared resources:**
   ```bash
   cd infra/global
   terraform init -backend-config="bucket=<state_bucket>" -backend-config="region=af-south-1"
   terraform apply
   ```
   Copy the `github_repository_variables` output into GitHub as repository variables (Settings → Secrets and variables → Actions → Variables).

3. **Staging:**
   ```bash
   cd infra/envs/staging
   terraform init -backend-config="bucket=<state_bucket>" -backend-config="region=af-south-1"
   terraform apply
   ```
   Create a GitHub environment named `staging` and copy the `github_environment_variables` output into it.

4. **Production:** copy `envs/production/terraform.tfvars.example` to `terraform.tfvars`, fill it in, then init and apply as above. Create the `production` GitHub environment with the output variables, add **required reviewers** and limit deployment branches to `main`. Point your DNS name at `load_balancer_dns_name`.

5. **M-Pesa keys:** put the Daraja values into the `homend/<env>/mpesa` secret in Secrets Manager (keys `MPESA_CONSUMER_KEY`, `MPESA_CONSUMER_SECRET`, `MPESA_PASSKEY`, `MPESA_INITIATOR`, `MPESA_SECURITY_CREDENTIAL`). Terraform creates them empty and never overwrites them. Production will not start without them; staging uses the fake client until you set `mpesa_mode = "sandbox"`.

6. **First deploy:** run **Backend deploy** from the Actions tab on `main` (or merge to `main`). Until then the service points at a placeholder image tag and its tasks fail to start, which is expected.

After the first deploy, create the first staff user by running the task definition once with the command `create-staff -email … -name … -roles admin` (ECS console → Run task, or `aws ecs run-task` with a command override) and read the password and TOTP secret from the task's log.

## Who changes what

- **Terraform** owns configuration: environment variables, secrets, CPU and memory, roles, networking. Changing them registers a new task definition revision; it goes live with the next deploy, which copies the latest revision and swaps in the new image.
- **The deploy workflow** owns the image and which revision the service runs. Terraform ignores the service's `task_definition` and `desired_count` (autoscaling owns the count).

## Secrets and state

Terraform generates the database password, `FIELD_ENCRYPTION_KEY` and `WEBHOOK_TOKEN`, so they are in the state file. The state bucket is private, versioned and encrypted; limit who can read it. `FIELD_ENCRYPTION_KEY` encrypts the `*_enc` columns: never replace the `random_bytes.field_key` resource, or that data becomes unreadable. Moving to a KMS-wrapped data key (ADR-0002 known gaps) removes it from state.

## Still to decide or add

- **SMS sender:** `sms.New` only has the log sender today, so production must be given a `sms_provider` value other than `log` to start, and it will still only log messages until the Africa's Talking sender is wired (ADR-0002 known gaps).
- **SQS and EventBridge** (ADR-0001) are not created yet because nothing in the code uses them; add them with the outbox relay.
- **CloudFront** for the public site and console, and S3 access logging on the vault.
- **Alarms** (5xx rate, unhealthy tasks, RDS CPU and storage) and where they notify.
- **Separate production account:** this layout runs both environments in one account. To split, apply `global/` in the shared account with `pull_account_ids = ["<prod account>"]`, create the OIDC provider in the production account too, and point the production GitHub environment's `AWS_REGION`/role at that account.
- **Provider lock files:** commit each root's `.terraform.lock.hcl` after the first `terraform init` (run `terraform providers lock -platform=linux_amd64 -platform=darwin_arm64` so CI and laptops agree).

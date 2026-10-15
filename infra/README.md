# Homend Residence infrastructure (Terraform)

Everything the backend needs on AWS (ADR-0001), as Terraform, in `eu-central-1` (Frankfurt) by default. The deploy workflows in `.github/workflows/backend-deploy*.yml` ship new images onto this; Terraform does not deploy code.

```
infra/
  bootstrap/            S3 bucket for Terraform state (run once per account, local state)
  global/               GitHub OIDC provider, ECR repository, role the build job pushes with
  modules/environment/  one environment: VPC, ALB + WAF, ECS Fargate, RDS PostgreSQL 16,
                        console bucket + CloudFront,
                        S3 document vault, KMS key, Secrets Manager, IAM, deploy role
  envs/staging/         staging settings (small, fake M-Pesa, no deletion protection)
  envs/production/      production settings (Multi-AZ, deletion protection, HTTPS required)
```

## What each environment gets

| Piece | Staging | Production |
|---|---|---|
| Network | VPC `10.20.0.0/16`, 2 AZs, public + private subnets, 1 NAT gateway | VPC `10.30.0.0/16`, same layout |
| Existing network | Optional: set `existing_vpc_id`, `existing_public_subnet_ids` and `existing_private_subnet_ids` (two AZs each) to use a VPC you already have; no VPC, subnets or NAT gateway are created. The private subnets need outbound HTTPS (NAT or VPC endpoints). | same |
| Load balancer | ALB, HTTP until a certificate is given; WAF rate limit per IP | ALB, HTTPS (TLS 1.2+), HTTP redirects; WAF rate limit per IP |
| Staff console | Private S3 bucket behind CloudFront on the `cloudfront.net` domain | Same, on `console_domain` with a us-east-1 certificate |
| API | ECS Fargate service `homend-api`, 1 task (scales to 2 on CPU) | 2 tasks (scales to 6 on CPU) |
| Database | RDS PostgreSQL 16, `db.t4g.micro`, single AZ, 7-day backups | `db.t4g.small`, Multi-AZ, 30-day backups, deletion protection, final snapshot |
| Document vault | S3, SSE-KMS, versioned, TLS only, CORS so the portals can upload ID documents and photos from the browser (`public_base_url` plus `allowed_origins`) | same |
| Secrets | `homend/staging/app` (generated), `homend/staging/mpesa` and `homend/staging/push` (you fill in) | `homend/production/app`, `homend/production/mpesa`, `homend/production/push` |
| Logs | CloudWatch, 30 days | CloudWatch, 365 days |

The tasks, the migration task and the database run in private subnets. Only the load balancer is public.

## First-time setup

You need AWS credentials for an admin in the target account, Terraform 1.10 or newer, and, for production, a domain and an ACM certificate in the same region. To use another region, pass `-var region=...` and the matching `-backend-config="region=..."`.

1. **State bucket** (once per account):
   ```bash
   cd infra/bootstrap && terraform init && terraform apply
   ```
   Note the `state_bucket` output. Keep `bootstrap/terraform.tfstate` somewhere safe or import the bucket later; it only describes this one bucket.

2. **Shared resources:**
   ```bash
   cd infra/global
   terraform init -backend-config="bucket=<state_bucket>" -backend-config="region=eu-central-1"
   terraform apply
   ```
   Copy the `github_repository_variables` output into GitHub as repository variables (Settings → Secrets and variables → Actions → Variables).

3. **Staging:**
   ```bash
   cd infra/envs/staging
   terraform init -backend-config="bucket=<state_bucket>" -backend-config="region=eu-central-1"
   terraform apply
   ```
   Create a GitHub environment named `staging` and copy the `github_environment_variables` output into it.

4. **Production:** copy `envs/production/terraform.tfvars.example` to `terraform.tfvars`, fill it in, then init and apply as above. Create the `production` GitHub environment with the output variables, add **required reviewers** and limit deployment branches to `main`. Point your API DNS name at `load_balancer_dns_name` and `console_domain` at `console_cloudfront_domain`. The console certificate must be issued in **us-east-1** (a CloudFront rule); the API certificate stays in the main region.

5. **M-Pesa keys:** put the Daraja values into the `homend/<env>/mpesa` secret in Secrets Manager (keys `MPESA_CONSUMER_KEY`, `MPESA_CONSUMER_SECRET`, `MPESA_PASSKEY`, `MPESA_INITIATOR`, `MPESA_SECURITY_CREDENTIAL`). Terraform creates them empty and never overwrites them. Production will not start without them; staging uses the fake client until you set `mpesa_mode = "sandbox"`.

   **Web push keys:** run `npx web-push generate-vapid-keys` and put the two values into `homend/<env>/push` as `VAPID_PUBLIC_KEY` and `VAPID_PRIVATE_KEY` (Terraform cannot generate the raw P-256 keys web push uses). Set `vapid_subject` to a `mailto:` contact. Use a separate pair per environment and never change a pair once clients have subscribed, or their subscriptions stop working.

6. **First deploy:** run **Backend deploy** from the Actions tab on `main` (or merge to `main`). Until then the service points at a placeholder image tag and its tasks fail to start, which is expected.

After the first deploy, create the first staff user by running the task definition once with the command `create-staff -email … -name … -roles admin` (ECS console → Run task, or `aws ecs run-task` with a command override) and read the password and TOTP secret from the task's log.

## Who changes what

- **Terraform** owns configuration: environment variables, secrets, CPU and memory, roles, networking. Changing them registers a new task definition revision; it goes live with the next deploy, which copies the latest revision and swaps in the new image.
- **The deploy workflow** owns the image and which revision the service runs. Terraform ignores the service's `task_definition` and `desired_count` (autoscaling owns the count).

## Secrets and state

Terraform generates the database password, `FIELD_ENCRYPTION_KEY` and `WEBHOOK_TOKEN`, so they are in the state file. The state bucket is private, versioned and encrypted; limit who can read it. `FIELD_ENCRYPTION_KEY` encrypts the `*_enc` columns: never replace the `random_bytes.field_key` resource, or that data becomes unreadable. Moving to a KMS-wrapped data key (ADR-0002 known gaps) removes it from state.

## Still to decide or add

- **SMS sender:** `sms.New` only has the log sender today. Production refuses `sms_provider = "log"`, and any other value starts the service but every send fails (it no longer falls back to logging codes), so clients and providers cannot sign in until the Africa's Talking sender is wired (ADR-0002 known gaps).
- **First staff user:** `create-staff` prints the password and TOTP secret to the task log, so they sit in CloudWatch. After that admin signs in, have a second admin reset their MFA and change the password, or delete those log events.
- **SQS and EventBridge** (ADR-0001) are not created yet because nothing in the code uses them; add them with the outbox relay.
- **CloudFront** in front of the public site and portals (the console already has one), and S3 access logging on the vault and CloudFront.
- **Alarms** (5xx rate, unhealthy tasks, RDS CPU and storage) and where they notify.
- **Separate production account:** this layout runs both environments in one account. To split, apply `global/` in the shared account with `pull_account_ids = ["<prod account>"]`, create the OIDC provider in the production account too, and point the production GitHub environment's `AWS_REGION`/role at that account.
- **Provider lock files:** commit each root's `.terraform.lock.hcl` after the first `terraform init` (run `terraform providers lock -platform=linux_amd64 -platform=darwin_arm64` so CI and laptops agree).

## The console distribution

CloudFront serves the built console from S3 and sends `/api/*` to the load balancer uncached, with every header, cookie and query string except `Host`, so the console and the API share one origin and need no CORS (`console/README.md`, "Deploying").

- **SPA fallback** is a CloudFront Function on the S3 behaviour that serves `/index.html` for any path without a file extension. It is deliberately not done with custom error responses: those apply to the whole distribution and would turn the API's own 403 and 404 answers under `/api/*` into `index.html` with status 200, hiding permission errors from the console.
- **Caching** comes from the object headers the deploy sets: `assets/*` immutable for a year, `index.html` and other root files `no-cache`. Each deploy invalidates `/*`. Old hashed assets are kept so open tabs keep working.
- **Rate limiting:** a CloudFront WAF limits each viewer IP. Requests CloudFront forwards to the load balancer carry a secret `X-Origin-Verify` header and are left out of the load balancer's per-IP rule, which would otherwise count all console users behind a few CloudFront addresses as one.


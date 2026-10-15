# Homend Residence backend

Go modular monolith (ADR-0001): one service, PostgreSQL 16, goose migrations, and a JSON API defined by `api/openapi.yaml`. The OpenAPI file is the source of truth: the server interface and models are generated from it, and the access rules for each route are read from it at startup.

```
backend/
  api/openapi.yaml        contract: 239 operations, 223 schemas (x-roles on every console route)
  migrations/             goose SQL migrations, embedded into the binary (65 tables)
  cmd/api/                the binary: serve | migrate | create-staff
  internal/
    api/                  handlers (+ gen/: generated server interface and models)
    auth/ crypt/          sessions, OTP, argon2id + TOTP; AES-GCM field encryption (KMS seam)
    domain/               business rules with no HTTP or SQL (pricing, assignability, matching)
    config/ db/ httpx/ sms/
  Makefile  Dockerfile  docker-compose.yml  .env.example
../.github/workflows/backend-ci.yml, backend-deploy.yml, backend-deploy-ecs.yml
```

## Run it

```bash
cd backend
make db-up                 # Postgres 16 in Docker (databases: homend, homend_test)
make migrate-up            # goose up, using the migrations embedded in the binary
go run ./cmd/api create-staff -email you@example.com -name "Your Name" -roles admin,triage_officer
make run                   # http://localhost:8080  (spec at /openapi.yaml, health at /api/v1/healthz)
make test-db               # unit + end-to-end tests against Postgres, race detector on
```

`create-staff` prints a password and a TOTP secret once. Staff sign in with `POST /api/v1/auth/staff/login`, then `POST /api/v1/auth/staff/mfa`. Clients and providers sign in by phone and SMS code; with `SMS_PROVIDER=log` (dev only, refused when `APP_ENV=prod`) the code is printed in the server log.

Migrations are plain goose files (`-- +goose Up` / `-- +goose Down`), so the goose CLI works too: `goose -dir migrations postgres "$DATABASE_URL" up`.

After editing `api/openapi.yaml`, run `make generate` and commit `internal/api/gen/api.gen.go`; CI fails if it is stale.

## What is built and what is not

**Built and tested: every operation in the spec (238).** None answers `501`; `TestNoOperationIsUnimplemented` calls each one as its owning role and fails on a 501 or any 5xx. That covers authentication, the document vault, vetting and the provider portal, training, booking and triage (shortlists, assignments, visits, exceptions), job variations, offline sync, payments and M-Pesa, the double-entry ledger, payouts, complaints and discipline (two-officer disqualification), appraisal, messaging with contact masking, notifications and push, reports and CSV export, catalogue and staff administration, and the USSD, SMS and WhatsApp channels.

**Staff creation.** `POST /console/staff` returns `initial_credentials` (password and TOTP secret) once; they are not retrievable later. `reset-mfa` returns a fresh `TotpEnrolment`.

**Channels.** `/webhooks/ussd`, `/webhooks/sms/inbound`, `/webhooks/sms/delivery` and `/webhooks/whatsapp` reuse the web handlers in process, so a job accepted by USSD or SMS follows the same rules as one accepted in the app. They are guarded by `WEBHOOK_TOKEN` (and optionally `WEBHOOK_ALLOWED_CIDRS`).

**Configuration.** See `.env.example`: `MPESA_*` (Daraja), `WEBHOOK_TOKEN`, `STORAGE_BUCKET`/`STORAGE_KMS_KEY_ID`, `PUBLIC_BASE_URL`.

**How it is checked:** `internal/api/e2e_test.go` runs the whole booking journey (register, book, triage, assign, accept, check in with a door code, check out, rate, complaint opened) over HTTP against Postgres and validates every response against `openapi.yaml`, so code and contract cannot drift. `policy_test.go` fails the build if a console route has no `x-roles`.

**Known gaps to close before launch:** rate limiting for staff login and per-IP OTP requests (the per-phone limit exists; put AWS WAF/ALB limits in front); KMS-backed `crypt.Cipher` (an AES-GCM key from `FIELD_ENCRYPTION_KEY` is used today); SQS outbox relay (events are written to `outbox_events` in the same transaction, nothing publishes them yet; SMS to providers and clients is sent directly, best effort); Africa's Talking/WhatsApp senders (`sms.Sender` is the seam); recurring visit expansion; CSRF tokens for the cookie-based HTML portals.

## Deploying to AWS

Two workflows deploy the service to ECS Fargate (ADR-0001). They do not create AWS resources: `infra/` does that with Terraform (see `infra/README.md`), and its outputs give the GitHub variables listed below.

- `.github/workflows/backend-deploy.yml` runs after **Backend CI** passes on a push to `main`. It builds the image once, tagged with the commit SHA, pushes it to ECR and deploys that same image (pinned by digest) to **staging**, then to **production** once a reviewer approves the `production` environment. Run it by hand on `main` to redeploy, or pass `image_tag` (an earlier commit SHA already in ECR) to roll back without rebuilding.
- `.github/workflows/backend-deploy-ecs.yml` is the reusable deploy job. It calls `scripts/deploy-ecs.sh`, which registers a new task definition revision with the new image, runs it once as `api migrate up` (a one-off Fargate task) and stops if the migration fails, then updates the service and waits until the new revision is the stable primary deployment. If the circuit breaker rolls the service back, the job fails. A health check of `$PUBLIC_BASE_URL/api/v1/healthz` follows.
- **Console.** When the commit has `console/`, the build job also runs `npm ci && npm run build` once, and each environment's deploy uploads `dist/` to the console bucket after the API is live: hashed `assets/*` with a one-year immutable cache (never deleted, so open tabs keep working), then `index.html` and the other root files with `no-cache`, then a CloudFront invalidation. The workflow also runs after **Console CI** passes on `main`, so console-only changes deploy too. CloudFront serves the console and sends `/api/*` to the load balancer on the same domain (see `console/README.md`, "Deploying").

Migrations run **before** the new code takes traffic, so the old tasks briefly run against the new schema. Keep migrations backward compatible (add columns and tables first, remove them in a later release).

No AWS keys are stored in GitHub. Each job assumes an IAM role through GitHub OIDC. Application secrets stay in AWS and reach the container through the task definition.

### AWS resources

`infra/` creates all of these. The workflows read the names from GitHub variables.

| Resource | Suggested name | Notes |
|---|---|---|
| IAM OIDC provider | `token.actions.githubusercontent.com` | Audience `sts.amazonaws.com`. |
| ECR repository | `homend-api` | Tag immutability on, scan on push. If production is a separate account, allow it to pull in the repository policy. |
| Push role | `homend-github-ecr-push` | Trust: `repo:KING-CYBERTON/home-residency:ref:refs/heads/main`. Allow `ecr:GetAuthorizationToken` and, on the repository, `ecr:BatchCheckLayerAvailability`, `InitiateLayerUpload`, `UploadLayerPart`, `CompleteLayerUpload`, `PutImage`, `BatchGetImage`, `DescribeImages`. |
| Deploy role, per environment | `homend-github-deploy-staging`, `homend-github-deploy-production` | Trust: `repo:KING-CYBERTON/home-residency:environment:staging` (or `:production`). Allow `ecs:DescribeTaskDefinition`, `RegisterTaskDefinition`, `RunTask`, `DescribeTasks`, `StopTask`, `UpdateService`, `DescribeServices`, and `iam:PassRole` on the task and execution roles below. |
| ECS cluster, per environment | `homend-staging`, `homend-production` | Fargate. |
| Console bucket + CloudFront | `homend-<env>-console-<account id>` | Private bucket behind CloudFront (origin access control). `/api/*` goes to the ALB uncached; 403/404 from S3 return `/index.html`. The deploy role needs `s3:ListBucket`, `PutObject`, `DeleteObject` on it and `cloudfront:CreateInvalidation` on the distribution. |
| ECS service | `homend-api` | Behind an ALB (target group health check `/api/v1/healthz`, port 8080). Turn on the deployment circuit breaker with rollback. |
| Task definition family | `homend-api-staging`, `homend-api-production` | Create the first revision yourself; each deploy copies the latest revision and swaps only the image. Container name `api`, port 8080, logs to CloudWatch. |
| Task execution role | `homend-<env>-api-execution` | Pulls from ECR, writes logs, reads the secrets below. |
| Task role | `homend-<env>-api-task` | S3 document bucket, KMS key (`STORAGE_KMS_KEY_ID`), later SQS. |
| RDS PostgreSQL 16 | `homend-staging`, `homend-production` | Private subnets; security group open to the service and migration tasks. |
| S3 bucket + KMS key | `homend-<env>-vault-<account id>`, `alias/homend-<env>` | SSE-KMS document vault. |
| Secrets Manager | `homend/<env>/app`, `homend/<env>/mpesa` | Values for the container (next section). |

### Container configuration (in the task definition)

Plain environment: `APP_ENV=prod` in production, `HTTP_ADDR=:8080`, `STORAGE_DRIVER=s3`, `STORAGE_BUCKET`, `STORAGE_KMS_KEY_ID`, `PUBLIC_BASE_URL`, `ALLOWED_ORIGINS`, `SMS_PROVIDER`, `MPESA_MODE` (`sandbox` in staging, `production` in production), `MPESA_SHORTCODE`, `MPESA_B2C_SHORTCODE`, `WEBHOOK_ALLOWED_CIDRS`.

From Secrets Manager (task definition `secrets`): `DATABASE_URL`, `FIELD_ENCRYPTION_KEY`, `WEBHOOK_TOKEN`, `MPESA_CONSUMER_KEY`, `MPESA_CONSUMER_SECRET`, `MPESA_PASSKEY`, `MPESA_INITIATOR`, `MPESA_SECURITY_CREDENTIAL`.

`api migrate up` loads the same configuration as `serve`, so the migration task needs these too; it gets them because it runs the same task definition.

### GitHub settings

**Repository variables** (Settings → Secrets and variables → Actions → Variables):

| Variable | Example |
|---|---|
| `AWS_REGION` | `eu-central-1` |
| `AWS_ECR_PUSH_ROLE_ARN` | `arn:aws:iam::111111111111:role/homend-github-ecr-push` |
| `ECR_REPOSITORY` | `homend-api` |

**Environments** `staging` and `production` (Settings → Environments), each with these variables:

| Variable | Example (staging) |
|---|---|
| `AWS_DEPLOY_ROLE_ARN` | `arn:aws:iam::111111111111:role/homend-github-deploy-staging` |
| `ECS_CLUSTER` | `homend-staging` |
| `ECS_SERVICE` | `homend-api` |
| `ECS_TASK_FAMILY` | `homend-api-staging` |
| `ECS_CONTAINER_NAME` | `api` (the default if unset) |
| `ECS_SUBNETS` | `subnet-aaa,subnet-bbb` (private subnets for the migration task) |
| `ECS_SECURITY_GROUPS` | `sg-ccc` (must reach RDS) |
| `PUBLIC_BASE_URL` | `https://api.staging.example.co.ke` (optional; enables the health check) |
| `CONSOLE_BUCKET` | `homend-staging-console-111111111111` (optional; without it the console step is skipped) |
| `CONSOLE_DISTRIBUTION_ID` | `E2ABCDEF123456` (CloudFront distribution to invalidate) |
| `AWS_REGION` | only if this environment is in a different region |

On `production`, add **required reviewers** (this is the manual approval) and limit **deployment branches** to `main`. No GitHub secrets are needed.

## Defaults chosen where the report leaves decisions open

| Open question (report §11) | Default in this design | What changes if decided otherwise |
|---|---|---|
| Legal model (employer vs contractor) | Stored per provider as `engagement_basis` (`undecided` until set). The rate card holds `provider_pay_minor` so a wage floor can be enforced. | Nothing in the schema. The employee model would add a payroll module later (PAYE/NSSF/SHIF tables). |
| Money handling in v1 | The platform collects via M-Pesa STK push into a segregated client-funds account, pays providers by B2C on a weekly cycle and keeps commission. A double-entry ledger keeps the funds visibly separate. | If the platform stops touching money, drop the payouts and ledger and keep invoices only. |
| Revenue model | All three are supported: per-job commission (rate card), placement fee (rate card column), and subscriptions (plans table). Pricing is data only. | Just turn plans or fees on or off. |
| Pilot service lines and catchment | Every category, service and area has an `is_active` flag. Nothing is hard-coded. | Set the flags. |
| Caregiving at launch | Categories carry `vetting_tier` (`standard`, `trade` or `enhanced`) and checklists differ per category. | Activate the category when ready. |
| Contact masking | Client and provider never see each other's contacts in any API schema. Messaging is mediated by management. Number masking via a calling proxy is not modelled. | Add a proxy-number table if chosen. |
| Auth | Clients and providers use phone + SMS OTP, with no passwords. Staff use email + password + TOTP. Opaque session tokens work as a cookie for HTMX pages and as a Bearer token for the console and Flutter. | None. |
| USSD/SMS gateway | Africa's Talking-style callbacks. | Change the webhook payload shapes only. |

## Database: tables by module

| Migration | Module | Tables |
|---|---|---|
| 00001 foundation | shared | extensions (pgcrypto, citext, btree_gist), `set_updated_at()`, reference sequences, role enums |
| 00002 identity | identity | `users`, `staff_roles`, `sessions`, `otp_codes`, `clients`, `service_areas`, `addresses`, `consents` |
| 00003 catalogue | catalogue and rate card | `service_categories`, `services`, `rate_card_entries` (versioned, no overlapping windows), `checklist_templates`, `checklist_template_items`, `training_courses` |
| 00004 providers_vetting | vetting and documents | `providers`, `provider_categories`, `provider_service_areas`, `documents` (vault metadata), `applications`, `application_items`, `referees`, `clearance_checks`, `assessments`, `training_enrolments`, `provider_contracts`, `provider_availability`, `provider_time_off` |
| 00005 requests_bookings | bookings and triage | `service_requests`, `shortlist_entries`, `assignments`, `visits`, `visit_events` (offline sync), `variations`, `ratings`, `job_exceptions` |
| 00006 payments | payments | `invoices`, `invoice_lines`, `payments`, `mpesa_callbacks`, `refunds`, `provider_earnings`, `payout_batches`, `payouts`, `ledger_entries`, `subscription_plans`, `subscriptions` |
| 00007 complaints_appraisal | complaints and appraisal | `complaints`, `complaint_events`, `disciplinary_actions`, `spot_checks`, `appraisal_snapshots` |
| 00008 platform | notifications, content, audit | `notification_templates`, `notifications`, `web_push_subscriptions`, `conversations`, `messages`, `content_pages`, `faqs`, `leads`, `app_settings` (seeded), `audit_log`, `document_access_log`, `outbox_events`, `idempotency_keys`, `ussd_sessions` |

### Report controls enforced in the database
These hold even if handler code has a bug:
- The vetter cannot approve the same application (`applications_vetter_not_approver`), and a decision needs a reason.
- One open application per provider per category.
- A provider must be heard before a complaint is decided. The investigator cannot also decide the outcome.
- Disqualification needs a second, different officer (`disqualification_two_officers`).
- The payout batch preparer and approver must differ. So must the refund requester and approver.
- Only one live offer per request. No overlapping rate-card validity windows. No overlapping time-off.
- `audit_log`, `document_access_log` and `ledger_entries` are append-only (enforced by triggers).
- Earnings must satisfy `net = gross − commission − withholding`, and invoices must satisfy `total = subtotal + VAT`.

### Rules enforced in Go domain code (documented in the API)
- Only active, trained, contracted providers with documents in date can be assigned.
- Safeguarding and loss complaints suspend the provider straight away.
- A rating at or below the threshold opens a complaint.
- A suspended provider cannot be reinstated before retraining is passed.
- Mandatory document expiry suspends the provider. This runs as a nightly EventBridge job over `documents_expiry_idx`.

### Conventions
- UUID keys and `TIMESTAMPTZ` everywhere. Money is `BIGINT` KES cents (`*_minor`).
- Sensitive fields (`*_enc`: ID numbers, gate access details, referee statements, complaint details, household notes, TOTP secrets) are encrypted in the Go service with KMS envelope encryption. Postgres only stores ciphertext. Files sit in S3 with SSE-KMS and are read through 5-minute presigned URLs, and every read is logged.
- Status enums are used instead of deletes, so history and the retention schedule (`documents.retention_until`) stay intact.
- Domain events go to `outbox_events` in the same transaction and are relayed to SQS (notifications, payouts, M-Pesa callback processing, appraisal recompute).
- Tunable rules live in `app_settings`: the rating threshold, offer expiry, geo check-in radius, appraisal weights, payout cycle and expiry warning days.

## API: endpoint groups

Base path `/api/v1`. Full detail, including request and response schemas, roles (`x-roles`) and error codes, is in `api/openapi.yaml`. It opens in Swagger UI, Redoc or Stoplight.

| Group | Ops | Covers |
|---|---|---|
| Auth | 8 | OTP request/verify (register + login), staff login + TOTP, logout, `/me`, consents |
| Public | 8 | Service areas, categories, rate card, subscription plans, pages, FAQs, leads, all unauthenticated for the SEO site |
| Documents | 4 | Presigned upload, confirm upload, metadata, logged download link |
| Notifications / Messaging | 9 | In-app inbox, web push registration, management-mediated conversations |
| Client | 28 | Addresses, ID verification, quote, requests, shortlist choice, check-in code, visit confirmation, mandatory rating, variations, invoices, M-Pesa pay, subscription, complaints |
| Provider | 36 | Profile, checklist, applications and referees, documents, training, contract signing by OTP, availability and time off, offers accept/decline, jobs, check-in/out, offline sync batch, variations, earnings, payouts, appraisal, complaint responses |
| Console: Vetting / Compliance | 16 | Application queue, claim, return-incomplete, item review, referee record, clearance checks, assessments, complete vetting, approve/defer/reject, document verify/reject, expiring documents, re-verification, vault access log |
| Console: Providers / Training | 14 | Provider search and file, activate, suspend, reinstate, timeline, appraisal, courses, enrolments and results |
| Console: Triage | 17 | Request queue, claim, generate shortlist, share shortlist with client, assign + reason, withdraw/replace, schedule visits, manual check-in, unfilled, cancel, variations, daily exception log |
| Console: Clients | 5 | Client search and file, ID decision, suspend/reinstate |
| Console: Complaints / Quality | 17 | Case queue, open, assign investigator, notes/evidence, provider account, investigation report, decision on the disciplinary ladder, communicate outcome, close, two-officer disqualification, spot checks |
| Console: Finance | 20 | Invoices, payments, manual payment, M-Pesa requery, refunds with approval, earnings hold/release, payout batches with approval, retry, ledger balances, reconciliation |
| Console: Catalogue / Content | 33 | CRUD for areas, categories, services, plans, pages, FAQs, notification templates, plus rate card publishing and checklist versioning |
| Console: Admin / Reports / Dashboard | 12 | Staff and roles, MFA reset, settings, audit log, leads, the six §10 metric reports plus CSV export, queue dashboard |
| Webhooks | 11 | Daraja STK, C2B, B2C and reversal callbacks, USSD steps, inbound SMS and delivery reports, WhatsApp |

### Main flows mapped to endpoints
- **Provider onboarding (§5.1):** `POST /provider/applications` → `POST /documents/uploads` + `/complete` → `PATCH /provider/applications/{id}` → `/referees` → `/submit` → console `/claim` → item reviews, `/referees/{id}/record`, `/clearance-checks`, `/assessments` → `/complete-vetting` → `/decision` (a different officer) → `/console/trainings` → `POST /provider/contract/sign` → `/console/providers/{id}/activate`.
- **Client request (§5.2):** `POST /client/quotes` → `POST /client/requests` → console `/claim` → `/shortlist` → (optional `/share-shortlist` → client `/select`) → `/assignments` → provider `/offers/{id}/accept` (web, USSD or SMS) → `/visits/{id}/check-in` (or `/provider/sync` offline) → `/check-out` → client `/confirm` + `/rating` → invoice paid via `/client/invoices/{id}/pay` → earnings → weekly `/console/payout-batches`.
- **Complaint (§5.4):** `POST /client/complaints` (or a low rating auto-opens one) → `/assign` → `/request-provider-account` → provider `/complaints/{id}/account` → `/report` → `/decision` → `/disciplinary-actions/{id}/authorise` for disqualification → `/communicate` → `/close`.

## Not covered yet
- Go code, sqlc queries and seed data for the real catalogue, checklists and rate card. These wait for a repository and the Phase 1 business design.
- Payroll tables, needed only if the employee model is chosen.
- Card acquiring (the `card` method is in the enum, but no gateway is wired).
- Data-subject request tooling under the DPA (export/erase). Retention dates are already on documents.

## Changes since the first design

- Migrations were converted to goose single-file format (`0000N_name.sql`), plus `00009` (VAT, session and OTP settings) and `00010` (door-code attempt counter).
- Every operation now also declares a `default` error response, and nullable object references use `allOf` + `nullable` (OpenAPI 3.0 ignores siblings of `$ref`). Both were found by the contract test.

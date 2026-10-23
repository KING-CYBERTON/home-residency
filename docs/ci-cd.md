# CI/CD

One workflow, `.github/workflows/ci.yml`, runs for every pull request and every push to `main`. It checks only the parts that changed, reports a single **CI result** check, and on `main` releases what changed once everything passed.

```
pull request / push to main
        │
   Changed parts ──┬── Backend ──────── build, vet, race tests on Postgres, generated code, CSS, OpenAPI lint,
                   │                     migration round trip, govulncheck, image build
                   ├── Console ──────── generated API types, lint, typecheck, build
                   ├── Mobile ───────── format, analyze, tests (incl. OpenAPI contract), debug APKs, signed bundles
                   ├── Infra ────────── terraform fmt and validate, Trivy misconfiguration scan
                   ├── End-to-end ───── 54 browser tests of every feature on a fresh stack
                   ├── Security ─────── gitleaks (full history), CodeQL (Go, TypeScript, workflows),
                   │                     Trivy dependencies and image          [always runs]
                   └── Lint workflows ─ actionlint + shellcheck                [always runs]
                                │
                           CI result  ◄── the one required check
                                │  (main only)
               ┌────────────────┴─────────────────┐
             Deploy                          Mobile release
   build image → Trivy scan → push to ECR     staging APKs (artifacts)
   → provenance + SBOM, build console         signed bundles → Play internal track
   → staging: migrate, roll out, version            │
     check, smoke test, console, smoke test   Mobile promote (by hand, approval)
   → production (approval): same steps        internal → production, staged rollout
```

## What runs when

| Change | Checks | Released on merge |
| --- | --- | --- |
| `backend/` | Backend, End-to-end, Security | API, website and portals to staging, then production |
| `console/` | Console, End-to-end, Security | Console (with the same API image) |
| `backend/api/openapi.yaml` | Backend, Console, Mobile, End-to-end | API and console |
| `mobile/` | Mobile, Security | Staging APKs and a Play internal-track upload |
| `infra/` | Infra, Security | Nothing: Terraform is applied by hand (`infra/README.md`) |
| `e2e/` | End-to-end, Security | Nothing |
| `.github/` | Everything | Everything, so a pipeline change is proven end to end |
| Anything else | Security | Nothing |

A job that was not needed shows as skipped, and **CI result** treats skipped as passed, so a docs-only PR can still merge.

## Deploys (API, website, portals, console)

`backend-deploy.yml` (called by CI, or run by hand):

1. **Build** the image once, tagged with the commit SHA, into the runner's Docker.
2. **Scan** it with Trivy; a fixable HIGH or CRITICAL finding stops the release before anything reaches ECR.
3. **Push** it, record signed **build provenance** for the digest (`gh attestation verify oci://<image> -R KING-CYBERTON/home-residency`), and keep its CycloneDX **SBOM** as a run artifact for 90 days.
4. **Build the console** once.
5. **Staging**, then **production** after a reviewer approves the `production` environment. Each runs `backend-deploy-ecs.yml`:
    1. migrations as a one-off task, then the rollout (`backend/scripts/deploy-ecs.sh`; the ECS circuit breaker undoes a rollout whose tasks never get healthy),
    2. `/api/v1/healthz` must report `ok` and the deployed version,
    3. the read-only smoke test in `e2e/deployed` (`@site`): every public page, both portal sign-in screens, headers, no script errors or CSP violations,
    4. if 2 or 3 fails, the service goes back to the task definition it ran before (migrations stay applied, so keep them backward compatible),
    5. the console is published, CloudFront is invalidated, the job waits until the new `index.html` is served, then smoke-tests the console (`@console`).

**Roll back by hand:** Actions → Deploy → Run workflow on `main` with `image_tag` set to an earlier commit SHA. The image is reused from ECR and the console is rebuilt from that commit.

Until `AWS_ECR_PUSH_ROLE_ARN` is set, Deploy is skipped rather than failed. Without `PUBLIC_BASE_URL` on an environment the health and smoke checks are skipped; without `CONSOLE_URL` the console smoke test is skipped.

## Mobile

- **Mobile release** (`mobile-release.yml`), after CI on a merge that changes `mobile/`:
    - when `MOBILE_STAGING_API_BASE_URL` is set, release APKs of both apps against the staging API, kept 30 days as run artifacts for testers (debug-signed: sideload, after removing any Play build);
    - when the signing secrets and Play access are set, signed bundles against `MOBILE_API_BASE_URL` uploaded to the Play **internal testing** track.
- **Mobile promote** (`mobile-promote.yml`, run by hand): after testing an internal build, promotes it to **production** for everyone or as a staged rollout (for example `0.2` for 20%). It runs in the `production` environment, so it needs the same approval as a production deploy.

Neither workflow holds a Google key: each job exchanges its GitHub OIDC token for short-lived credentials of the Play service account through Workload Identity Federation, the way the deploys use OIDC for AWS.

The internal-track build is the binary that ships, so `MOBILE_API_BASE_URL` must always be the production API.

## One-time GitHub setup

1. **Branch protection on `main`:** require a pull request, require the status check **CI result** (only this one; the per-part checks are inside it), and block force pushes and deletions.
2. **Environments** `staging` and `production`: deployment branches limited to `main`; on `production`, add yourself as a required reviewer.
3. Copy `CONSOLE_URL` from each environment's Terraform output (`github_environment_variables`) into that GitHub environment, so the console is smoke-tested.
4. Optional: the repository variable `MOBILE_STAGING_API_BASE_URL` (the staging API, ending in `/api/v1`) for staging APKs.

Everything else (AWS roles, ECR, the Play signing key and keyless Play access through Workload Identity Federation) is in `backend/README.md`, `infra/README.md` and `mobile/README.md`.

# Homend Residence management console

The staff console from ADR-0001: React + TypeScript with [Refine](https://refine.dev), Tailwind CSS and shadcn/ui, talking to the Go JSON API with TanStack Query. It builds to static files for S3 + CloudFront.

## Run it

```bash
# backend first (see ../backend/README.md): Postgres, migrations, a staff user, then `make run`
cd console
npm install
npm run dev            # http://localhost:5173, /api is proxied to http://localhost:8080
```

Sign in with the email, password and authenticator code printed by `go run ./cmd/api create-staff`. Point the proxy elsewhere with `API_PROXY_TARGET=https://… npm run dev`.

| Script | What it does |
|---|---|
| `npm run generate` | Regenerates `src/api/` from `../backend/api/openapi.yaml`. Run after any contract change and commit the result; CI fails if it is stale. |
| `npm run lint` | ESLint |
| `npm run build` | Typecheck, then build to `dist/` |

## How it fits together

- **The contract drives everything.** `scripts/gen-api.mjs` writes three files from `openapi.yaml`:
  - `schema.d.ts`: request and response types (openapi-typescript). Every API call is typed through `openapi-fetch` / `openapi-react-query` (`$api.useQuery("get", "/console/requests", …)`), so a renamed field breaks the build.
  - `roles.gen.ts`: the `x-roles` of every console operation. `canCall()` in `src/lib/access.ts` applies the same rule as `backend/internal/api/policy.go`, so staff only see pages and buttons their roles can use. The server still enforces it; this is only about not showing dead ends.
  - `enums.gen.ts`: every string enum, for filters and selects.
- **Refine** provides sign-in state (`providers/auth.ts`), access control (`providers/access.ts`), routing, notifications, and the data provider (`providers/data.ts`) used by the catalogue and content CRUD pages. Workflow screens (triage, vetting, complaints, finance) call operations directly with the typed hooks.
- **One query cache** is shared by Refine and the typed hooks. Every successful write invalidates it, so queues refresh after an action. Queues that other staff work at the same time (requests, exceptions, complaints) also poll.
- **Actions** are `FormDialog`s (`components/form-dialog.tsx`): a button, a short form, one API call. Money is entered in KES and sent as cents.
- **Sign-in** is email + password, then TOTP. The bearer token is kept in `sessionStorage`, so it is per tab and gone when the tab closes.

## Screens

| Area | Pages |
|---|---|
| Dashboard | Queue counts for the roles you hold |
| Triage | Live request queue; request file with claim, ranked shortlist, side-by-side compare, offer to client, assign with reason, withdraw/replace, visits (schedule, reschedule, missed, manual check-in), unfilled, cancel; variations; exception log; leads |
| Vetting | Application queue; application file with claim, checklist review, document verify/reject (logged views), referee calls, clearance checks, assessments, return incomplete, complete vetting, approve/defer/reject; expiring documents with re-verification; document access log |
| Providers | Search; provider file with activate, settings, suspend/reinstate, disciplinary actions (two-officer disqualification), training enrolment, re-verification, documents, applications, appraisal and timeline; training enrolments and results; courses |
| Clients | Search; client file with ID decision and suspend/reinstate |
| Quality | Complaint queue and case file (assign, notes and evidence, provider account, report, decision, communicate, close); spot checks |
| Finance | Invoices (void, manual payment), payments (M-Pesa re-query), refunds (request, approve by a second officer, reject), earnings (hold/release), payout batches (prepare, approve by a second officer, retry), ledger balances and reconciliation |
| Catalogue and content | Service areas, categories, services, rate card publishing, vetting checklist versions, subscription plans, website pages, FAQs, message templates |
| Admin | Reports with CSV export, staff and roles (one-time credentials, MFA reset), settings, audit log |

Segregation-of-duties rules (vetter ≠ approver, investigator ≠ decider, preparer ≠ approver, two officers for disqualification) are enforced by the database and API; the console explains them and disables the button for the officer who cannot act.

## Deploying

`npm run build` produces `dist/`: `index.html` plus hashed files in `assets/`. The console expects to be served **from the same origin as the API**, with CloudFront sending `/api/*` to the Go service and everything else to the S3 bucket. That avoids CORS and keeps the API's `hr_session` cookie same-site. Set `VITE_API_BASE_URL` at build time only if the API lives elsewhere (the backend would then need CORS).

CloudFront needs, for the console distribution:
- an `/api/*` behaviour to the ALB, with caching disabled and all headers, query strings and cookies forwarded;
- SPA fallback: custom error responses mapping 403 and 404 from S3 to `/index.html` with status 200;
- `Cache-Control: public, max-age=31536000, immutable` on `assets/*`, and `no-cache` on `index.html`.

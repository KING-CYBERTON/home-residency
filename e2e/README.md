# End-to-end tests

Playwright drives the whole platform the way people use it: the public site and recruitment pages,
the client portal (`/account`), the provider portal (`/provider`) and the staff console. Nothing is
mocked inside the platform. SMS codes are read from the API log (`SMS_PROVIDER=log`) and the tests
play Safaricom by posting the M-Pesa callbacks Daraja would send to the in-memory M-Pesa fake.

| Spec | What it proves |
| --- | --- |
| `journey.spec.ts` | One provider and one household from start to finish: catalogue set-up, recruitment by SMS code, document upload, vetting by one officer and approval by another, training, contract signed by SMS code, activation, booking from a Book link, triage and shortlist, offer, reschedule, door-code check-in, offline check-out, rating, M-Pesa payment (a wrong-amount callback is refused), reconciliation, a refund needing a second officer, and a payout batch prepared and approved by different officers. Each step is its own test, so a failure names the step. |
| `smoke.spec.ts` | Every screen of the console, public site and both portals opens without a script error, a server error, a refused API read or an error banner; a screen outside someone's roles says so; sitemap, robots, manifests, service workers and offline pages are served. |
| `public.spec.ts` | Contact form validation and the enquiry reaching the console's Leads; FAQs and plans edited in the console show on the public site. |
| `triage.spec.ts` | Shortlist shared with the household and their choice, a provider declining, cancellation, variations, manual check-in by a supervisor. |
| `complaints.spec.ts` | A household complaint, investigation, the provider's account in their portal, a decision by a different officer, closing. |
| `admin.spec.ts` | Real console sign-in with password and authenticator code, staff accounts, settings, the audit log and reports. |

Every spec fails if any page threw a script error or the server answered 5xx while it ran.

## Run it locally

You need Go, Node 22 and Postgres 16 on `localhost:5432` (user `postgres`, password `postgres`;
`make db-up` in `backend/` starts one in Docker). Then:

```sh
cd e2e
npm ci
npx playwright install chromium
bash scripts/stack.sh          # fresh database "homend_e2e", API on :8080, console on :4173
npx playwright test            # or: npx playwright test tests/journey.spec.ts --headed
npx playwright show-report
bash scripts/stack.sh stop
```

`E2E_PG_URL` points the stack at another Postgres server. Staff credentials for the run are written
to `.stack/staff.json`, and the API log is `.stack/api.log`.

## In CI

`.github/workflows/e2e.yml` runs the suite on every pull request that touches the backend, console
or tests, on every push to `main`, every morning at 06:00 Nairobi time, and on demand from the
Actions tab (optionally only tests whose title matches a word). The HTML report, failure traces
and server logs are kept as the `e2e-report` artifact for 14 days.

## Writing a test

- Use `lib/world.ts` to get to a starting point quickly (an active provider, a household with an
  address, an accepted job) and spend the browser time on the feature under test.
- Console forms are dialogs; `act(page, "Button", { field: value })` opens one, fills fields by
  name and submits. `actRefused` expects the server to say no.
- Staff log in once per run (the API refuses a reused authenticator code); use `staffToken(name)`
  and `consolePage(browser, name, problems)`.
- SMS codes are limited to one a minute per phone; use a new phone from `newPhone()` per person, or
  `waitForNextCode` when the same person needs a second code.

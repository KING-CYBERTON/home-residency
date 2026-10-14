# Provider portal and recruitment

Server-rendered pages for providers (ADR-0001), built on the shared foundation in `internal/web`.
Mounted in `cmd/api/web.go` at `/provider` (portal) and `/join` (recruitment).

Every action calls the same handler as the JSON route, in-process, through `api.Invoke`. No business
rule lives in this package: a check-in from this page, from `/api/v1/provider/sync`, from USSD or SMS
goes through `PostProviderVisitsCheckIn` either way.

| Flow (report) | Pages |
|---|---|
| Recruitment, §5.1 step 1 | `/join`, `/join/apply` (name, phone, privacy and vetting consents, SMS code) |
| Application and vetting | `/provider/applications`, `…/new`, `…/{id}` (checklist, uploads, referees, submit, withdraw), `/provider/documents` |
| Training and contract | `/provider/training`, `/provider/contract` (signed with an SMS code) |
| Profile and availability | `/provider/profile` (M-Pesa payout number, areas), `/provider/availability` (weekly hours, time off) |
| Job offers, §5.2 | `/provider/offers` (accept or decline; HTMX swaps the card) |
| Jobs, check-in, completion | `/provider/jobs`, `/provider/jobs/{id}` (address and access details, check-in by door code or location, check-out, scope variation) |
| Pay | `/provider/pay` (summary, earnings, M-Pesa payouts) |
| Quality, §5.4 | `/provider/appraisal`, `/provider/complaints` (give my account) |
| Messages | `/provider/inbox` |

## Offline check-in and completion

- `static/sw.js` (served at `/provider/sw.js`, scope `/provider/`) caches the shared CSS, HTMX, the
  portal scripts, an offline page, and every visit of `/provider/`, `/provider/jobs` and
  `/provider/jobs/{id}` (network first, cache when offline).
- `static/portal.js` turns the check-in and check-out forms into events with a `client_event_id` and
  the phone's own time, and stores them in IndexedDB (`static/queue.js`).
- The queue is sent to `POST /provider/sync` on page load, when the phone comes back online, and by
  the service worker's Background Sync where the browser has it. A Web Lock lets only one of them
  send at a time. The server applies each event once and keeps the phone's time, so a check-in made
  without signal counts at the time it happened.
- The page shows what is waiting ("saved on this phone, waiting to send") and any event the server
  refused (for example a wrong door code), with the server's message.
- Signing out sends `Clear-Site-Data: "cache"`, so cached job pages (addresses, access details) leave
  the phone with the session. Queued events stay until they are sent.
- Without JavaScript every form still posts normally (door-code check-in only; location needs JS).

## Page-weight budget

Enforced by `portal_test.go` (gzip sizes, as sent): each page's HTML ≤ 10 KB, portal scripts ≤ 6 KB,
and a first visit (HTML + shared CSS + HTMX + portal scripts) ≤ 60 KB. Today the shared CSS and HTMX
are about 20 KB and the portal scripts about 4 KB. No web fonts and no images beyond the icon.

## Tests

`go test ./internal/web/provider` with `TEST_DATABASE_URL` set runs the recruitment and application
journey (sign-up, upload to the vault, referee, submit) and the job journey (sign in, HTMX accept,
offline batch with replay, wrong door code, decline, availability, time off, profile, every page),
then checks the budget.

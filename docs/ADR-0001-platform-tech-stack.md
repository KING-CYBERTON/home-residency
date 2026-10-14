# ADR-0001: Platform tech stack — Go on AWS, server-rendered web first, React console

- **Status:** Accepted
- **Date:** 2026-10-13
- **Deciders:** Homend Residence promoters; product owner (names to be added)

## Context

Homend Residence is building a home management platform for Kenyan households: a **managed placement and service delivery business**, not a listings app. The platform vets, approves, matches, supervises and appraises domestic workers and home service providers. All contact runs through management (see the Format Partners Advisory report FPA/HR/ADV/2026-01).

The stack has to serve five surfaces:

1. **Public website** — marketing and service pages that must rank well on Google, including local pages (e.g. "vetted cleaners in Kilimani").
2. **Client portal** — registration, booking, payment, ratings, complaints. Mostly used on phones.
3. **Provider portal and recruitment** — online application, document upload, job offers, accept, check-in, completion.
4. **Management console** — vetting, triage and assignment, complaints and disqualification, reporting. This is the operational core of the business.
5. **Later: native mobile app**, once there is proven demand.

Constraints and facts that shaped the decision:

- **Maintainability first.** The team is small and staff will change over time. We prefer fewer moving parts to "full infra".
- **A native app is a hard sell at launch.** Households and providers are unlikely to install an app from an unknown brand, so v1 must work in the browser with nothing to install.
- **Device reality.** Many providers use low-end Android phones with limited storage and metered data. Pages must be light, and check-in/completion must tolerate patchy connectivity. SMS/USSD and WhatsApp fallbacks are needed regardless.
- **Sensitive data.** The platform stores ID copies, Certificates of Good Conduct, referee statements, home addresses, gate/access details and (for caregiving) health data. That is sensitive personal data under the Data Protection Act, 2019, which requires encryption, access control, audit logging and a documented hosting location.
- **Ownership.** Code, repositories, domains and cloud accounts must belong to the company from day one (report §6.2, risk T2).
- **Timeline.** About 12 months from feasibility to launch, with a 12-week MVP build window (report §5.3, §9).
- **Payments.** M-Pesa via Safaricom Daraja for collection and payouts.

## Options considered

### Option 1: Go backend on AWS + Flutter mobile apps from day one
- **Pros:** One codebase for Android and iOS. Native feel, push notifications and offline storage. The original plan, and recommended in the report.
- **Cons:** Requires an app install, which is the hardest sell for a new brand. App size is heavy for low-end provider phones. App-store review slows releases. Still needs a separate public website and admin console. More to build before the pilot.

### Option 2: Go API on AWS + React single-page apps for every web surface
- **Pros:** Largest developer pool in Nairobi. Rich interactivity everywhere. One front-end technology to learn.
- **Cons:** A JavaScript-rendered public site is weaker for SEO unless a server-rendering framework such as Next.js is added, which brings a second server runtime. JavaScript bundles are heavier on low-end provider phones. Every simple form needs both an API and client-side code.

### Option 3: Go server-rendered pages (Go templates + HTMX + Tailwind) for the public site and both portals, React for the management console only, on a lean AWS setup — Flutter later on the same API
- **Pros:** Full HTML in the first response is good for SEO and fast on cheap phones. The public site, client portal and provider portal share one stack, layout and styling. Public pages can use live data (rate card, service areas). React is used only where rich interaction is needed. The API-first backend lets Flutter be added later without rework.
- **Cons:** Two front-end approaches to maintain. Fewer developers know HTMX than React. Text changes on the public site need a developer unless a content tool is added. The backend serves two response types (HTML fragments and JSON).

## Decision

We adopt **Option 3**.

**Backend — Go**
- One Go service (a modular monolith), split internally into modules: identity, vetting and documents, catalogue and rate card, bookings and triage, payments, complaints and appraisal, notifications, content.
- Business rules live in the shared domain modules only. Two thin delivery layers sit on top:
  - **HTML handlers** (Go templates + HTMX fragments) for the public site and portals.
  - **JSON REST API**, documented with **OpenAPI**, for the management console and the future Flutter app.
- Example: "only vetted, trained providers can be assigned" is written once in the domain layer, never in the handlers.

**Public website, client portal and provider portal — Go server-rendered**
- Go `html/template` + **HTMX** + **Tailwind CSS**, mobile-first.
- Public pages: clean URLs (`/services/<service>/<area>`), unique titles and descriptions, sitemap, structured data for local services, cached at **CloudFront**.
- Client and provider portals are installable as a **PWA** (manifest and service worker). The provider portal queues check-in/completion offline and syncs when back online.
- **SMS/USSD and WhatsApp** fallbacks for job offers, acceptance, check-in and booking confirmations (e.g. via Africa's Talking).
- Content editing: a simple "pages and blog" section in the console, or Markdown files read by the Go server. The owner is to be decided.

**Management console — React**
- **React + TypeScript**, using **Refine** for admin scaffolding (tables, filters, forms, role-based screens), Tailwind + shadcn/ui, and TanStack Query against the Go JSON API.
- Built as static files and served from **S3 + CloudFront**.
- Reason: the triage console needs a live queue, ranked shortlists, side-by-side comparison, filtering and several panels updating together. That is where React's in-browser state pays off. Staff use it on office computers with good connections.

**Infrastructure — AWS, kept lean**
- **ECS Fargate** to run the Go service container (no Kubernetes).
- **RDS PostgreSQL** as the primary database.
- **S3 with KMS encryption** for the document vault: private buckets, short-lived signed URLs, access logging.
- **SQS** for background jobs (notifications, M-Pesa callbacks, payouts).
- **EventBridge Scheduler** for scheduled jobs (document-expiry checks, re-verification).
- **CloudFront** in front of the public pages and the console.
- **Secrets Manager** for credentials; **CloudWatch** for logs and alarms.
- All infrastructure defined as code (**Terraform or AWS CDK**). All AWS accounts, domains and repositories registered to the company.

**Later — Flutter**
- When demand justifies it, build Flutter client and/or provider apps against the same OpenAPI-documented JSON API. No backend redesign is expected.

**Why this over the alternatives**
- Over Option 1: there is nothing to install at launch, the provider experience is lighter, and the time to pilot is shorter. Flutter is kept as a later step rather than dropped.
- Over Option 2: SEO and low-end-phone performance come without adding a Node/Next.js server. Simple forms stay simple. React's strengths are applied only where they matter (the console).

## Consequences

### Positive
- No app install needed for clients or providers at launch.
- Public pages ship as full HTML: good for Google ranking and fast on mobile.
- Light pages and offline-tolerant check-in suit providers' phones and data costs.
- One language (Go) for all server logic. Deployment is a single binary in a container.
- Business rules are written once and shared by the HTML pages, the console and the future mobile app.
- A rich, productive console for the vetting and triage work that the business depends on.
- Lean AWS footprint: few managed services, no cluster to operate, infrastructure reproducible from code.
- The company owns everything from day one.

### Negative / trade-offs accepted
- Two front-end approaches (HTMX and React) to maintain and hire for.
- Fewer HTMX developers than React developers. We rely on Go developers picking it up, which is usually quick.
- The backend maintains two delivery layers (HTML and JSON). This is extra work, controlled by keeping all logic in shared modules.
- With no CMS by default, public-site copy changes need a developer until a content-editing feature is built.
- PWA limits: on iPhone, push notifications only work after the user adds the site to their home screen. Critical messages go by SMS/WhatsApp instead.
- Fully custom build rather than a configured off-the-shelf product, so a longer and costlier path to pilot (report §6.2, Decision 10).
- AWS has no Kenyan region. The nearest is Cape Town (af-south-1), which is a cross-border transfer of personal data.

### Risks and mitigations
- **Data protection and cross-border hosting** — get a written legal position on hosting in af-south-1 (or elsewhere) under the Data Protection Act, 2019 before go-live, including whether any data must stay in Kenya. Register with the ODPC. Encrypt at rest (KMS) and in transit, use role-based access and access logging on the vault, and adopt a retention schedule.
- **Business logic duplicated across HTML and JSON handlers** — code review rule: handlers only translate requests and responses. Put tests at the domain layer.
- **HTMX hiring pool** — choose a build partner with proven Go experience. Keep templates simple and documented. Write a short internal guide on HTMX conventions.
- **Provider connectivity** — offline queue in the service worker, SMS/USSD fallback, page-weight budget for provider pages, testing on a real low-end Android phone.
- **Console scope creep** — scope the console strictly to the MoSCoW "Must have" list for v1 (report §6.1).
- **Vendor lock-in / ownership** — infrastructure as code, IP assignment and repository access in the development contract, all accounts in the company name.
- **Security breach** — penetration test before launch (report Phase 5), least-privilege IAM, secrets in Secrets Manager, CloudWatch alarms.
- **Cost drift** — AWS budgets and alerts from day one. Review monthly spend against the pilot plan.

## Revisit when
- **Mobile demand is proven** — e.g. a large share of active clients or providers use the PWA weekly and ask for a native app, or push/offline limits on the PWA hurt fill rate. Then start the Flutter app on the existing API.
- **Portal screens become highly interactive** — if client or provider screens need rich in-browser state that HTMX handles awkwardly, consider moving that portal to React.
- **Team composition changes** — if the team becomes mostly React/TypeScript developers and few Go developers, reconsider the HTMX layer.
- **A legal opinion requires in-country hosting** — if personal data must be stored in Kenya, move the vault (or the whole stack) to a local provider or adopt a hybrid setup.
- **Scale** — if one service on Fargate becomes a bottleneck, or separate teams own separate modules, split the busiest modules (e.g. notifications, payments) into their own services.
- **Marketing needs frequent copy changes** — add a headless CMS or a proper content-editing feature.
- **AWS cost** exceeds the budget agreed for the pilot by a meaningful margin.

## References
- Format Partners Advisory LLP, *Homend Residence — Home Management Platform* (FPA/HR/ADV/2026-01, 30 July 2026): §5.3 build process, §6.1 MoSCoW, §6.2 technology decisions, §7.6 data protection, §7.7 technology and build, §8 risks T1–T4 and C3, §11 Decision 10. Saved in project knowledge as `homend-residence-platform-report.md`; source https://formatgroup.co.ke/
- Go `html/template`: https://pkg.go.dev/html/template
- HTMX: https://htmx.org/docs/
- Tailwind CSS: https://tailwindcss.com/docs
- Refine (React admin framework): https://refine.dev/docs/
- AWS ECS on Fargate: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/AWS_Fargate.html
- AWS Africa (Cape Town) region, af-south-1: https://aws.amazon.com/about-aws/global-infrastructure/regions_az/
- Safaricom Daraja API: https://developer.safaricom.co.ke/
- Kenya Data Protection Act, 2019 / ODPC: https://www.odpc.go.ke/

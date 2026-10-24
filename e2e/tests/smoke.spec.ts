import { expect, test, type Page } from "@playwright/test";
import { CONSOLE, SITE } from "../lib/env";
import { call, staffToken } from "../lib/api";
import { one } from "../lib/db";
import { consolePage, portalPage, watch } from "../lib/ui";
import { acceptedJob, activeProvider, client } from "../lib/world";

// Every screen of every surface opens without a script error, a server error or an error banner.
// The deep checks of what each screen does live in the feature specs; this one catches the page
// that nobody clicked on since the last change.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const ids: Record<string, string> = {};

test.beforeAll(async () => {
  const ops = await staffToken("ops");
  const p = await activeProvider("Smoke Provider");
  const c = await client("Smoke Household");
  const job = await acceptedJob(ops, c, p, 60);
  const other = await one<{ id: string }>(`SELECT c.id FROM service_categories c WHERE c.slug <> 'cleaning' AND c.is_active
     AND EXISTS (SELECT 1 FROM checklist_templates t WHERE t.category_id = c.id AND t.is_current) ORDER BY c.sort_order LIMIT 1`);
  const app = await call<{ id: string }>(p.token, "POST", "/provider/applications", { category_id: other.id });
  const complaint = await call<{ id: string }>(c.token, "POST", "/client/complaints", { request_id: job.requestId, severity: "quality", summary: "Smoke test complaint" });
  Object.assign(ids, { request: job.requestId, provider: p.userId, client: c.userId, application: app.id, complaint: complaint.id, clientToken: c.token, providerToken: p.token, assignment: job.assignmentId });
  const doc = await one<{ id: string } | undefined>(`SELECT id FROM documents LIMIT 1`).catch(() => undefined);
  if (doc) ids.document = doc.id;
});

test.afterAll(() => {
  expect(problems, "script errors or server errors on some screen").toEqual([]);
});

async function opensCleanly(page: Page, url: string) {
  const failed: string[] = [];
  const onResponse = (r: import("@playwright/test").Response) => {
    // A read the screen needs that the API refused (not a 404 for a deliberately missing thing).
    if (r.request().method() === "GET" && r.url().includes("/api/v1/") && r.status() >= 400) failed.push(`${r.status()} ${r.url()}`);
  };
  page.on("response", onResponse);
  const res = await page.goto(url);
  await page.waitForLoadState("networkidle");
  page.off("response", onResponse);
  if (res) expect.soft(res.status(), url).toBeLessThan(400);
  expect.soft(failed, `API reads refused on ${url}`).toEqual([]);
}

const consoleRoutes = [
  "/", "/requests", "/variations", "/exceptions", "/leads", "/applications", "/expiring-documents",
  "/providers", "/trainings", "/training-courses", "/clients", "/complaints", "/spot-checks",
  "/invoices", "/payments", "/refunds", "/earnings", "/payout-batches", "/reconciliation",
  "/service-areas", "/categories", "/services", "/rate-card", "/checklists", "/subscription-plans",
  "/content-pages", "/faqs", "/notification-templates", "/reports", "/staff", "/settings", "/audit-log",
];

test("console: every list screen opens for an officer who holds the roles", async ({ browser }) => {
  // ops holds every role but approver and complaints_manager; approver covers the rest below.
  const page = await consolePage(browser, "ops", problems);
  for (const path of consoleRoutes) {
    await test.step(path, async () => {
      await opensCleanly(page, CONSOLE + path);
      await expect.soft(page.getByRole("heading", { level: 1 }).first(), path).toBeVisible();
      await expect.soft(page.getByText("Your roles do not include this area"), path).toHaveCount(0);
      await expect.soft(page.locator("main .text-destructive.border"), path).toHaveCount(0);
    });
  }
});

test("console: every detail screen opens", async ({ browser }) => {
  const page = await consolePage(browser, "ops", problems);
  const approver = await consolePage(browser, "approver", problems);
  const details: [Page, string][] = [
    [page, `/requests/${ids.request}`],
    [page, `/providers/${ids.provider}`],
    [page, `/clients/${ids.client}`],
    [page, `/applications/${ids.application}`],
    [approver, `/complaints/${ids.complaint}`],
  ];
  if (ids.document) details.push([page, `/documents/${ids.document}/access-log`]);
  for (const [p, path] of details) {
    await test.step(path, async () => {
      await opensCleanly(p, CONSOLE + path);
      await expect.soft(p.getByRole("heading", { level: 1 }).first(), path).toBeVisible();
      await expect.soft(p.locator("main .text-destructive.border"), path).toHaveCount(0);
    });
  }
});

test("console: a screen outside someone's roles says so instead of failing", async ({ browser }) => {
  const page = await consolePage(browser, "supervisor2", problems);
  await page.goto(CONSOLE + "/payments");
  await expect(page.getByText("Your roles do not include this area")).toBeVisible();
  await page.goto(CONSOLE + "/staff");
  await expect(page.getByText("Your roles do not include this area")).toBeVisible();
});

test("public site: every page, the sitemap and robots", async ({ browser }) => {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  watch(page, "public", problems);
  const pages = ["/", "/services", "/services/cleaning", "/services/cleaning/kilimani", "/areas", "/areas/kilimani", "/pricing", "/how-it-works", "/privacy", "/faq", "/book", "/contact", "/join", "/join/apply"];
  for (const path of pages) {
    await test.step(path, async () => {
      await opensCleanly(page, SITE + path);
      await expect.soft(page.locator("h1").first(), path).toBeVisible();
    });
  }
  const sitemap = await (await fetch(SITE + "/sitemap.xml")).text();
  expect(sitemap).toContain("<urlset");
  expect(sitemap).toContain("/services/cleaning");
  const robots = await (await fetch(SITE + "/robots.txt")).text();
  expect(robots).toMatch(/Sitemap: .*sitemap\.xml/i);
  for (const missing of ["/services/no-such-thing", "/areas/no-such-place"]) {
    expect((await page.goto(SITE + missing))?.status(), missing).toBe(404);
  }
});

test("client portal: every screen opens for a signed-in household", async ({ browser }) => {
  const page = await portalPage(browser, "client", ids.clientToken, problems);
  for (const path of ["/account", "/account/book", "/account/requests", `/account/requests/${ids.request}`, "/account/invoices", "/account/complaints", "/account/complaints/new", `/account/complaints/${ids.complaint}`, "/account/profile", "/account/addresses", "/account/plans", "/account/notifications", "/account/messages"]) {
    await test.step(path, async () => {
      await opensCleanly(page, SITE + path);
      await expect.soft(page.locator("h1").first(), path).toBeVisible();
    });
  }
});

test("provider portal: every screen opens for a signed-in provider", async ({ browser }) => {
  const page = await portalPage(browser, "provider", ids.providerToken, problems);
  for (const path of ["/provider", "/provider/more", "/provider/applications", `/provider/applications/${ids.application}`, "/provider/applications/new", "/provider/documents", "/provider/training", "/provider/contract", "/provider/profile", "/provider/availability", "/provider/offers", "/provider/jobs", `/provider/jobs/${ids.assignment}`, "/provider/pay", "/provider/appraisal", "/provider/complaints", "/provider/inbox"]) {
    await test.step(path, async () => {
      await opensCleanly(page, SITE + path);
      await expect.soft(page.locator("h1").first(), path).toBeVisible();
    });
  }
});

test("installable apps: manifests, service workers and offline pages", async () => {
  for (const base of ["/account", "/provider"]) {
    const m = await fetch(`${SITE}${base}/manifest.webmanifest`);
    expect(m.status, `${base} manifest`).toBe(200);
    expect((await m.json()).start_url, `${base} start_url`).toContain(base);
    const sw = await fetch(`${SITE}${base}/sw.js`);
    expect(sw.status, `${base} service worker`).toBe(200);
    expect(sw.headers.get("content-type"), `${base} service worker type`).toContain("javascript");
    expect((await fetch(`${SITE}${base}/offline`)).status, `${base} offline page`).toBe(200);
  }
});

test("API: health and the security headers every response carries", async () => {
  const r = await fetch(SITE + "/api/v1/healthz");
  expect(r.status).toBe(200);
  const page = await fetch(SITE + "/");
  expect(page.headers.get("x-content-type-options")).toBe("nosniff");
  expect(page.headers.get("content-security-policy") ?? page.headers.get("x-frame-options")).toBeTruthy();
});

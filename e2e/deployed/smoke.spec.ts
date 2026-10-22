import { expect, test, type Page } from "@playwright/test";

// Read-only checks of a live environment after a deploy: the API is healthy and on the
// expected version, every public page and both portal sign-in screens render with their
// security headers, and the console loads its sign-in screen. Nothing signs in or writes,
// so this is safe to run on production. A script error or a Content-Security-Policy
// violation fails the page, which is how a blank console or a blocked upload would show.

const SITE = process.env.E2E_SITE_URL?.replace(/\/$/, "");
const CONSOLE = process.env.E2E_CONSOLE_URL?.replace(/\/$/, "");
const VERSION = process.env.E2E_EXPECT_VERSION;

/** Collects script errors and CSP violations on a page; the test checks the list at the end. */
function watch(page: Page): string[] {
  const problems: string[] = [];
  page.on("pageerror", (e) => problems.push(`script error: ${e.message}`));
  page.on("console", (m) => {
    if (m.type() === "error" && /Content Security Policy|Refused to/i.test(m.text())) problems.push(`CSP: ${m.text()}`);
  });
  page.on("response", (r) => {
    if (r.status() >= 500) problems.push(`${r.status()} ${r.request().method()} ${r.url()}`);
  });
  return problems;
}

test.describe("website and portals @site", () => {
  test.skip(!SITE, "E2E_SITE_URL is not set");

  test("API is healthy and runs the deployed version", async ({ request }) => {
    const res = await request.get(`${SITE}/api/v1/healthz`);
    expect(res.status()).toBe(200);
    const body = (await res.json()) as { status: string; db: boolean; version: string };
    expect(body.status).toBe("ok");
    expect(body.db).toBe(true);
    if (VERSION) expect(body.version).toBe(VERSION);
  });

  const pages = ["/", "/services", "/areas", "/pricing", "/how-it-works", "/faq", "/contact", "/privacy", "/book", "/join"];
  for (const path of pages) {
    test(`public page ${path} renders`, async ({ page }) => {
      const problems = watch(page);
      const res = await page.goto(SITE + path);
      expect(res?.status(), path).toBeLessThan(400);
      expect(res?.headers()["content-security-policy"], "Content-Security-Policy header").toBeTruthy();
      await expect(page.locator("h1").first()).toBeVisible();
      await page.waitForLoadState("networkidle");
      expect(problems).toEqual([]);
    });
  }

  test("sitemap lists pages", async ({ request }) => {
    const res = await request.get(`${SITE}/sitemap.xml`);
    expect(res.status()).toBe(200);
    expect(await res.text()).toContain("<urlset");
  });

  for (const [name, path] of [["household portal", "/account/login"], ["provider portal", "/provider/login"]]) {
    // Email and password where that sign-in exists, otherwise phone and SMS code.
    test(`${name} sign-in screen shows a sign-in form`, async ({ page }) => {
      const problems = watch(page);
      const res = await page.goto(SITE + path);
      expect(res?.status(), path).toBeLessThan(400);
      await expect(page.locator('input[type="password"], input[name="phone"][type="tel"]').first()).toBeVisible();
      await page.waitForLoadState("networkidle");
      expect(problems).toEqual([]);
    });
  }

  test("unknown API paths answer with the JSON error envelope", async ({ request }) => {
    const res = await request.get(`${SITE}/api/v1/this-route-does-not-exist`);
    expect(res.status()).toBe(404);
    expect(res.headers()["content-type"]).toContain("json");
  });
});

test.describe("staff console @console", () => {
  test.skip(!CONSOLE, "E2E_CONSOLE_URL is not set");

  test("console loads its sign-in screen", async ({ page }) => {
    const problems = watch(page);
    const res = await page.goto(CONSOLE + "/");
    expect(res?.status()).toBeLessThan(400);
    if (CONSOLE!.startsWith("https://")) {
      expect(res?.headers()["content-security-policy"], "CloudFront sets the console CSP").toBeTruthy();
      expect(res?.headers()["strict-transport-security"], "CloudFront sets HSTS").toBeTruthy();
    }
    await expect(page.getByRole("button", { name: "Continue" })).toBeVisible();
    await page.waitForLoadState("networkidle");
    expect(problems).toEqual([]);
  });

  test("console reaches the API through CloudFront", async ({ request }) => {
    const res = await request.get(`${CONSOLE}/api/v1/healthz`);
    expect(res.status()).toBe(200);
    expect(((await res.json()) as { status: string }).status).toBe("ok");
  });
});

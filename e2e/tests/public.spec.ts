import { expect, test, type Page } from "@playwright/test";
import { SITE, newPhone } from "../lib/env";
import { one } from "../lib/db";
import { latestCheckout, stkCallback } from "../lib/mpesa";
import { act, consolePage, goConsole, portalPage, watch } from "../lib/ui";
import { client } from "../lib/world";

// What the public sees and what the office edits for them: enquiries from the contact form land
// in the console's Leads, FAQs written in the console show on the site and in search results'
// structured data, website pages are served to the site, and a household plan made in the console
// can be bought from the portal by M-Pesa.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const stamp = Date.now().toString(36);
let ops: Page, visitor: Page;

test.beforeAll(async ({ browser }) => {
  ops = await consolePage(browser, "ops", problems);
  visitor = await (await browser.newContext()).newPage();
  watch(visitor, "public", problems);
});

test.afterAll(() => {
  expect(problems, "script errors or server errors").toEqual([]);
});

test("home page leads to booking, services and recruitment", async () => {
  await visitor.goto(SITE + "/");
  await expect(visitor.locator("h1")).toContainText("Trusted help at home");
  await visitor.getByRole("link", { name: /services/i }).first().click();
  await expect(visitor).toHaveURL(/\/services/);
  await expect(visitor.locator("main")).toContainText(/Cleaning/i);
  await visitor.goto(SITE + "/services/cleaning/kilimani");
  await expect(visitor.locator("main")).toContainText(/Kilimani/);
  await expect(visitor.locator("main")).toContainText(/KES [\d,]+/); // live rate card, not a placeholder
  const ld = await visitor.locator('script[type="application/ld+json"]').allInnerTexts();
  expect(ld.join("\n"), "structured data for search engines").toContain("@context");
});

test("contact form: missing details are explained, a good enquiry reaches Leads", async () => {
  await visitor.goto(SITE + "/contact");
  const form = visitor.locator("#contact-form");
  await form.getByRole("button", { name: "Send" }).click();
  await expect(form).toContainText("Please tell us your name.");
  await form.locator("input[name=full_name]").fill(`Lead ${stamp}`);
  await form.getByRole("button", { name: "Send" }).click();
  await expect(form).toContainText("Give us a phone number or an email so we can reply.");
  await form.locator("input[name=phone]").fill("12");
  await form.getByRole("button", { name: "Send" }).click();
  await expect(form).toContainText("Enter a Kenyan mobile number");
  await form.locator("input[name=phone]").fill(newPhone().local);
  await form.locator("select[name=category]").selectOption("cleaning");
  await form.locator("textarea[name=message]").fill(`Weekly cleaning for a 2-bedroom flat (${stamp})`);
  await form.getByRole("button", { name: "Send" }).click();
  await expect(visitor.getByRole("status")).toContainText(`Thank you, Lead ${stamp}`);

  await goConsole(ops, "/leads");
  const row = ops.getByRole("row").filter({ hasText: `Lead ${stamp}` });
  await expect(row).toContainText("House cleaning");
  await expect(row).toContainText(stamp);
});

test("FAQs: one written in the console shows on the FAQ page; unpublished ones do not", async () => {
  await goConsole(ops, "/faqs");
  await act(ops, "New FAQ", { question: `Do you bring supplies? ${stamp}`, answer: "Yes, our cleaners bring eco-friendly supplies.", is_published: true });
  await act(ops, "New FAQ", { question: `Draft question ${stamp}`, answer: "Not ready yet.", is_published: false });
  await visitor.goto(SITE + "/faq");
  await expect(visitor.locator("main")).toContainText(`Do you bring supplies? ${stamp}`);
  await expect(visitor.locator("main")).not.toContainText(`Draft question ${stamp}`);
  const ld = (await visitor.locator('script[type="application/ld+json"]').allInnerTexts()).join("\n");
  expect(ld).toContain("FAQPage");

  // Deactivating takes it off the site.
  await goConsole(ops, "/faqs");
  await act(ops, "Deactivate", {}, { within: ops.getByRole("row").filter({ hasText: `Do you bring supplies? ${stamp}` }) });
  await visitor.reload();
  await expect(visitor.locator("main")).not.toContainText(`Do you bring supplies? ${stamp}`);
});

test("website pages: a published page is served to the site, a draft is not", async () => {
  await goConsole(ops, "/content-pages");
  await act(ops, "New page", { title: `Moving-out cleans ${stamp}`, slug: `moving-out-${stamp}`, body_markdown: "## Leave it spotless\n\nWe clean before you hand back the keys.", meta_description: "End-of-tenancy cleaning in Nairobi.", is_published: true });
  await act(ops, "New page", { title: `Draft ${stamp}`, slug: `draft-${stamp}`, body_markdown: "Not yet.", is_published: false });
  const pub = await fetch(`${SITE}/api/v1/public/pages/moving-out-${stamp}`);
  expect(pub.status).toBe(200);
  expect(JSON.stringify(await pub.json())).toContain("Leave it spotless");
  expect((await fetch(`${SITE}/api/v1/public/pages/draft-${stamp}`)).status).toBe(404);
});

test("message templates: the office can edit what an SMS says", async () => {
  await goConsole(ops, "/notification-templates");
  await act(ops, "New template", { event_key: `e2e.check.${stamp}`, channel: "sms", language: "en", body: "Hello {{name}}, this is a test.", is_active: true });
  await expect(ops.getByRole("row").filter({ hasText: `e2e.check.${stamp}` })).toBeVisible();
});

test("household plans: a plan made in the console is bought in the portal by M-Pesa", async ({ browser }) => {
  await goConsole(ops, "/subscription-plans");
  const name = `Weekly fresh ${stamp}`;
  await act(ops, "New plan", { code: `WF-${stamp}`, name, description: "Four standard cleans a month", price_minor: 6000, billing_period: "monthly", is_active: true });
  await expect(ops.getByRole("row").filter({ hasText: name })).toContainText("6,000");

  const c = await client("Plan Household");
  const page = await portalPage(browser, "client", c.token, problems);
  await page.goto(SITE + "/account/plans");
  const card = page.locator("form").filter({ hasText: name });
  await expect(card).toContainText("KES 6,000");
  await card.locator("input[name=phone]").fill(c.phone.local);
  await card.getByRole("button", { name: `Start ${name}` }).click();
  await expect(page.locator("body")).toContainText("Approve the M-Pesa prompt");
  await expect(page.locator("main")).toContainText(`Your plan: ${name}`);

  const inv = await one<{ id: string; total: string }>(
    `SELECT i.id, i.total_minor AS total FROM invoices i JOIN subscriptions s ON s.id = i.subscription_id WHERE s.client_id = $1`, c.userId);
  await stkCallback(await latestCheckout(inv.id), Number(inv.total) / 100, `E2EPLAN${stamp.slice(-3).toUpperCase()}`);
  await expect.poll(async () => (await one<{ s: string }>(`SELECT status::text AS s FROM subscriptions WHERE client_id = $1`, c.userId)).s).toBe("active");
  await page.reload();
  await expect(page.locator("main")).toContainText(/Renews on/);

  await page.getByRole("button", { name: "Cancel plan" }).click();
  await expect(page.locator("main")).toContainText(/Ends on/);
});

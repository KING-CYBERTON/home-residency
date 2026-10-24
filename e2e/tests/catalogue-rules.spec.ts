import { expect, test, type Page } from "@playwright/test";
import { SITE } from "../lib/env";
import { call, staffToken, type ApiError } from "../lib/api";
import { act, consolePage, goConsole, portalPage } from "../lib/ui";
import { one } from "../lib/db";
import { client, service } from "../lib/world";

// The catalogue rules from QA round 2: area pages exist only where a category has a price, a
// renamed area keeps its old addresses with a permanent redirect, each category's hours and notice
// are enforced when a visit is booked, an inactive category (House fumigation) cannot be booked,
// and the request queue shows the service area rather than the address line.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const run = Date.now().toString(36).slice(-5);
let ops: Page;
let token: string;

test.beforeAll(async ({ browser }) => {
  ops = await consolePage(browser, "ops", problems);
  token = await staffToken("ops");
});

test.afterAll(() => {
  expect(problems, "script errors or server errors").toEqual([]);
});

/** Fetches a site page without following redirects. */
async function head(path: string) {
  const res = await fetch(SITE + path, { redirect: "manual" });
  return { status: res.status, location: res.headers.get("location") };
}

async function refusal(p: Promise<unknown>): Promise<{ status: number; message: string }> {
  const err = (await p.then(
    () => null,
    (e: ApiError) => e,
  )) as ApiError | null;
  expect(err, "the API should refuse this").not.toBeNull();
  return { status: err!.status, message: (err!.body as { message: string }).message };
}

test("area pages: only areas with a price for the category get a page", async () => {
  const north = await call(token, "POST", "/console/service-areas", { slug: `ruaka-${run}`, name: `Ruaka ${run}`, county: "Kiambu", is_active: true });
  const kilimani = await one<{ id: string }>(`SELECT id FROM service_areas WHERE slug = 'kilimani'`);
  const cat = await call(token, "POST", "/console/categories", { slug: `tiling-${run}`, name: `Tiling ${run}`, engagement: "one_off", vetting_tier: "trade", is_active: true });
  const svc = await call(token, "POST", "/console/services", { category_id: cat.id, slug: `floor-${run}`, name: `Floor tiling ${run}`, pricing_unit: "per_job", is_active: true });
  await call(token, "POST", "/console/rate-card", {
    service_id: svc.id, service_area_id: kilimani.id, client_price_minor: 800000, provider_pay_minor: 600000, valid_from: new Date(Date.now() - 60_000).toISOString(),
  });

  expect((await head(`/services/tiling-${run}/kilimani`)).status).toBe(200);
  expect((await head(`/services/tiling-${run}/${north.slug}`)).status).toBe(404);
  const page = await (await fetch(SITE + `/services/tiling-${run}`)).text();
  expect(page).toContain(`href="/services/tiling-${run}/kilimani"`);
  expect(page).not.toContain(`href="/services/tiling-${run}/${north.slug}"`);
  const sitemap = await (await fetch(SITE + "/sitemap.xml")).text();
  expect(sitemap).toContain(`/services/tiling-${run}/kilimani`);
  expect(sitemap).not.toContain(`/services/tiling-${run}/${north.slug}`);
});

test("renamed area: the old addresses redirect permanently to the new slug", async () => {
  await call(token, "POST", "/console/service-areas", { slug: `kasa-${run}`, name: `Kasa ${run}`, county: "Nairobi", is_active: true });
  await goConsole(ops, "/service-areas");
  await act(ops, "Edit", { name: `Kasarani ${run}`, slug: `kasarani-${run}` }, { within: ops.getByRole("row").filter({ hasText: `Kasa ${run}` }) });
  await expect(ops.getByRole("row").filter({ hasText: `Kasarani ${run}` })).toBeVisible();

  expect(await head(`/areas/kasa-${run}`)).toEqual({ status: 301, location: `/areas/kasarani-${run}` });
  expect(await head(`/services/cleaning/kasa-${run}?from=sms`)).toEqual({ status: 301, location: `/services/cleaning/kasarani-${run}?from=sms` });
  expect((await head(`/areas/kasarani-${run}`)).status).toBe(200);
  expect((await head(`/areas/never-${run}`)).status).toBe(404);
});

test("service hours and notice: set per category in the console and enforced on booking", async ({ browser }) => {
  // Appliance installation (Electrical) is not used by the other specs, so its hours can change here.
  const svc = await service("appliance-install");
  const c = await client(`Hours Household ${run}`);
  await goConsole(ops, "/categories");
  const row = ops.getByRole("row").filter({ hasText: "Electrical" });
  await act(ops, "Edit", { opens_at: "08:00", closes_at: "17:00", min_lead_minutes: 180 }, { within: row });
  await expect(row).toContainText("08:00–17:00");
  await expect(row).toContainText("3 hours");

  try {
    const book = (start: Date) =>
      call(c.token, "POST", "/client/requests", { accept_terms: true, service_id: svc.id, address_id: c.addressId, requested_start: start.toISOString() });
    // Tomorrow at a given Nairobi time (UTC+3).
    const at = (hh: number, mm = 0) => {
      const d = new Date(Date.now() + 24 * 3600_000);
      return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate(), hh - 3, mm));
    };

    const soon = await refusal(book(new Date(Date.now() + 60 * 60_000)));
    expect(soon.status).toBe(422);
    expect(soon.message).toContain("Electrical needs 3 hours' notice");
    const late = await refusal(book(at(18, 30)));
    expect(late.message).toBe("Electrical visits start between 08:00 and 17:00");
    expect((await refusal(book(at(7, 45)))).status).toBe(422);
    await book(at(10));

    // The household's booking form says when visits can start, and shows the refusal in words.
    const page = await portalPage(browser, "client", c.token, problems);
    await page.goto(SITE + "/account/book?category=electrical");
    await expect(page.locator("main")).toContainText("Electrical visits start between 08:00 and 17:00 and need booking at least 3 hours ahead.");
    await page.getByLabel(/Appliance installation/).check();
    const tomorrow = at(20).toISOString(); // 23:00 Nairobi
    const local = new Date(new Date(tomorrow).getTime() + 3 * 3600_000).toISOString().slice(0, 16);
    await page.fill("input[name=start]", local);
    await page.getByLabel(/I agree to the terms of service/).check();
    await page.getByRole("button", { name: "Book now" }).click();
    await expect(page.getByRole("alert").filter({ hasText: "Electrical visits start between 08:00 and 17:00" }).first()).toBeVisible();
  } finally {
    const cat = await one<{ id: string }>(`SELECT id FROM service_categories WHERE slug = 'electrical'`);
    await call(token, "PATCH", `/console/categories/${cat.id}`, { opens_at: "", closes_at: "", min_lead_minutes: 0 });
  }
});

test("House fumigation is not offered and cannot be booked while Pest control is inactive", async () => {
  const fumigation = await service("fumigation-house");
  const cat = await one<{ is_active: boolean }>(`SELECT is_active FROM service_categories WHERE id = $1`, fumigation.categoryId);
  expect(cat.is_active).toBe(false);
  const c = await client(`Fumigation Household ${run}`);

  const quote = await refusal(call(c.token, "POST", "/client/quotes", { service_id: fumigation.id, address_id: c.addressId }));
  expect(quote).toEqual({ status: 422, message: "This service is not available yet" });
  const booking = await refusal(call(c.token, "POST", "/client/requests", {
    accept_terms: true, service_id: fumigation.id, address_id: c.addressId, requested_start: new Date(Date.now() + 3 * 24 * 3600_000).toISOString(),
  }));
  expect(booking.status).toBe(422);

  expect((await head("/services/pest-control")).status).toBe(404);
  const services = await (await fetch(SITE + "/services")).text();
  expect(services).not.toContain("House fumigation");
  expect(await (await fetch(SITE + "/sitemap.xml")).text()).not.toContain("/services/pest-control");
});

test("request queue: the Area column is the service area, with the estate underneath", async () => {
  const c = await client(`Queue Household ${run}`, "westlands");
  const svc = await service();
  const req = await call(c.token, "POST", "/client/requests", {
    accept_terms: true, service_id: svc.id, address_id: c.addressId, requested_start: new Date(Date.now() + 5 * 24 * 3600_000).toISOString(),
  });
  await goConsole(ops, `/requests?q=${req.reference}`);
  const row = ops.getByRole("row").filter({ hasText: req.reference });
  await expect(row).toContainText("Westlands");
  await expect(row).toContainText("Kilimani Gardens"); // the estate typed into the address, shown as a detail
});

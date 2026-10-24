import { expect, test, type Page } from "@playwright/test";
import { SITE } from "../lib/env";
import { call, staffToken } from "../lib/api";
import { one } from "../lib/db";
import { act, consolePage, goConsole, nairobi, portalPage } from "../lib/ui";
import { acceptedJob, activeProvider, client, service, type Person } from "../lib/world";

// Triage beyond the straight path in journey.spec: the household chooses from a shortlist, a
// provider declines, an offer is withdrawn, the household cancels, a job is marked unfilled,
// extra work is proposed by the provider and approved or declined by the household, and a
// supervisor records a check-in by hand.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const run = Date.now().toString(36).slice(-4); // names stay unique when the suite runs again on the same database
let ops: Page;
let mary: Person, peter: Person;
let household: Person & { addressId: string };

async function book(startInMinutes: number) {
  const svc = await service();
  return call<{ id: string; reference: string }>(household.token, "POST", "/client/requests", {
    accept_terms: true,
    service_id: svc.id, address_id: household.addressId, requested_start: new Date(Date.now() + startInMinutes * 60_000).toISOString(),
  });
}

test.beforeAll(async ({ browser }) => {
  ops = await consolePage(browser, "ops", problems);
  mary = await activeProvider(`Mary Atieno-${run}`);
  peter = await activeProvider(`Peter Otieno-${run}`);
  household = await client("Wanjiru Kamau");
});

test.afterAll(() => {
  expect(problems, "script errors or server errors").toEqual([]);
});

test("shortlist: the household picks between two providers; one declines, the other accepts", async ({ browser }) => {
  const req = await book(48 * 60);
  await goConsole(ops, `/requests/${req.id}`);
  await act(ops, "Claim");
  await act(ops, "Generate shortlist", { size: 10 });
  const maryRow = ops.getByRole("row").filter({ hasText: mary.name });
  const peterRow = ops.getByRole("row").filter({ hasText: peter.name });
  await maryRow.getByRole("checkbox", { name: "Select" }).click();
  await peterRow.getByRole("checkbox", { name: "Select" }).click();
  await ops.getByRole("button", { name: "Compare 2" }).click();
  await act(ops, "Offer client 2 choices");
  await expect(ops.locator("main")).toContainText("Awaiting client choice");

  // The household sees first names and an initial, never phone numbers.
  const c = await portalPage(browser, "client", household.token, problems);
  await c.goto(`${SITE}/account/requests/${req.id}`);
  await expect(c.locator("main")).toContainText("Choose your provider");
  await expect(c.locator("main")).not.toContainText(mary.phone.e164.slice(4));
  await c.getByRole("button", { name: "Choose Mary A." }).click();
  await expect(c.locator("body")).toContainText("We will confirm with the provider you chose");

  await ops.reload();
  await expect(maryRow).toContainText("Client's choice");
  await act(ops, "Assign", { selection_reason: "The household chose Mary" }, { within: maryRow });

  // Mary declines; triage offers Peter, who accepts.
  const m = await portalPage(browser, "provider", mary.token, problems);
  await m.goto(SITE + "/provider/offers");
  await m.getByRole("button", { name: "Decline" }).first().click();
  await expect(m.getByRole("status")).toBeVisible();
  await ops.reload();
  await expect(ops.locator("main")).toContainText("Declined");
  await act(ops, "Assign", { selection_reason: "Mary declined; Peter is next closest" }, { within: peterRow });
  const p = await portalPage(browser, "provider", peter.token, problems);
  await p.goto(SITE + "/provider/offers");
  await p.getByRole("button", { name: "Accept" }).first().click();
  await expect(p.getByRole("link", { name: "Open the job" })).toBeVisible();
  await c.reload();
  await expect(c.locator("main")).toContainText("Peter O.");
});

test("offers: triage withdraws an offer before the provider answers", async ({ browser }) => {
  const req = await book(72 * 60);
  const t = await staffToken("ops");
  await call(t, "POST", `/console/requests/${req.id}/claim`);
  await call(t, "POST", `/console/requests/${req.id}/assignments`, { provider_id: mary.userId, selection_reason: "Closest" });
  await goConsole(ops, `/requests/${req.id}`);
  await act(ops, "Withdraw", { reason: "Household asked for someone they know" });
  await expect(ops.locator("main")).toContainText("Withdrawn");
  const m = await portalPage(browser, "provider", mary.token, problems);
  await m.goto(SITE + "/provider/offers");
  await expect(m.locator("main")).not.toContainText(req.reference);
});

test("cancellation: the household cancels from the portal and triage sees why", async ({ browser }) => {
  const req = await book(96 * 60);
  const c = await portalPage(browser, "client", household.token, problems);
  await c.goto(`${SITE}/account/requests/${req.id}`);
  await c.getByText("Cancel this booking").click();
  await c.locator("select[name=reason]").selectOption("Plans changed");
  await c.getByRole("button", { name: "Cancel booking" }).click();
  await expect(c.locator("main")).toContainText("Plans changed");
  await goConsole(ops, `/requests/${req.id}`);
  await expect(ops.locator("h1")).toContainText("Cancelled");
});

test("unfilled: triage closes a request nobody can take, with a reason", async () => {
  const req = await book(120 * 60);
  await goConsole(ops, `/requests/${req.id}`);
  await act(ops, "Claim");
  await act(ops, "Mark unfilled", { reason: "No provider free in Kilimani that day" });
  await expect(ops.locator("h1")).toContainText("Unfilled");
});

test("variations: the provider asks for extra work; the household approves one and declines another", async ({ browser }) => {
  const job = await acceptedJob(await staffToken("ops"), household, peter, 30);
  const p = await portalPage(browser, "provider", peter.token, problems);
  for (const what of ["Clean the balcony as well", "Wash the curtains"]) {
    await p.goto(`${SITE}/provider/jobs/${job.assignmentId}`);
    await p.getByText("The client wants more work done").click();
    await p.locator("#var-desc").fill(what);
    await p.locator("#var-qty").fill("1");
    await p.getByRole("button", { name: "Send to the office" }).click();
    await expect(p.locator("body")).toContainText(/sent|office/i);
  }
  const c = await portalPage(browser, "client", household.token, problems);
  await c.goto(`${SITE}/account/requests/${job.requestId}`);
  const balcony = c.locator("div.rounded-xl").filter({ hasText: "Clean the balcony as well" });
  await expect(balcony).toContainText(/Adds KES [\d,]+/);
  await balcony.getByRole("button", { name: "Approve" }).click();
  await c.locator("div.rounded-xl").filter({ hasText: "Wash the curtains" }).getByRole("button", { name: "Decline" }).click();

  await goConsole(ops, "/variations?status=approved");
  await expect(ops.getByRole("row").filter({ hasText: "Clean the balcony as well" })).toBeVisible();
  await goConsole(ops, "/variations?status=rejected");
  await expect(ops.getByRole("row").filter({ hasText: "Wash the curtains" })).toBeVisible();
});

test("visits: a supervisor records a check-in by hand when the provider's phone is dead", async () => {
  const job = await acceptedJob(await staffToken("ops"), household, mary, 20);
  await goConsole(ops, `/requests/${job.requestId}`);
  await act(ops, "Manual check-in", { checked_in_at: nairobi(0), reason: "Provider's phone battery died; client confirmed by call" });
  await expect(ops.locator("main")).toContainText("Checked in");
  const v = await one<{ m: string }>(`SELECT checkin_method::text AS m FROM visits WHERE id = $1`, job.visitId);
  expect(v.m).toBe("console");
});

test("exceptions: a missed visit is logged and resolved with what was done", async () => {
  const joseph = await activeProvider(`Joseph Kamau-${run}`);
  const job = await acceptedJob(await staffToken("ops"), household, joseph, 45);
  await goConsole(ops, `/requests/${job.requestId}`);
  await act(ops, "Change", { status: "missed", reason: "Provider did not arrive" });
  await goConsole(ops, "/exceptions");
  const row = ops.getByRole("row").filter({ hasText: /No show/i }).first();
  await expect(row).toBeVisible();
  await act(ops, "Resolve", { resolution: "Provider was ill; household rebooked for tomorrow" }, { within: row });
  await goConsole(ops, "/exceptions?open=false");
  await expect(ops.locator("main")).toContainText("Provider was ill");
});

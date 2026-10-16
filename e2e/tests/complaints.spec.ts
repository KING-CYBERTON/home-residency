import { expect, test, type Page } from "@playwright/test";
import { SITE } from "../lib/env";
import { ApiError, call, staffToken } from "../lib/api";
import { one } from "../lib/db";
import { act, consolePage, goConsole, portalPage } from "../lib/ui";
import { acceptedJob, activeProvider, client, workDone, type Person } from "../lib/world";

// The quality loop: a household reports a problem with a photo, a manager assigns a supervisor to
// investigate, the provider gives their side in their portal, the investigator reports, a different
// manager decides (the investigator may not), the outcome reaches both sides and the case closes.
// Spot checks and a low rating feed the same queue.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const run = Date.now().toString(36).slice(-4);
const png = { name: "stain.png", mimeType: "image/png", buffer: Buffer.from("89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000d49444154789c6360000002000154a24f5d0000000049454e44ae426082", "hex") };

let manager: Page, investigator: Page;
let provider: Person, household: Person & { addressId: string };
let job: Awaited<ReturnType<typeof acceptedJob>>;
let complaintUrl = "";

test.beforeAll(async ({ browser }) => {
  manager = await consolePage(browser, "approver", problems); // complaints_manager
  investigator = await consolePage(browser, "supervisor2", problems); // supervisor and complaints_manager
  provider = await activeProvider(`Esther Wairimu-${run}`);
  household = await client(`Otieno Family-${run}`);
  job = await acceptedJob(await staffToken("ops"), household, provider, 30);
});

test.afterAll(() => {
  expect(problems, "script errors or server errors").toEqual([]);
});

test("household: reports a problem with a photo from the booking", async ({ browser }) => {
  const c = await portalPage(browser, "client", household.token, problems);
  await c.goto(`${SITE}/account/requests/${job.requestId}`);
  await c.getByRole("link", { name: "Report a problem with this booking" }).click();
  await c.locator("label:has(input[name=severity][value=quality])").click();
  await expect(c.locator("select[name=request_id]")).toHaveValue(job.requestId);
  await c.locator("input[name=summary]").fill(`Kitchen floor left sticky ${run}`);
  await c.locator("textarea[name=details]").fill("The floor was not rinsed after mopping.");
  await c.locator("input[type=file][data-upload]").setInputFiles(png);
  await expect(c.locator("[data-upload-status]")).toHaveText("Uploaded stain.png");
  await expect(c.locator("#evidence")).not.toHaveValue("");
  await c.getByRole("button", { name: "Send report" }).click();
  await expect(c.locator("h1")).toContainText(`Kitchen floor left sticky ${run}`);
  const row = await one<{ n: string }>(
    `SELECT count(*)::text AS n FROM complaint_events e JOIN complaints c ON c.id = e.complaint_id WHERE c.summary = $1 AND e.event_type = 'evidence' AND e.document_id IS NOT NULL`,
    `Kitchen floor left sticky ${run}`);
  expect(row.n, "the photo is attached to the complaint").toBe("1");
});

test("manager: assigns a supervisor to investigate", async () => {
  await goConsole(manager, "/complaints");
  await manager.getByRole("row").filter({ hasText: `Kitchen floor left sticky ${run}` }).click();
  await manager.waitForLoadState("networkidle");
  complaintUrl = manager.url();
  const sup = await one<{ id: string }>(`SELECT id FROM users WHERE email = 'supervisor2@e2e.test'`);
  await act(manager, "Assign investigator", { investigator_id: sup.id });
  await expect(manager.locator("main")).toContainText("Investigating");
});

test("investigator: asks the provider for their side; the provider answers in their portal", async ({ browser }) => {
  await investigator.goto(complaintUrl);
  await investigator.waitForLoadState("networkidle");
  await act(investigator, "Ask provider for account");
  await act(investigator, "Add note", { event_type: "note", body: "Called the household; photo matches.", is_internal: true });

  const p = await portalPage(browser, "provider", provider.token, problems);
  await p.goto(SITE + "/provider/complaints");
  await expect(p.locator("main")).toContainText(`Kitchen floor left sticky ${run}`);
  await expect(p.locator("main"), "internal notes stay internal").not.toContainText("photo matches");
  await p.locator("textarea[name=account]").fill("The mop bucket ran out of clean water; I will bring two next time.");
  await p.getByRole("button", { name: "Send my account" }).click();
  await expect(p.locator("main")).not.toContainText("Send my account");

  await investigator.reload();
  await expect(investigator.locator("main")).toContainText("I will bring two next time");
  await act(investigator, "Submit report", { investigation_report: "Floor not rinsed. Provider accepts it. First complaint." });
  await expect(investigator.locator("main")).toContainText("Awaiting decision");
});

test("decision: the investigator may not decide their own case; another manager does", async () => {
  await expect(investigator.getByRole("button", { name: "Decide" })).toBeDisabled();
  // and the API says no too, so it is not only the button
  const id = complaintUrl.split("/").pop()!;
  const refused = await call(await staffToken("supervisor2"), "POST", `/console/complaints/${id}/decision`, { action: "warning", outcome_summary: "A warning.", reason: "First minor complaint" }).then(() => null, (e: ApiError) => e);
  expect(refused?.status).toBe(409);
  await manager.reload();
  await act(manager, "Decide", { action: "warning", outcome_summary: "Your provider has been given a formal warning and we will check her next job.", reason: "First complaint, accepted fault" });
  await expect(manager.locator("main")).toContainText("Decided");
  await expect(manager.locator("main")).toContainText(/Warning/);
});

test("outcome: both sides are told, the case closes, and the household sees the outcome", async ({ browser }) => {
  await act(manager, "Communicate outcome", { to: "client", message: "Thank you for telling us. We have warned your provider and will check her next visit.", channels: ["In app"] });
  await act(manager, "Communicate outcome", { to: "provider", message: "Please complete the cleaning refresher this week.", channels: ["In app"] });
  await act(manager, "Close case");
  await expect(manager.locator("main")).toContainText("Closed");

  const c = await portalPage(browser, "client", household.token, problems);
  await c.goto(SITE + "/account/complaints");
  await c.getByText(`Kitchen floor left sticky ${run}`).click();
  await expect(c.locator("main")).toContainText("formal warning");
  await c.goto(SITE + "/account/notifications");
  await expect(c.locator("main")).toContainText("Thank you for telling us");
  const p = await portalPage(browser, "provider", provider.token, problems);
  await p.goto(SITE + "/provider/complaints");
  await expect(p.locator("main")).toContainText("Outcome: Your provider has been given a formal warning");
});

test("spot check: a low score raises a complaint for the same queue", async ({ browser }) => {
  const sup = await consolePage(browser, "ops", problems); // supervisor
  await goConsole(sup, "/spot-checks");
  await act(sup, "Record spot check", { provider_id: provider.userId, visit_id: job.visitId, method: "phone_client", scores: '{"punctuality": 40, "quality": 30}', total_score: 35, notes: `Household unhappy ${run}`, raise_complaint: true });
  await expect(sup.locator("main")).toContainText(`Household unhappy ${run}`);
  await goConsole(manager, "/complaints");
  await expect(manager.getByRole("row").filter({ hasText: "Spot check scored 35 out of 100" }).first()).toBeVisible();
});

test("low rating: a 1-star rating opens a case, holds the earning and is logged as an exception", async ({ browser }) => {
  const brian = await activeProvider(`Brian Mwangi-${run}`);
  const second = await acceptedJob(await staffToken("ops"), household, brian, 20);
  await workDone(household, brian, second.visitId);
  const c = await portalPage(browser, "client", household.token, problems);
  await c.goto(`${SITE}/account/requests/${second.requestId}`);
  await c.getByRole("button", { name: "Confirm the work is done" }).click();
  await c.locator('label:has(input[name=stars][value="1"])').click();
  await c.fill("textarea[name=comment]", "Arrived late and left early");
  await c.getByRole("button", { name: "Submit rating" }).click();
  await expect(c.locator("main")).toContainText(/Done|Thank/);

  await goConsole(manager, "/complaints");
  await expect(manager.getByRole("row").filter({ hasText: "Automatic case: 1-star rating" }).first()).toBeVisible();
  const ops = await consolePage(browser, "ops", problems);
  await goConsole(ops, "/exceptions?kind=low_rating");
  await expect(ops.getByRole("row").filter({ hasText: /Low rating/i }).first()).toBeVisible();
  const e = await one<{ s: string }>(`SELECT status::text AS s FROM provider_earnings WHERE request_id = $1`, second.requestId);
  expect(e.s, "the provider's earning waits for the case").toBe("held");
});

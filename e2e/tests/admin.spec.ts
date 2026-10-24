import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import { CONSOLE } from "../lib/env";
import { totp } from "../lib/otp";
import { call, expectStatus, staffToken } from "../lib/api";
import { act, consolePage, fill, goConsole, watch } from "../lib/ui";
import { one } from "../lib/db";
import { client, service } from "../lib/world";

// Running the office: an admin invites a colleague, who signs in with password and authenticator
// code through the real sign-in screen; roles decide what they see; an account can be suspended
// and its authenticator reset; business settings change with an audit trail; the monthly reports
// render and export as CSV; a household's ID is checked and an account suspended and reinstated.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const run = Date.now().toString(36).slice(-5);
const colleague = { name: `Faith Njeri ${run}`, email: `faith.${run}@e2e.test`, password: "", secret: "" };
let admin: Page, approver: Page;

test.beforeAll(async ({ browser }) => {
  admin = await consolePage(browser, "ops", problems);
  approver = await consolePage(browser, "approver", problems);
});

test.afterAll(() => {
  expect(problems, "script errors or server errors").toEqual([]);
});

async function signIn(page: Page, email: string, password: string, code: string) {
  await page.goto(CONSOLE + "/login");
  await page.locator("#email").fill(email);
  await page.locator("#password").fill(password);
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.locator("#code")).toBeVisible();
  await page.locator("#code").fill(code);
  await page.getByRole("button", { name: "Sign in" }).click();
}

/** Waits for a 30-second step the server has not seen this account use. */
async function nextCode(secret: string): Promise<string> {
  await new Promise((r) => setTimeout(r, 30_000 - (Date.now() % 30_000) + 300));
  return totp(secret);
}

test("staff: an admin invites a colleague and is shown their sign-in details once", async () => {
  await goConsole(admin, "/staff");
  await admin.getByRole("button", { name: "Invite staff" }).click();
  await fill(admin, { full_name: colleague.name, email: colleague.email, roles: ["Reporting", "Triage officer"] });
  await admin.getByRole("dialog").locator('button[type="submit"]').click();
  // The invite form closes and the one-time sign-in details open in its place.
  const secret = admin.getByRole("dialog").filter({ hasText: "Sign-in details" });
  await expect(secret).toContainText(`Sign-in details for ${colleague.name}`);
  const values = await secret.locator("dd").allInnerTexts();
  colleague.password = values[0].trim();
  colleague.secret = values[1].trim();
  expect(colleague.password.length).toBeGreaterThanOrEqual(12);
  expect(values[2]).toContain("otpauth://totp/");
  await admin.keyboard.press("Escape");
  await expect(admin.getByRole("row").filter({ hasText: colleague.email })).toContainText(/Triage officer, Reporting|Reporting, Triage officer/);
});

test("sign-in: wrong password and wrong code are refused; the right ones open the console", async ({ browser }) => {
  test.setTimeout(150_000);
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  watch(page, "console:colleague", problems);
  await page.goto(CONSOLE + "/requests");
  await expect(page).toHaveURL(/\/login/); // signed-out visitors are sent to sign in

  await page.locator("#email").fill(colleague.email);
  await page.locator("#password").fill("not-the-password");
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByRole("alert")).toBeVisible();

  await page.locator("#password").fill(colleague.password);
  await page.getByRole("button", { name: "Continue" }).click();
  const right = totp(colleague.secret);
  await page.locator("#code").fill(right === "000000" ? "111111" : "000000");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await page.locator("#code").fill(totp(colleague.secret));
  await page.getByRole("button", { name: "Sign in" }).click();
  // back to where they were going before they were asked to sign in
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Request queue");

  // Roles decide what a colleague can open.
  await page.goto(CONSOLE + "/reports");
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Reports");
  await page.goto(CONSOLE + "/staff");
  await expect(page.getByText("Your roles do not include this area")).toBeVisible();
  await page.goto(CONSOLE + "/payments");
  await expect(page.getByText("Your roles do not include this area")).toBeVisible();

  // Signing out ends the session.
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
  await ctx.close();
});

test("staff: roles change, the authenticator is reset, and a suspended colleague cannot sign in", async ({ browser }) => {
  test.setTimeout(150_000);
  await goConsole(admin, "/staff");
  const row = admin.getByRole("row").filter({ hasText: colleague.email });
  await act(admin, "Roles", { roles: ["Finance"] }, { within: row });
  await expect(row).toContainText("Finance");

  await row.getByRole("button", { name: "Reset MFA" }).click();
  await admin.getByRole("dialog").locator('button[type="submit"]').click();
  const fresh = admin.getByRole("dialog").filter({ hasText: "New authenticator" });
  await expect(fresh).toBeVisible();
  const newSecret = (await fresh.locator("dd").allInnerTexts()).find((t) => /^[A-Z2-7]{16,}$/.test(t.trim()))!.trim();
  expect(newSecret).not.toBe(colleague.secret);
  await admin.keyboard.press("Escape");

  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  watch(page, "console:colleague", problems);
  await signIn(page, colleague.email, colleague.password, await nextCode(colleague.secret));
  await expect(page.getByRole("alert"), "the old authenticator stops working").toBeVisible();
  await signIn(page, colleague.email, colleague.password, totp(newSecret));
  await expect(page.getByRole("heading", { level: 1 })).toContainText(/Good/);
  await page.goto(CONSOLE + "/payments");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.getByText("Your roles do not include this area")).toHaveCount(0);

  await act(admin, "Edit", { status: "suspended" }, { within: row });
  await expect(row).toContainText("Suspended");
  await page.reload();
  await expect(page, "a suspended colleague is signed out").toHaveURL(/\/login/, { timeout: 15_000 });
  await ctx.close();
});

test("settings: a business rule changes and the audit log records who changed it", async () => {
  await goConsole(admin, "/settings");
  const row = admin.getByRole("row").filter({ hasText: "offer.expiry_minutes" });
  await act(admin, "Change", { value: "90" }, { within: row });
  await expect(row).toContainText("90");
  await act(admin, "Change", { value: "120" }, { within: row });
  await expect(row).toContainText("120");

  await goConsole(admin, "/audit-log?action=setting.update");
  await expect(admin.getByRole("row").filter({ hasText: "setting.update" }).first()).toBeVisible();
  await goConsole(admin, "/audit-log?action=staff.roles");
  await expect(admin.getByRole("row").filter({ hasText: "staff.roles" }).first()).toBeVisible();
});

test("reports: each management report renders and exports as CSV", async () => {
  for (const report of ["supply", "demand", "matching", "quality", "commercial", "compliance"]) {
    await test.step(report, async () => {
      await goConsole(admin, `/reports?report=${report}`);
      await expect.soft(admin.locator("main .text-destructive.border"), report).toHaveCount(0);
      const [download] = await Promise.all([admin.waitForEvent("download"), admin.getByRole("button", { name: "Export CSV" }).click()]);
      expect(download.suggestedFilename()).toMatch(new RegExp(`^${report}-.*\\.csv$`));
      const csv = readFileSync((await download.path())!, "utf8");
      expect.soft(csv.split("\n")[0], `${report} has a header row`).toMatch(/,/);
    });
  }
});

test("dashboard: the counters link to the queues behind them", async () => {
  await goConsole(admin, "/");
  await admin.getByRole("link", { name: /Requests to triage/ }).click();
  await expect(admin).toHaveURL(/\/requests/);
});

test("households: search, and suspending and reinstating an account", async () => {
  const c = await client(`Halima Yusuf ${run}`);
  await goConsole(admin, `/clients?q=${encodeURIComponent(`Halima Yusuf ${run}`)}`);
  await admin.getByRole("row").filter({ hasText: `Halima Yusuf ${run}` }).click();
  await admin.waitForLoadState("networkidle");
  await expect(admin.locator("main")).toContainText(c.phone.e164);
  await expect(admin.locator("main"), "gate codes are never shown to staff").not.toContainText("Gate code 1234");
  await act(admin, "Suspend", { reason: "Abusive messages to a provider" });
  await expect(admin.locator("main")).toContainText("Suspended");
  await act(admin, "Reinstate", { reason: "Apologised; first incident" });
  await expect(admin.locator("main")).toContainText("Active");
});

test("catalogue: an inactive category can be activated again, and its services only go live with it", async () => {
  await goConsole(admin, "/categories");
  const row = admin.getByRole("row").filter({ hasText: "Pest control" });
  await expect(row).toContainText("Inactive");
  await act(admin, "Activate", {}, { within: row });
  await expect(row).not.toContainText("Inactive");
  await act(admin, "Deactivate", {}, { within: row });
  await expect(row).toContainText("Inactive");

  const fumigation = await service("fumigation-house");
  const token = await staffToken("ops");
  await expectStatus(call(token, "PATCH", `/console/services/${fumigation.id}`, { is_active: true }), 422);
  await call(token, "PATCH", `/console/services/${fumigation.id}`, { is_active: false });
});

// ---- vetting checklists: the row editor, the diff against the current version, and approval

const requirements = (page: Page) => page.getByRole("listitem", { name: /^Requirement \d+$/ });

/** The editor row whose Name field holds this text. */
async function requirement(page: Page, name: string) {
  const rows = requirements(page);
  for (let i = 0; i < (await rows.count()); i++) if ((await rows.nth(i).getByLabel("Name").inputValue()) === name) return rows.nth(i);
  throw new Error(`no requirement named "${name}"`);
}

test("checklists: a new category's checklist is copied from another, edited row by row, and approved", async () => {
  const token = await staffToken("ops");
  const name = `Tiling ${run}`;
  const cat = await call(token, "POST", "/console/categories", { slug: `tiling-${run}`, name, engagement: "one_off", vetting_tier: "trade", is_active: true });
  const plumbing = await one<{ n: number }>(
    `SELECT count(*)::int AS n FROM checklist_template_items i JOIN checklist_templates t ON t.id = i.template_id JOIN service_categories c ON c.id = t.category_id
     WHERE c.slug = 'plumbing' AND t.is_current`,
  );

  await goConsole(admin, "/checklists");
  const section = admin.getByRole("region", { name });
  await expect(section).toContainText("No checklist: applicants can't be vetted");
  await section.getByRole("button", { name: new RegExp(name) }).click();
  await section.getByRole("link", { name: "Create checklist" }).click();
  await expect(admin.locator("#cl-category")).toHaveValue(cat.id);

  // Start from a copy of Plumbing's current checklist.
  const copy = await admin.locator("#cl-from option", { hasText: "Copy from Plumbing" }).getAttribute("value");
  await admin.locator("#cl-from").selectOption(copy!);
  await expect(requirements(admin)).toHaveCount(plumbing.n);
  await expect(admin.locator("main")).toContainText("Trade certificate (NITA or TVET)");
  await expect(admin.locator("main")).toContainText(`${name} has no current checklist, so everything here is new.`);

  // Rename the licence and make it required.
  const licence = await requirement(admin, "Valid trade licence (EPRA or NCA)");
  await licence.getByLabel("Name").fill("NCA tiling licence");
  await licence.getByLabel("Required").check();

  // A new row is checked as it is filled in.
  await admin.getByRole("button", { name: "Add requirement" }).click();
  const added = requirements(admin).last();
  await admin.getByRole("button", { name: "Save draft" }).click();
  await expect(admin.getByRole("alert").filter({ hasText: "Fix the highlighted requirements" })).toBeVisible();
  await expect(added).toContainText("Give it a name.");
  await expect(added).toContainText("Choose which document.");
  await added.getByLabel("Name").fill("NCA tiling licence");
  await expect(added).toContainText("Another requirement has this name.");
  await added.getByLabel("Name").fill("Tile adhesive safety course");
  await added.getByLabel("Type").selectOption({ label: "Training" });
  await expect(added.getByLabel("Which document")).toHaveCount(0);
  await added.getByLabel("Expires after (months)").fill("200");
  await expect(added).toContainText("1 to 120");
  await added.getByLabel("Expires after (months)").fill("36");
  await expect(added.locator("ul.text-destructive")).toHaveCount(0);

  // Reorder: up one place with the button, then dragged to the top by its handle.
  await added.getByRole("button", { name: "Move up" }).click();
  await expect(requirements(admin).nth(plumbing.n - 1).getByLabel("Name")).toHaveValue("Tile adhesive safety course");
  // A tall window keeps both rows on screen: a scroll between press and move would grab another handle.
  const viewport = admin.viewportSize()!;
  await admin.setViewportSize({ width: viewport.width, height: 2400 });
  await requirements(admin).nth(plumbing.n - 1).getByRole("button", { name: "Drag to reorder" }).dragTo(requirements(admin).first(), { targetPosition: { x: 40, y: 4 } });
  await admin.setViewportSize(viewport);
  await expect(requirements(admin).first().getByLabel("Name")).toHaveValue("Tile adhesive safety course");

  await admin.getByRole("button", { name: "Save draft" }).click();
  await expect(admin).toHaveURL(/\/checklists\?category_id=.*&saved=/);
  await expect(admin.getByRole("status")).toContainText(`Draft v1 of the ${name} checklist is saved. It is waiting for an approver to make it current.`);
  await expect(section).toContainText("Draft v1 waiting for approval");

  // The approver sees the draft and makes it current.
  await goConsole(approver, `/checklists?category_id=${cat.id}`);
  const theirs = approver.getByRole("region", { name });
  await expect(theirs).toContainText("Tile adhesive safety course");
  await act(approver, "Approve and make current", {}, { within: theirs });
  await expect(theirs).toContainText("v1 current");
  await expect(theirs).toContainText(`${plumbing.n + 1} requirements`);
  await expect(theirs).not.toContainText("waiting for approval");
});

test("checklists: a change to a current checklist shows what changed, and the old version is kept", async () => {
  await goConsole(admin, "/checklists");
  const section = admin.getByRole("region", { name: "Pest control" });
  await section.getByRole("button", { name: /Pest control/ }).click();
  await expect(section).toContainText("v1 current");
  await section.getByRole("link", { name: "Edit checklist" }).click();

  // It starts from the current version, and saving it unchanged is refused.
  await expect(admin.locator("#cl-from option:checked")).toContainText("current version (v1)");
  await expect(admin.getByRole("alert").filter({ hasText: "Nothing has changed from the current version (v1)" })).toBeVisible();

  // Standard requirements are marked, documents included, and removing one asks first.
  await expect(await requirement(admin, "National ID (both sides)")).toContainText("Standard");
  await expect(await requirement(admin, "Practical skills test")).not.toContainText("Standard");
  const interview = await requirement(admin, "In-person interview");
  await interview.getByRole("button", { name: "Remove" }).click();
  await expect(interview.getByRole("alert")).toContainText("This is a Standard requirement");
  await interview.getByRole("button", { name: "Keep it" }).click();
  await expect(requirements(admin).filter({ has: admin.getByRole("alert") })).toHaveCount(0);
  await interview.getByRole("button", { name: "Remove" }).click();
  await interview.getByRole("button", { name: "Remove anyway" }).click();

  await (await requirement(admin, "Certificate of Good Conduct (DCI)")).getByLabel("Expires after (months)").fill("24");
  await admin.getByRole("button", { name: "Add requirement" }).click();
  const medical = requirements(admin).last();
  await medical.getByLabel("Type").selectOption({ label: "Medical" });
  await medical.getByLabel("Name").fill("Chemical exposure medical");
  await medical.getByLabel("Expires after (months)").fill("12");

  const changes = admin.getByRole("list", { name: "Changes" });
  await expect(changes.locator('[data-change="removed"]')).toContainText("In-person interview");
  await expect(changes.locator('[data-change="changed"]')).toContainText("expires after 24 months (was 12)");
  await expect(changes.locator('[data-change="added"]')).toContainText("Chemical exposure medical");
  await expect(changes.locator('[data-change="same"]').first()).toBeVisible();

  // The same list in the Advanced text mode.
  await admin.getByText("Advanced: edit as text").click();
  await expect(admin.getByLabel("Requirements as text")).toHaveValue(/medical \| Chemical exposure medical \| yes \| {2}\| 12/);
  await expect(admin.getByLabel("Requirements as text")).toHaveValue(/clearance \| Certificate of Good Conduct \(DCI\) \| yes \| {2}\| 24/);

  await admin.getByRole("button", { name: "Save draft" }).click();
  await expect(admin.getByRole("status")).toContainText("Draft v2 of the Pest control checklist is saved");

  // The approver reviews the same diff, then approves; v1 is kept as replaced.
  await goConsole(approver, "/checklists");
  const theirs = approver.getByRole("region", { name: "Pest control" });
  await theirs.getByRole("button", { name: /Pest control/ }).click();
  await expect(theirs).toContainText("Draft v2 waiting for approval");
  await expect(theirs.locator('[data-change="removed"]')).toContainText("In-person interview");
  await act(approver, "Approve and make current", {}, { within: theirs });
  await expect(theirs).toContainText("v2 current");
  await expect(theirs).toContainText("Earlier versions: v1 (replaced)");
  await expect(theirs).toContainText("Not asked for, though Standard: In-person interview");
});

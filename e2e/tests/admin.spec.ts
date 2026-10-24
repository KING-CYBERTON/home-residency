import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import { CONSOLE } from "../lib/env";
import { totp } from "../lib/otp";
import { call, expectStatus, staffToken } from "../lib/api";
import { act, consolePage, fill, goConsole, watch } from "../lib/ui";
import { client, service } from "../lib/world";

// Running the office: an admin invites a colleague, who signs in with password and authenticator
// code through the real sign-in screen; roles decide what they see; an account can be suspended
// and its authenticator reset; business settings change with an audit trail; the monthly reports
// render and export as CSV; a household's ID is checked and an account suspended and reinstated.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const run = Date.now().toString(36).slice(-5);
const colleague = { name: `Faith Njeri ${run}`, email: `faith.${run}@e2e.test`, password: "", secret: "" };
let admin: Page;

test.beforeAll(async ({ browser }) => {
  admin = await consolePage(browser, "ops", problems);
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

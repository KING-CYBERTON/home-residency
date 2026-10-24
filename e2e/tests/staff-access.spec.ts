import { expect, test, type Page } from "@playwright/test";
import { CONSOLE } from "../lib/env";
import { totp } from "../lib/otp";
import { call, expectStatus, staffToken } from "../lib/api";
import { act, consolePage, fill, goConsole, watch } from "../lib/ui";

// Managing access to the console: a colleague sees where they are signed in, ends a session they
// don't recognise and changes their own password; an admin signs a colleague out everywhere and
// resets their password; the Roles page says what each role can do.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const run = Date.now().toString(36).slice(-5);
const colleague = { name: `Akinyi Otieno ${run}`, email: `akinyi.${run}@e2e.test`, password: "", secret: "", id: "" };
let admin: Page, page: Page;
let otherToken = "";

test.beforeAll(async ({ browser }) => {
  test.setTimeout(90_000);
  admin = await consolePage(browser, "ops", problems);
  const created = await call(await staffToken("ops"), "POST", "/console/staff", { full_name: colleague.name, email: colleague.email, roles: ["finance"] });
  colleague.id = created.id;
  colleague.password = created.initial_credentials.password;
  colleague.secret = created.initial_credentials.totp_secret;

  // A session somewhere else (here, this test's own API client), signed in before the browser one.
  const login = await call(null, "POST", "/auth/staff/login", { email: colleague.email, password: colleague.password });
  await call(login.token, "POST", "/auth/staff/mfa", { code: totp(colleague.secret) });
  otherToken = login.token;

  // The browser needs the next 30-second code: the API refuses one that was already used.
  await new Promise((r) => setTimeout(r, 30_000 - (Date.now() % 30_000) + 300));
  const ctx = await browser.newContext({ viewport: { width: 1360, height: 900 } });
  page = await ctx.newPage();
  watch(page, "console:colleague", problems);
  await page.goto(CONSOLE + "/login");
  await page.locator("#email").fill(colleague.email);
  await page.locator("#password").fill(colleague.password);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.locator("#code").fill(totp(colleague.secret));
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
});

test.afterAll(() => {
  expect(problems, "script errors or server errors").toEqual([]);
});

test("my account: a colleague sees where they are signed in and signs out the other session", async () => {
  // Their name in the header opens My account.
  await page.getByRole("link", { name: colleague.name }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("My account");
  await expect(page.getByRole("row").filter({ hasText: "(this browser)" })).toHaveCount(1);
  await expect(page.getByRole("row").filter({ has: page.getByRole("button", { name: "Sign out" }) })).toHaveCount(1);

  await act(page, "Sign out", {}, { within: page.getByRole("row").filter({ has: page.getByRole("button", { name: "Sign out" }) }) });
  await expect(page.getByRole("row").filter({ has: page.getByRole("button", { name: "Sign out" }) })).toHaveCount(0);
  await expectStatus(call(otherToken, "GET", "/me"), 401);
});

test("my account: the password change checks the current password and the new one twice", async () => {
  const next = `river stones ${run}`;
  await page.locator("#current-password").fill("not my password");
  await page.locator("#new-password").fill(next);
  await page.locator("#new-password-again").fill(next + "x");
  await page.getByRole("button", { name: "Change password" }).click();
  await expect(page.getByRole("alert")).toHaveText("The new passwords do not match.");

  await page.locator("#new-password-again").fill(next);
  await page.getByRole("button", { name: "Change password" }).click();
  await expect(page.getByRole("alert")).toBeVisible(); // the server refuses the wrong current password
  await expect(page.getByRole("alert")).not.toHaveText("The new passwords do not match.");

  await page.locator("#current-password").fill(colleague.password);
  await page.getByRole("button", { name: "Change password" }).click();
  await expect(page.getByText("Password changed")).toBeVisible();
  await expect(page.locator("#current-password")).toHaveValue("");

  await expectStatus(call(null, "POST", "/auth/staff/login", { email: colleague.email, password: colleague.password }), 401);
  await call(null, "POST", "/auth/staff/login", { email: colleague.email, password: next });
  colleague.password = next;
  // still signed in here
  await page.reload();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("My account");
});

test("roles: every colleague can read what each role is allowed to do", async () => {
  await page.getByRole("link", { name: "Roles" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Roles");
  await expect(page.getByText("Admin holds every role")).toBeVisible();
  const count = page.getByText(/^\d+ of \d+ actions/);
  const all = Number((await count.innerText()).split(" ")[0]);

  await page.getByLabel("Role").selectOption({ label: "Finance" });
  await expect(count).not.toHaveText(new RegExp(`^${all} of`));
  await page.getByLabel("Search").fill("refund");
  const refunds = page.getByRole("row").filter({ hasText: /refund/i });
  await expect(refunds.first()).toBeVisible();
  await expect(refunds.first()).toContainText("Finance");

  // Admin can do everything.
  await page.getByLabel("Search").fill("");
  await page.getByLabel("Role").selectOption({ label: "Admin" });
  await expect(count).toHaveText(`${all} of ${all} actions (admin can do all of them)`);
});

test("staff: an admin signs a colleague out everywhere, then resets their password", async () => {
  await goConsole(admin, "/staff");
  await expect(admin.getByText("Admin holds every role")).toBeVisible();
  await expect(admin.getByText("Admin does not imply other roles")).toHaveCount(0);
  // an admin cannot reset their own password here; they change it on My account
  const self = admin.getByRole("row").filter({ hasText: "(you)" });
  await expect(self.getByRole("button", { name: "Reset password" })).toHaveCount(0);

  const row = admin.getByRole("row").filter({ hasText: colleague.email });
  await row.getByRole("button", { name: "Sessions" }).click();
  const dialog = admin.getByRole("dialog").filter({ hasText: `Where ${colleague.name} is signed in` });
  // the colleague's browser, plus a sign-in the API client above started without the authenticator step
  await expect(dialog.getByRole("row").filter({ hasText: "Checked" })).toHaveCount(1);
  await expect(dialog.getByRole("row").filter({ hasText: "Not yet" })).toHaveCount(1);
  // the confirmation opens over the sessions list, which stays open
  await dialog.getByRole("button", { name: "Sign out everywhere" }).click();
  await fill(admin, { reason: "Left a computer signed in" });
  await admin.getByRole("dialog").last().locator('button[type="submit"]').click();
  await expect(dialog.getByText("Not signed in anywhere.")).toBeVisible();
  await admin.keyboard.press("Escape");
  await expect(dialog).toBeHidden();

  await page.reload();
  await expect(page, "the colleague is signed out").toHaveURL(/\/login/, { timeout: 15_000 });

  // the form closes and the one-time password opens in its place
  await row.getByRole("button", { name: "Reset password" }).click();
  await fill(admin, { reason: "Forgot their password" });
  await admin.getByRole("dialog").locator('button[type="submit"]').click();
  const secret = admin.getByRole("dialog").filter({ hasText: `New password for ${colleague.name}` });
  await expect(secret).toBeVisible();
  const fresh = (await secret.locator("dd").first().innerText()).trim();
  expect(fresh.length).toBeGreaterThanOrEqual(12);
  await secret.getByRole("button", { name: "I have saved these" }).click();

  await expectStatus(call(null, "POST", "/auth/staff/login", { email: colleague.email, password: colleague.password }), 401);
  const login = await call(null, "POST", "/auth/staff/login", { email: colleague.email, password: fresh });
  expect(login.mfa_required).toBe(true);

  // Both actions are on the audit log with their reasons.
  await goConsole(admin, "/audit-log");
  await expect(admin.getByRole("row").filter({ hasText: "staff.reset_password" }).first()).toBeVisible();
  await expect(admin.getByRole("row").filter({ hasText: "staff.sign_out" }).first()).toBeVisible();
});

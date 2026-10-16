import { expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { CONSOLE, SITE, type StaffName } from "./env";
import { staffToken } from "./api";

/**
 * Collects what a person would see as broken: uncaught script errors and any 5xx from our server.
 * Specs call assertClean() at the end; 4xx answers are part of normal flows (wrong codes, rules).
 */
export function watch(page: Page, tag: string, problems: string[]) {
  page.on("pageerror", (e) => {
    // Chromium refuses Background Sync registration in automation; the page falls back to sending itself.
    if (!/Background Sync is disabled/.test(e.message)) problems.push(`[${tag}] script error: ${e.message}`);
  });
  page.on("response", (r) => {
    if (r.status() >= 500 && (r.url().startsWith(SITE) || r.url().startsWith(CONSOLE))) problems.push(`[${tag}] ${r.status()} ${r.request().method()} ${r.url()}`);
  });
}

/** A console tab already signed in as this staff member (the sign-in screen itself is tested in admin.spec). */
export async function consolePage(browser: Browser, who: StaffName, problems: string[]): Promise<Page> {
  const token = await staffToken(who);
  const ctx = await browser.newContext({ viewport: { width: 1360, height: 900 } });
  await ctx.addInitScript((t) => {
    if (!sessionStorage.getItem("hr_console_token")) sessionStorage.setItem("hr_console_token", t);
  }, token);
  const page = await ctx.newPage();
  watch(page, `console:${who}`, problems);
  await page.goto(CONSOLE + "/");
  await expect(page.getByRole("navigation")).toBeVisible();
  return page;
}

/** A phone-sized browser for households and providers. */
export async function phone(browser: Browser, tag: string, problems: string[], extra: Parameters<Browser["newContext"]>[0] = {}): Promise<{ ctx: BrowserContext; page: Page }> {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, ...extra });
  const page = await ctx.newPage();
  watch(page, tag, problems);
  return { ctx, page };
}

export async function goConsole(page: Page, path: string) {
  await page.goto(CONSOLE + path);
  await page.waitForLoadState("networkidle");
}

/** Opens a row in a console table by visible text (rows navigate on click). */
export async function openRow(page: Page, text: string | RegExp) {
  await page.getByRole("row").filter({ hasText: text }).first().click();
  await page.waitForLoadState("networkidle");
}

type Value = string | number | boolean | { label: string } | string[];

/**
 * Fills the open FormDialog. Fields are found by their id (f-<name>); selects take a value or
 * { label }, checkboxes take a boolean, multiselects take the labels to tick.
 */
export async function fill(page: Page, values: Record<string, Value>) {
  const dialog = page.getByRole("dialog").last();
  await expect(dialog).toBeVisible();
  for (const [name, v] of Object.entries(values)) {
    const el = dialog.locator(`#f-${name}`);
    if (Array.isArray(v)) {
      for (const label of v) await dialog.getByLabel(label, { exact: true }).check();
    } else if (typeof v === "boolean") {
      const checked = (await el.getAttribute("data-state")) === "checked";
      if (checked !== v) await el.click();
    } else if ((await el.evaluate((e) => e.tagName)) === "SELECT") {
      await el.selectOption(typeof v === "object" ? { label: v.label } : String(v));
    } else {
      await el.fill(String(v));
    }
  }
}

/** Clicks a button that opens a FormDialog, fills it and submits; expects the dialog to close (success). */
export async function act(page: Page, button: string | RegExp, values: Record<string, Value> = {}, opts: { nth?: number; within?: ReturnType<Page["locator"]> } = {}) {
  const root = opts.within ?? page;
  await root.getByRole("button", { name: button, exact: typeof button === "string" }).nth(opts.nth ?? 0).click();
  await fill(page, values);
  const dialog = page.getByRole("dialog").last();
  await dialog.locator('button[type="submit"]').click();
  await expect(dialog, `dialog "${button}" should close; it says: ${await dialog.innerText().catch(() => "")}`).toBeHidden();
  await page.waitForLoadState("networkidle");
}

/** Same, but the server must refuse: the dialog stays open showing the message. */
export async function actRefused(page: Page, button: string | RegExp, values: Record<string, Value>, message: string | RegExp) {
  await page.getByRole("button", { name: button, exact: typeof button === "string" }).first().click();
  await fill(page, values);
  const dialog = page.getByRole("dialog").last();
  await dialog.locator('button[type="submit"]').click();
  await expect(dialog).toContainText(message);
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
}

/** datetime-local value in Nairobi time, `minutes` from now, on a quarter hour (booking forms step by 15). */
export function nairobi(minutes: number): string {
  const quarter = 15 * 60_000;
  const t = Math.ceil((Date.now() + minutes * 60_000) / quarter) * quarter;
  return new Date(t + 3 * 3_600_000).toISOString().slice(0, 16);
}

/** A phone-sized portal tab (client /account or provider /provider) already signed in as this person. */
export async function portalPage(browser: Browser, tag: string, token: string, problems: string[]): Promise<Page> {
  const { ctx, page } = await phone(browser, tag, problems);
  await ctx.addCookies([{ name: "hr_session", value: token, url: SITE, httpOnly: true, sameSite: "Lax" }]);
  return page;
}

import { expect, test, type Page } from "@playwright/test";
import { call, staffToken } from "../lib/api";
import { staff } from "../lib/env";
import { consolePage, goConsole } from "../lib/ui";
import { one } from "../lib/db";
import { client } from "../lib/world";

// The console usability fixes from QA round 2: people are found by name rather than by pasting an
// id, invoices and payments name the household and link to each other, money reads "KES 999"
// like the website, dates are day first, and the audit log says who did what from where.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const run = Date.now().toString(36).slice(-5);
let ops: Page;
let household: Awaited<ReturnType<typeof client>>;
let invoice: { id: string; reference: string; due_at: Date };

test.beforeAll(async ({ browser }) => {
  ops = await consolePage(browser, "ops", problems);
  household = await client(`Wanjiku Otieno ${run}`);
  const plan = await one<{ id: string }>(`SELECT id FROM subscription_plans WHERE code = 'basic-monthly'`);
  await call(household.token, "POST", "/client/subscription", { plan_id: plan.id, accept_terms: true, pay_phone: household.phone.e164 });
  invoice = await one(`SELECT id, reference, due_at FROM invoices WHERE client_id = $1`, household.userId);
});

test.afterAll(() => {
  expect(problems, "script errors or server errors").toEqual([]);
});

test("invoices: filter by the client's name; the list and the invoice name them", async () => {
  await goConsole(ops, "/invoices");
  await ops.getByRole("combobox", { name: "Client" }).fill(`Wanjiku Otieno ${run}`);
  await ops.getByRole("option", { name: new RegExp(`Wanjiku Otieno ${run}`) }).click();
  await expect(ops).toHaveURL(new RegExp(`client_id=${household.userId}`));
  const row = ops.getByRole("row").filter({ hasText: invoice.reference });
  await expect(row).toContainText(`Wanjiku Otieno ${run}`);
  await expect(row).toContainText("KES 999");
  await expect(row).not.toContainText("Ksh");

  // A link with the id in it still shows the name, and the filter clears.
  await ops.reload();
  await expect(ops.locator("main")).toContainText(`Wanjiku Otieno ${run}`);

  await row.click();
  await expect(ops.getByRole("link", { name: `Wanjiku Otieno ${run}` })).toHaveAttribute("href", `/clients/${household.userId}`);
  const due = new Intl.DateTimeFormat("en-GB", { day: "2-digit", month: "2-digit", year: "numeric", timeZone: "Africa/Nairobi" }).format(invoice.due_at);
  await expect(ops.locator("main")).toContainText(`Due${due}`);
});

test("payments: each one names its invoice and client, with links", async () => {
  await ops.getByRole("link", { name: "See payments" }).click();
  await expect(ops).toHaveURL(new RegExp(`invoice_id=${invoice.id}`));
  const row = ops.getByRole("row").filter({ hasText: invoice.reference });
  await expect(row).toBeVisible();
  await expect(row.getByRole("link", { name: invoice.reference })).toHaveAttribute("href", `/invoices/${invoice.id}`);
  await expect(row.getByRole("link", { name: `Wanjiku Otieno ${run}` })).toHaveAttribute("href", `/clients/${household.userId}`);
});

test("audit log: filter by who did it, by name, and open the record", async () => {
  const token = await staffToken("ops");
  const me = await one<{ id: string; full_name: string }>(`SELECT id, full_name FROM users WHERE email = $1`, staff("ops").email);
  await call(token, "POST", `/console/clients/${household.userId}/suspend`, { reason: `Usability check ${run}` });
  await call(token, "POST", `/console/clients/${household.userId}/reinstate`, { reason: `Usability check ${run}` });

  await goConsole(ops, "/audit-log");
  await ops.getByRole("combobox", { name: "Done by" }).fill(me.full_name);
  await ops.getByRole("option", { name: new RegExp(me.full_name) }).first().click();
  await expect(ops).toHaveURL(new RegExp(`actor_id=${me.id}`));
  const entry = ops.getByRole("row").filter({ hasText: `Usability check ${run}` }).first();
  await expect(entry).toContainText(me.full_name);
  await expect(entry, "the address and program it came from (this test calls the API from Node)").toContainText(/(127\.0\.0\.1|::1)node/);
  await entry.getByRole("button", { name: "History" }).click();
  await expect(ops).toHaveURL(new RegExp(`entity_id=${household.userId}`));
  await expect(ops.getByRole("row").filter({ hasText: `Usability check ${run}` })).toHaveCount(2);
  await ops.getByRole("row").filter({ hasText: `Usability check ${run}` }).first().getByRole("link", { name: "Open" }).click();
  await expect(ops).toHaveURL(new RegExp(`/clients/${household.userId}$`));
});

test("layout: the primary button sits top right even beside a long subtitle", async () => {
  await goConsole(ops, "/notification-templates");
  const title = await ops.getByRole("heading", { level: 1 }).boundingBox();
  const button = await ops.getByRole("button", { name: "New template" }).boundingBox();
  expect(button!.y, "on the title's line, not under it").toBeLessThan(title!.y + title!.height);
  expect(button!.x).toBeGreaterThan(title!.x + 300);
  await expect(ops.getByRole("navigation").getByRole("link", { name: "Ledger", exact: true })).toBeVisible();
});

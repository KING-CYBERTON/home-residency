import { expect, test, type Page } from "@playwright/test";
import { CONSOLE, SITE, newPhone } from "../lib/env";
import { smsCode, waitForNextCode } from "../lib/otp";
import { one, sql } from "../lib/db";
import { b2cResult, latestCheckout, stkCallback } from "../lib/mpesa";
import { act, actRefused, consolePage, goConsole, nairobi, openRow, phone } from "../lib/ui";

// The whole life of the platform through its screens, in order: the catalogue is set up in the
// console, a provider is recruited, vetted by one officer and approved by another, trained, signs
// a contract and is activated; a household books from the public site, triage offers the job, the
// provider checks in with the door code and checks out offline, the household rates and pays by
// M-Pesa, and finance refunds and pays the provider out. Each step is a test, so a failure says
// exactly where the journey breaks.

test.describe.configure({ mode: "serial" });

const problems: string[] = [];
const provider = { phone: newPhone(), name: "Grace Wanjiku" };
const household = { phone: newPhone(), name: "Amina Hassan" };
const pdf = { name: "id.pdf", mimeType: "application/pdf", buffer: Buffer.from("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n") };

let ops: Page, approver: Page, providerPage: Page, clientPage: Page;
let requestRef = "";

test.beforeAll(async ({ browser }) => {
  ops = await consolePage(browser, "ops", problems);
  approver = await consolePage(browser, "approver", problems);
  // The provider is at the client's door (Kilimani) for the location check-in.
  providerPage = (await phone(browser, "provider", problems, { geolocation: { latitude: -1.2898, longitude: 36.7833 }, permissions: ["geolocation"] })).page;
  clientPage = (await phone(browser, "client", problems, { geolocation: { latitude: -1.2898, longitude: 36.7833 }, permissions: ["geolocation"] })).page;
});

test.afterAll(() => {
  expect(problems, "script errors or server errors seen during the journey").toEqual([]);
});

test("catalogue: an admin adds a category, service, price, course and checklist", async () => {
  await goConsole(ops, "/categories");
  await act(ops, "New category", { name: "Gardening", slug: "gardening", engagement: "one_off", vetting_tier: "standard", description: "Garden care", sort_order: 5, is_active: true });
  await expect(ops.getByRole("row").filter({ hasText: "Gardening" })).toBeVisible();

  await goConsole(ops, "/services");
  await act(ops, "New service", { category_id: { label: "Gardening" }, name: "Garden tidy", slug: "garden-tidy", scope_items: "Mowing\nWeeding\nHedges", pricing_unit: "per_job", default_duration_minutes: 180, is_active: true });

  await goConsole(ops, "/rate-card");
  await act(ops, "Publish price", { service_id: { label: "Garden tidy" }, client_price_minor: 3000, provider_pay_minor: 2400, valid_from: nairobi(-60) });
  await expect(ops.getByRole("row").filter({ hasText: "Garden tidy" })).toContainText("600.00"); // commission

  await goConsole(ops, "/training-courses");
  await act(ops, "New course", { code: "IND-GDN", name: "Gardening induction", category_id: { label: "Gardening" }, is_induction: true, pass_mark: 70, is_active: true });

  await goConsole(ops, "/checklists");
  await act(ops, "New version", {
    category_id: { label: "Gardening" },
    items: "document | National ID | yes | national_id\ndocument | Certificate of good conduct | yes | good_conduct_certificate | 12\nreferee | Referees | yes\ninterview | Interview | yes",
  });
  // An admin drafts; only an approver can make it current.
  const draft = ops.locator("main").getByText("Gardening v1");
  await expect(draft).toBeVisible();
  await goConsole(approver, "/checklists");
  await act(approver, "Make current");
  await expect(approver.locator("main")).toContainText("approved by you");
});

test("public site: the new service is listed with its price before VAT", async () => {
  await clientPage.goto(SITE + "/services/gardening");
  await expect(clientPage.locator("main")).toContainText("Garden tidy");
  await expect(clientPage.locator("main")).toContainText("3,000");
});

test("recruitment: a provider signs up by SMS code and applies", async () => {
  const p = providerPage;
  await p.goto(SITE + "/join");
  await p.getByRole("link", { name: /Apply now/ }).first().click();
  await p.getByRole("link", { name: "Apply with your phone number" }).click();
  // Both forms have a name field: wait for the phone form, or the name goes into the email one.
  await p.waitForURL(/with=phone/);
  await p.fill("#full_name", provider.name);
  await p.fill("#phone", provider.phone.local);
  await p.check("input[name=privacy]");
  await p.check("input[name=vetting]");
  await p.getByRole("button", { name: "Send code" }).click();
  await p.locator("input[name=code]").fill(await smsCode(provider.phone.e164));
  await p.getByRole("button", { name: "Create my account" }).click();
  await expect(p.locator("main")).toContainText("What work do you do?");

  await p.getByLabel(/Gardening/).check();
  await p.getByRole("button", { name: "Start application" }).click();
  await expect(p.locator("main")).toContainText("Still needed before you can submit");

  // Submitting before the documents are in is refused with what is missing.
  await p.getByRole("button", { name: "Submit application" }).click();
  await expect(p.locator("main")).toContainText("National ID");
});

test("recruitment: documents go to the vault, referees are added, the application is submitted", async () => {
  const p = providerPage;
  const appUrl = p.url().includes("/applications/") ? p.url() : await (async () => {
    await p.goto(SITE + "/provider/applications");
    await p.locator('a[href^="/provider/applications/"]').filter({ hasText: /Gardening|APP-/ }).first().click();
    return p.url();
  })();

  for (const doc of ["national_id", "good_conduct_certificate"]) {
    await p.goto(appUrl);
    const form = p.locator(`form[data-upload]:has(input[name=document_type][value=${doc}])`);
    if (doc === "good_conduct_certificate") {
      await form.locator("input[name=issued_on]").fill("2026-06-01");
      await form.locator("input[name=expires_on]").fill("2027-06-01");
    }
    await form.locator("input[type=file]").setInputFiles(pdf);
    await form.getByRole("button", { name: "Upload" }).click();
    await expect.poll(async () => (await sql(`SELECT 1 FROM documents d JOIN users u ON u.id = d.owner_user_id WHERE u.phone_e164 = $1 AND d.document_type = $2 AND d.status = 'uploaded'`, provider.phone.e164, doc)).length).toBe(1);
    // The page reloads itself once the vault confirms the upload; the item then shows as submitted.
    await expect(p.locator("li").filter({ has: p.locator(`input[name=document_type][value=${doc}]`) })).toContainText("Submitted");
  }

  for (const [name, tel, rel] of [["Mary Wambui", "0722 111 222", "Former employer"], ["John Kariuki", "0722 333 444", "Neighbour"]]) {
    await p.goto(appUrl);
    await p.locator("summary", { hasText: "Add a referee" }).click();
    await p.fill("#r-name", name);
    await p.fill("#r-phone", tel);
    await p.fill("#r-rel", rel);
    await p.getByRole("button", { name: "Add referee" }).click();
    await expect(p.locator("main")).toContainText(name);
  }

  await p.goto(appUrl);
  await p.getByRole("button", { name: "Submit application" }).click();
  await expect(p.locator("main")).toContainText("Submitted. Our vetting team will check your documents");
  await p.goto(SITE + "/provider/documents");
  await expect(p.locator("main")).toContainText(/National id/i);
});

test("vetting: one officer checks everything and sends it for approval", async () => {
  await goConsole(ops, "/applications");
  await openRow(ops, provider.name);
  await act(ops, "Claim");
  await expect(ops.locator("main")).toContainText("vetting officer you");
  // A document opens in a new tab through a signed link. (Headless Chrome downloads a PDF instead
  // of showing it, so the check is that the tab goes to a link that serves the file.)
  const [file] = await Promise.all([
    ops.context().waitForEvent("request", (r) => r.isNavigationRequest() && r.url().startsWith(SITE)),
    ops.getByRole("button", { name: "View", exact: true }).first().click(),
  ]);
  expect((await fetch(file.url())).status).toBe(200);
  for (const p of ops.context().pages()) if (p !== ops) await p.close();
  // Each document is opened (logged) and verified.
  for (let i = 0; i < 2; i++) await act(ops, "Verify doc", { notes: "Original seen" }, { nth: i });
  for (let i = 0; i < 2; i++) await act(ops, "Record call", { outcome: "positive", statement: "Reliable and honest, worked with us two years." }, { nth: i });
  await act(ops, "Record assessment", { kind: "interview", rubric_version: "v1", scores: '{"communication":4,"plants":5}', total_score: 86, passed: true, notes: "Strong" });
  // Not every item is reviewed yet: completing is refused.
  await actRefused(ops, "Complete vetting", { vetting_report: "Documents verified, referees positive, interview passed." }, /not settled|satisfied or waived/);
  const reviews = await ops.getByRole("button", { name: "Review", exact: true }).count();
  for (let i = 0; i < reviews; i++) await act(ops, "Review", { status: "satisfied", notes: "ok" }, { nth: i });
  await act(ops, "Complete vetting", { vetting_report: "Documents verified, both referees positive, interview passed." });
  await expect(ops.locator("main")).toContainText("Awaiting approval");
  // The vetter cannot also approve.
  const decide = ops.getByRole("button", { name: "Decide", exact: true });
  if (await decide.count()) await expect(decide).toBeDisabled();
  // Every document view was logged in the vault access log.
  const views = await one<{ n: string }>(`SELECT count(*) AS n FROM document_access_log l JOIN documents d ON d.id = l.document_id JOIN users u ON u.id = d.owner_user_id WHERE u.phone_e164 = $1`, provider.phone.e164);
  expect(Number(views.n)).toBeGreaterThanOrEqual(1);
});

test("vetting: a second officer approves", async () => {
  await goConsole(approver, "/applications");
  await openRow(approver, provider.name);
  await act(approver, "Decide", { decision: "approve", reason: "Meets the standard" });
  await expect(approver.locator("main")).toContainText("Approved");
  await providerPage.goto(SITE + "/provider/");
  await expect(providerPage.locator("main")).toContainText("You are approved");
});

test("training: the provider is enrolled in both inductions and passes", async () => {
  // The general induction comes with the database (migration 00015); the Gardening one was added above.
  for (const [label, name] of [
    ["GEN-IND · Homend induction: conduct, safeguarding and the provider app", "Homend induction"],
    ["IND-GDN · Gardening induction", "Gardening induction"],
  ]) {
    await goConsole(ops, "/providers");
    await openRow(ops, provider.name);
    await act(ops, "Enrol in training", { course_id: { label }, scheduled_for: nairobi(60) });
    await goConsole(ops, "/trainings");
    const row = ops.getByRole("row").filter({ hasText: name }).filter({ hasText: provider.name }).filter({ hasText: "Scheduled" });
    await act(ops, "Record result", { status: "passed", score: 88 }, { within: row });
  }
  await providerPage.goto(SITE + "/provider/training");
  await expect(providerPage.locator("main")).toContainText("Passed");
});

test("provider: profile, working hours, time off and contract signed by SMS code", async () => {
  test.setTimeout(180_000);
  const p = providerPage;
  await p.goto(SITE + "/provider/profile");
  await p.fill("#payout", provider.phone.local);
  await p.fill("#years", "6");
  await p.selectOption("#home", { label: "Kilimani" });
  await p.getByLabel("Kilimani", { exact: true }).check();
  await p.fill("#bio", "Gardener with six years in Nairobi homes");
  await p.getByRole("button", { name: "Save", exact: true }).click();
  await expect(p.locator("main")).toContainText(/saved/i);

  await p.goto(SITE + "/provider/availability");
  for (const d of [0, 1, 2, 3, 4, 5, 6]) {
    await p.check(`input[name=on_${d}]`);
    await p.fill(`input[name=start_${d}]`, "00:00");
    await p.fill(`input[name=end_${d}]`, "23:59");
  }
  await p.getByRole("button", { name: "Save hours" }).click();
  await expect(p.locator("main")).toContainText(/saved/i);
  await p.fill("input[name=starts_at]", nairobi(60 * 24 * 20));
  await p.fill("input[name=ends_at]", nairobi(60 * 24 * 22));
  await p.fill("#reason", "Family visit");
  await p.getByRole("button", { name: "Add time off" }).click();
  await expect(p.locator("main")).toContainText("Family visit");

  await p.goto(SITE + "/provider/contract");
  await waitForNextCode(provider.phone.e164);
  await p.getByRole("button", { name: "Send me a code to sign" }).click();
  await p.locator("input[name=code]").fill(await smsCode(provider.phone.e164));
  await p.getByRole("button", { name: "I agree and sign" }).click();
  await expect(p.locator("main")).toContainText("Contract signed");
});

test("activation: the approver activates the provider for Gardening", async () => {
  await goConsole(approver, "/providers");
  await openRow(approver, provider.name);
  await act(approver, "Activate");
  await expect(approver.locator("main")).toContainText("Active");
});

test("booking: a new household follows a Book link, registers, adds an address and books", async () => {
  const c = clientPage;
  await c.goto(SITE + "/services/gardening");
  await c.locator('a[href*="/account/book"]').first().click();
  await expect(c).toHaveURL(/\/account\/login/);
  await c.getByRole("link", { name: "Create an account" }).click();
  await c.getByRole("link", { name: "Use your phone number instead" }).click();
  await c.waitForURL(/with=phone/);
  await c.fill("input[name=full_name]", household.name);
  await c.fill("input[name=phone]", household.phone.local);
  await c.check("input[name=consent]");
  await c.getByRole("button", { name: "Send code" }).click();
  await c.locator("input[name=code]").fill(await smsCode(household.phone.e164));
  await c.getByRole("button", { name: "Create account" }).click();
  await expect(c.locator("main")).toContainText("Add your home address");

  await c.fill("input[name=label]", "Home");
  await c.selectOption("select[name=service_area_id]", { label: "Kilimani, Nairobi" });
  await c.fill("input[name=estate]", "Kilimani Gardens");
  await c.fill("input[name=house_number]", "B4");
  await c.fill("textarea[name=access_details]", "Gate code 4321, guard is Musa");
  await c.locator("[data-geolocate]").click();
  await expect(c.locator("[data-geo]")).toContainText("Location saved");
  await c.getByRole("button", { name: "Save address" }).click();

  // Back on the booking it started from, with a live quote: KES 3,000 + 16% VAT.
  await expect(c).toHaveURL(/\/account\/book/);
  await c.getByLabel(/Garden tidy/).check();
  await expect(c.locator("#quote")).toContainText("3,480");
  await c.fill("input[name=start]", nairobi(60 * 24 * 2));
  await c.fill("textarea[name=notes]", "Front and back garden");
  await c.getByRole("button", { name: "Book now" }).click();
  await expect(c.locator("main")).toContainText("Booked. Your reference is");
  requestRef = (await c.locator("main").innerText()).match(/REQ-\d{4}-\d+/)![0];
});

test("triage: claim, shortlist ranks the provider, offer with a reason", async () => {
  await goConsole(ops, "/requests");
  await openRow(ops, requestRef);
  await act(ops, "Claim");
  await act(ops, "Generate shortlist", { size: 3 });
  const row = ops.getByRole("row").filter({ hasText: provider.name });
  await expect(row).toBeVisible();
  await expect(row).toContainText(/0\.\d km|^0 km| [0-2](\.\d)? km/); // same area: distance from the home area, not a flat default
  await act(ops, "Assign", { selection_reason: "Closest, available, passed induction", offer_expires_in_minutes: 60 }, { within: row });
  await expect(ops.locator("main")).toContainText("Offered");
});

test("offer: the provider sees the job without the gate code, accepts; the code waits for the day", async () => {
  const p = providerPage;
  await p.goto(SITE + "/provider/offers");
  await expect(p.locator("main")).toContainText("Garden tidy");
  await expect(p.locator("main")).not.toContainText("4321");
  await p.getByRole("button", { name: "Accept" }).first().click();
  await expect(p.locator("main")).toContainText("Accepted");
  await p.goto(SITE + "/provider/jobs");
  await p.locator('a[href^="/provider/jobs/"]').first().click();
  await expect(p.locator("main")).toContainText("Garden tidy");
  await expect(p.locator("main")).not.toContainText("4321"); // two days out: released 12 hours before the visit
  // The household sees who is coming, but never the provider's phone number.
  await clientPage.goto(SITE + "/account/requests");
  await clientPage.locator(`a[href*="/account/requests/"]`).first().click();
  await expect(clientPage.locator("main")).toContainText("Grace W.");
  await expect(clientPage.locator("main")).not.toContainText(provider.phone.e164.slice(4));
});

test("visit: triage reschedules to now; door code shows; a wrong code is refused and a retry works", async () => {
  await goConsole(ops, "/requests");
  await openRow(ops, requestRef);
  await act(ops, "Change", { scheduled_start: nairobi(15), scheduled_end: nairobi(195), reason: "Household asked for today" });

  const c = clientPage;
  await c.reload();
  await c.getByRole("button", { name: "Show door code" }).click();
  await expect(c.getByText("DOOR CODE", { exact: false })).toBeVisible();
  const code = ((await c.locator("main").innerText()).match(/\b\d{4}\b/g) ?? []).find((x) => !/^20\d\d$/.test(x))!;

  const p = providerPage;
  await p.goto(SITE + "/provider/jobs");
  await p.locator('a[href^="/provider/jobs/"]').first().click();
  await expect(p.locator("main")).toContainText("Gate code 4321"); // moved to today, so it is released now
  const jobUrl = p.url();
  await p.locator("input[name=code]").fill(code === "0000" ? "1111" : "0000");
  await p.getByRole("button", { name: "Check in" }).click();
  await expect(p.locator("#hr-sync")).toContainText("That door code is wrong");
  await expect(p.locator("[data-offline=check_in]").first()).toBeVisible();
  await p.locator("input[name=code]").fill(code);
  await p.getByRole("button", { name: "Check in" }).click();
  await p.waitForURL(jobUrl);
  await expect(p.locator("main")).toContainText(/Checked in \d/);
});

test("visit: the provider checks out with no signal; it is sent when the phone is back online", async () => {
  const p = providerPage;
  await p.context().setOffline(true);
  await p.locator("textarea[name=notes]").fill("All beds weeded");
  await p.getByRole("button", { name: "Work done, check out" }).click();
  await expect(p.locator("[data-pending]").first()).toContainText("waiting to send");
  await p.context().setOffline(false);
  await expect.poll(async () => (await one<{ s: string }>(`SELECT v.status::text AS s FROM visits v JOIN service_requests r ON r.id = v.request_id WHERE r.reference = $1`, requestRef)).s, { timeout: 30_000 }).toBe("completed");
});

test("rating: the household confirms the work and rates it", async () => {
  const c = clientPage;
  await c.reload();
  await c.getByRole("button", { name: "Confirm the work is done" }).click();
  await c.locator('label:has(input[name=stars][value="5"])').click();
  await c.selectOption("select[name=quality]", "5");
  await c.fill("textarea[name=comment]", "Garden looks great");
  await c.getByRole("button", { name: "Submit rating" }).click();
  // Not just /Thank/: the "Thanks for confirming the visit" flash is already on the page, and moving
  // on before the rating is saved lets the next test's navigation cancel it (no earning is made).
  await expect(c.locator("main")).toContainText("Thank you for rating this job");
});

test("payment: M-Pesa STK; a callback for the wrong amount is refused, the right one pays the invoice", async () => {
  const c = clientPage;
  await c.goto(SITE + "/account/invoices");
  await c.locator('a[href*="/account/invoices/"]').first().click();
  // The list shows the same amount, so wait for the invoice's own URL before reading its id.
  await c.waitForURL(/\/account\/invoices\/[^/?#]+$/);
  await expect(c.locator("main")).toContainText("KES 3,480");
  const invoiceId = c.url().split("/").pop()!;

  await c.getByRole("button", { name: "Send M-Pesa prompt" }).click();
  await expect(c.locator("#pay")).toContainText("Check your phone");
  await stkCallback(await latestCheckout(invoiceId), 1, "E2EWRONG01");
  await expect.poll(async () => (await one<{ s: string }>(`SELECT status::text AS s FROM invoices WHERE id = $1`, invoiceId)).s).not.toBe("paid");

  await c.reload();
  await c.getByRole("button", { name: "Send M-Pesa prompt" }).click();
  await expect(c.locator("#pay")).toContainText("Check your phone");
  await stkCallback(await latestCheckout(invoiceId), 3480, "E2EPAID001");
  await expect(c.locator("main")).toContainText("Paid", { timeout: 20_000 });
});

test("finance: payments, the refused callback on the reconciliation, ledger balances", async () => {
  await goConsole(ops, "/payments");
  await expect(ops.getByRole("row").filter({ hasText: "E2EPAID001" })).toContainText("Succeeded");
  await expect(ops.getByRole("row").filter({ hasText: "E2EWRONG01" })).toContainText("Failed");
  await goConsole(ops, "/reconciliation");
  await expect(ops.locator("main")).toContainText("Platform revenue");
  await expect(ops.locator("main")).not.toContainText("Everything M-Pesa reported is matched"); // the KES 1 callback is listed
});

test("finance: a refund needs a second officer", async () => {
  const pay = await one<{ id: string }>(`SELECT id FROM payments WHERE mpesa_receipt_number = 'E2EPAID001'`);
  await goConsole(ops, "/refunds");
  await act(ops, "Request refund", { payment_id: pay.id, amount_minor: 200, reason: "Late arrival goodwill" });
  const mine = ops.getByRole("row").filter({ hasText: "Late arrival goodwill" });
  await expect(mine.getByRole("button", { name: "Approve" })).toBeDisabled();
  await goConsole(approver, "/refunds");
  await act(approver, "Approve", {}, { within: approver.getByRole("row").filter({ hasText: "Late arrival goodwill" }) });
  const r = await one<{ c: string }>(`SELECT mpesa_conversation_id AS c FROM refunds WHERE reason = 'Late arrival goodwill'`);
  await b2cResult(r.c, 200);
  await expect.poll(async () => (await one<{ s: string }>(`SELECT status::text AS s FROM refunds WHERE reason = 'Late arrival goodwill'`)).s).toBe("completed");
});

test("finance: earnings are paid out in a batch prepared and approved by different officers", async () => {
  await goConsole(ops, "/earnings");
  await expect(ops.getByRole("row").filter({ hasText: requestRef })).toContainText("Payable");
  await goConsole(ops, "/payout-batches");
  const today = nairobi(0).slice(0, 10);
  await act(ops, "Prepare batch", { cycle_start: "2026-01-01", cycle_end: today });
  await expect(ops.locator("main")).toContainText("Draft");
  const batchUrl = ops.url();
  await approver.goto(batchUrl.replace(/^https?:\/\/[^/]+/, CONSOLE));
  await approver.waitForLoadState("networkidle");
  await approver.getByRole("button", { name: "Approve and send" }).click();
  const dialog = approver.getByRole("dialog");
  if (await dialog.isVisible().catch(() => false)) await dialog.locator('button[type="submit"]').click();
  await expect(approver.locator("main")).toContainText("Processing");
  const payout = await one<{ c: string; amount: string }>(`SELECT p.mpesa_conversation_id AS c, p.amount_minor AS amount FROM payouts p JOIN users u ON u.id = p.provider_id WHERE u.phone_e164 = $1`, provider.phone.e164);
  await b2cResult(payout.c, Number(payout.amount) / 100);
  await providerPage.goto(SITE + "/provider/pay");
  await expect(providerPage.locator("main")).toContainText("2,400");
  await expect.poll(async () => (await one<{ s: string }>(`SELECT status::text AS s FROM payouts WHERE mpesa_conversation_id = $1`, payout.c)).s).toBe("paid");
});

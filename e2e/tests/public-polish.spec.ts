import { expect, test } from "@playwright/test";
import { SITE } from "../lib/env";

// The public site polish from QA round 2: one header style per kind of page, a worker photograph
// on /join with its own description, nav labels that match the page they open, and a sign-in
// page that asks whether you are a household or a provider.

const problems: string[] = [];

test.afterAll(() => {
  expect(problems, "script errors or server errors").toEqual([]);
});

test.beforeEach(({ page }) => {
  page.on("pageerror", (e) => problems.push(`script error: ${e.message}`));
  page.on("response", (r) => {
    if (r.status() >= 500) problems.push(`${r.status()} ${r.url()}`);
  });
});

test("section pages share one header: title and lead beside a photograph", async ({ page }) => {
  for (const [path, title] of [
    ["/services", "Home services in Nairobi"],
    ["/areas", "Areas we serve"],
    ["/pricing", "Prices"],
    ["/faq", "Questions and answers"],
    ["/how-it-works", "How it works"],
    ["/contact", "Contact us"],
  ]) {
    await test.step(path, async () => {
      await page.goto(SITE + path);
      const hero = page.locator("main section").first();
      await expect(hero.getByRole("heading", { level: 1 })).toHaveText(title);
      await expect(hero.locator("picture img"), `${path} has a header photograph`).toHaveCount(1);
      await expect(page.locator("h1")).toHaveCount(1);
    });
  }
});

test("/join: a photograph of a worker, and a description for search and sharing", async ({ page }) => {
  await page.goto(SITE + "/join");
  await expect(page.locator("main section").first().locator("img")).toHaveAttribute("src", /deep-clean/);
  await expect(page.locator('meta[name="description"]')).toHaveAttribute("content", /work in Nairobi homes/);
  await expect(page.locator('meta[property="og:image"]')).toHaveAttribute("content", /deep-clean/);
});

test("labels match the page they open", async ({ page }) => {
  await page.goto(SITE + "/");
  await page.getByRole("navigation", { name: "Main" }).first().getByRole("link", { name: "Prices", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Prices");
  await expect(page).toHaveTitle(/^Prices/);
  await page.getByRole("contentinfo").getByRole("link", { name: "Questions and answers" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Questions and answers");
  await expect(page).toHaveTitle(/^Questions and answers/);
});

test("sign in: households and providers choose their own portal", async ({ page }) => {
  await page.goto(SITE + "/services");
  await page.getByRole("banner").getByRole("link", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/sign-in$/);
  await expect(page.getByRole("main").getByRole("link", { name: /Client sign in/ })).toHaveAttribute("href", "/account");
  await expect(page.getByRole("main").getByRole("link", { name: /Provider sign in/ })).toHaveAttribute("href", "/provider");
  await page.getByRole("main").getByRole("link", { name: /Provider sign in/ }).click();
  await expect(page).toHaveURL(/\/provider\/login/);
});

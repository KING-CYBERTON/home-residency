import { defineConfig, devices } from "@playwright/test";

// The smoke test the deploy workflow runs against a live environment (staging or production)
// right after a rollout; see deployed/README.md. It only reads: it signs nobody in, writes
// nothing and needs no database, so it is safe on production.
export default defineConfig({
  testDir: "./deployed",
  outputDir: "./test-results-deployed",
  fullyParallel: true,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  retries: 2, // a fresh rollout can drop the odd connection while old tasks drain
  reporter: process.env.CI
    ? [["list"], ["html", { open: "never", outputFolder: "playwright-report-deployed" }], ["github"]]
    : [["list"]],
  use: {
    timezoneId: "Africa/Nairobi",
    locale: "en-KE",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});

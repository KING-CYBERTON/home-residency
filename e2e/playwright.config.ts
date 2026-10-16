import { defineConfig, devices } from "@playwright/test";

// The stack (Go API with the public site and portals, the built console, Postgres) is started by
// scripts/stack.sh before the tests run; see README.md. Every spec shares one database, so they run
// one at a time, and each spec creates the people and data it needs.
export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  workers: 1,
  timeout: 120_000,
  expect: { timeout: 10_000 },
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }], ["github"]] : [["list"], ["html", { open: "never" }]],
  use: {
    timezoneId: "Africa/Nairobi",
    locale: "en-KE",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    actionTimeout: 15_000,
  },
  projects: [
    // Staff use the console on a laptop; households and providers use the site on a phone.
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
  ],
});

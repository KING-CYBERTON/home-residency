# Smoke test of a deployed environment

`smoke.spec.ts` is what `.github/workflows/backend-deploy-ecs.yml` runs against staging and production right after each rollout. It only reads: no sign-in, no writes, no database, so it is safe on production.

- `@site`: `/api/v1/healthz` is ok and reports the deployed version, every public page renders with a Content-Security-Policy header, the household and provider sign-in screens ask for a phone number, and unknown API paths get the JSON 404. If this fails, the workflow puts the API back on the task definition it ran before.
- `@console`: the console's sign-in screen loads with CloudFront's CSP and HSTS headers, and `/api` is reachable through CloudFront.

Every page fails on a script error, a Content-Security-Policy violation or a 5xx response.

Run it yourself against any environment:

```sh
cd e2e
E2E_SITE_URL=https://api.staging.example.co.ke E2E_CONSOLE_URL=https://console.staging.example.co.ke \
  npx playwright test -c playwright.deployed.config.ts
```

Add `E2E_EXPECT_VERSION=<commit SHA>` to also check the version.

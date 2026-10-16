# Home Residency (Homend Residence)

Home management platform for Kenyan households: vetted domestic workers and home service providers, placed and supervised by management.

- `docs/` architecture decision records (start with `ADR-0001-platform-tech-stack.md`)
- `backend/` Go service, PostgreSQL migrations (goose) and the OpenAPI contract. See [backend/README.md](backend/README.md)
- `console/` staff management console: React, Refine, Tailwind and shadcn/ui on the JSON API, built as static files. See [console/README.md](console/README.md)
- `mobile/` Flutter apps for households (client) and service providers, sharing one package for the API client, sign-in and models. See [mobile/README.md](mobile/README.md)
- `.github/workflows/console-ci.yml` generated API types are current, lint, typecheck, build
- `.github/workflows/mobile-ci.yml` format, analyze, tests including the OpenAPI contract check, Android debug builds
- `.github/workflows/mobile-release.yml` on merges to main that change `mobile/`, uploads signed builds of both apps to the Google Play internal track
- `.github/workflows/backend-ci.yml` build, vet, tests against Postgres, generated-code check, OpenAPI lint, migration round trip, vulnerability scan, image build

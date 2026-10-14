# Home Residency (Homend Residence)

Home management platform for Kenyan households: vetted domestic workers and home service providers, placed and supervised by management.

- `docs/` architecture decision records (start with `ADR-0001-platform-tech-stack.md`)
- `backend/` Go service, PostgreSQL migrations (goose) and the OpenAPI contract. See [backend/README.md](backend/README.md)
- `.github/workflows/backend-ci.yml` build, vet, tests against Postgres, generated-code check, OpenAPI lint, migration round trip, vulnerability scan, image build

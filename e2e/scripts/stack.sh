#!/usr/bin/env bash
# Starts the whole platform for the end-to-end tests, on a fresh database:
#   - Postgres must already be running (CI service container, or `make db-up` in backend/)
#   - the Go API with the public site, client portal, provider portal and recruitment pages
#     (:8080), SMS codes written to the log, the in-memory M-Pesa fake
#   - the console, built for production and served by `vite preview` (:4173), /api proxied to :8080
#   - the demo catalogue (`api seed-demo`) and three staff accounts whose credentials go to
#     .stack/staff.json (two officers are needed for every two-person rule)
# Stop it with: bash scripts/stack.sh stop
set -euo pipefail

E2E="$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(cd "$E2E/.." && pwd)"
STACK="$E2E/.stack"
PG_URL="${E2E_PG_URL:-postgres://postgres:postgres@localhost:5432}"
DB="${E2E_DB:-homend_e2e}"

stop() {
  for f in "$STACK"/*.pid; do
    [ -f "$f" ] || continue
    kill "$(cat "$f")" 2>/dev/null || true
    rm -f "$f"
  done
}

if [ "${1:-}" = "stop" ]; then stop; exit 0; fi
stop
rm -rf "$STACK" && mkdir -p "$STACK"

echo "== fresh database $DB"
psql "$PG_URL/postgres" -v ON_ERROR_STOP=1 -q -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" -c "CREATE DATABASE $DB"
export DATABASE_URL="$PG_URL/$DB?sslmode=disable"

echo "== build the API"
(cd "$ROOT/backend" && go build -o "$STACK/api" ./cmd/api)
API="$STACK/api"
export APP_ENV=dev SMS_PROVIDER=log HTTP_ADDR=:8080
"$API" migrate up >/dev/null
"$API" seed-demo >/dev/null

echo "== staff accounts"
staff() { # name roles
  local out
  out="$("$API" create-staff -email "$1@e2e.test" -name "E2E $1" -roles "$2" 2>/dev/null)"
  printf '"%s":{"email":"%s@e2e.test","password":"%s","totp":"%s"}' "$1" "$1" \
    "$(sed -n 's/^Password: *//p' <<<"$out")" "$(sed -n 's/^TOTP secret: *//p' <<<"$out")"
}
{
  echo "{"
  staff ops "admin,triage_officer,vetting_officer,training_coordinator,finance,compliance,supervisor,reporting"; echo ","
  staff approver "approver,finance,complaints_manager,training_coordinator"; echo ","
  staff supervisor2 "supervisor,complaints_manager"
  echo "}"
} >"$STACK/staff.json"

echo "== start the API"
setsid nohup "$API" serve </dev/null >"$STACK/api.log" 2>&1 &
echo $! >"$STACK/api.pid"

echo "== build and serve the console"
(cd "$ROOT/console" && npm run build >/dev/null)
(cd "$ROOT/console" && setsid nohup npx vite preview --port 4173 --strictPort </dev/null >"$STACK/console.log" 2>&1 & echo $! >"$STACK/console.pid")

for url in http://localhost:8080/api/v1/healthz http://localhost:4173/login; do
  for _ in $(seq 1 60); do curl -fs -o /dev/null "$url" && break; sleep 1; done
  curl -fs -o /dev/null "$url" || { echo "not up: $url"; tail -50 "$STACK"/*.log; exit 1; }
done
echo "== stack is up: site http://localhost:8080, console http://localhost:4173"

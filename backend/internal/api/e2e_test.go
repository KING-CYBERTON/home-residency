package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"

	apispec "github.com/KING-CYBERTON/home-residency/backend/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/config"
	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	"github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/mpesa"
	"github.com/KING-CYBERTON/home-residency/backend/internal/sms"
)

// These tests need a real PostgreSQL. Set TEST_DATABASE_URL to an empty scratch database; the tests
// reset it. They skip when it is unset so `go test ./...` still works on a laptop without Postgres.
func testDSN(t *testing.T) string {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	app    *api.Server
	mpesa  *mpesa.Fake
	pool   *pgxpool.Pool
	sms    *sms.Capture
	router routers.Router
	tokens map[string]string
}

func newHarness(t *testing.T) *harness {
	dsn := testDSN(t)
	ctx := context.Background()
	scratch, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scratch.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal("clean schema:", err)
	}
	scratch.Close()
	if err := db.Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal("up:", err)
	}
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cfg := config.Config{Env: "test", FieldKey: "ZGV2LW9ubHkta2V5LWRvLW5vdC11c2UtaW4tcHJvZCE="}
	cipher, err := crypt.NewAESGCM(cfg.FieldKey)
	if err != nil {
		t.Fatal(err)
	}
	capture := &sms.Capture{}
	fake := &mpesa.Fake{}
	app := api.NewServer(pool, cfg, capture, cipher, fake)
	h, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	doc, err := openapi3.NewLoader().LoadFromData(apispec.YAML)
	if err != nil {
		t.Fatal(err)
	}
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, srv: srv, app: app, mpesa: fake, pool: pool, sms: capture, router: router, tokens: map[string]string{}}
}

// call performs a request and validates the response against openapi.yaml, so the implementation cannot drift from the contract.
func (h *harness) call(who, method, path string, body any, headers ...string) (int, []byte) {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+api.BaseURL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := h.tokens[who]; tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	// Validate against the contract (skip 501: unbuilt endpoints answer with the error envelope, which the spec lists as an error).
	specReq, _ := http.NewRequest(method, "https://api.homendresidence.co.ke"+api.BaseURL+path, nil)
	route, pathParams, err := h.router.FindRoute(specReq)
	if err != nil {
		h.t.Fatalf("%s %s is not in openapi.yaml: %v", method, path, err)
	}
	if resp.StatusCode != http.StatusNotImplemented {
		in := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: &openapi3filter.RequestValidationInput{Request: specReq, PathParams: pathParams, Route: route},
			Status:                 resp.StatusCode,
			Header:                 resp.Header,
			Options:                &openapi3filter.Options{IncludeResponseStatus: true},
		}
		in.SetBodyBytes(raw)
		if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
			h.t.Errorf("%s %s -> %d violates the OpenAPI contract: %v\nbody: %s", method, path, resp.StatusCode, err, raw)
		}
	}
	return resp.StatusCode, raw
}

func (h *harness) expect(want int, who, method, path string, body any, headers ...string) map[string]any {
	h.t.Helper()
	code, raw := h.call(who, method, path, body, headers...)
	if code != want {
		h.t.Fatalf("%s %s: got %d, want %d: %s", method, path, code, want, raw)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func (h *harness) expectList(want int, who, method, path string) []any {
	h.t.Helper()
	code, raw := h.call(who, method, path, nil)
	if code != want {
		h.t.Fatalf("%s %s: got %d, want %d: %s", method, path, code, want, raw)
	}
	var out []any
	_ = json.Unmarshal(raw, &out)
	return out
}

var codeRE = regexp.MustCompile(`\b(\d{6})\b`)

func (h *harness) register(who, phone, name, kind string) string {
	h.t.Helper()
	res := h.expect(200, "", "POST", "/auth/otp/request", map[string]any{"phone": phone, "purpose": "register"})
	m := codeRE.FindStringSubmatch(h.sms.Last().Body)
	if m == nil || h.sms.Last().To != phone {
		h.t.Fatalf("no code sent to %s: %+v", phone, h.sms.Last())
	}
	// a wrong code is rejected and counts as an attempt
	h.expect(400, "", "POST", "/auth/otp/verify", map[string]any{"otp_id": res["otp_id"], "code": "000000", "full_name": name, "kind": kind})
	s := h.expect(200, "", "POST", "/auth/otp/verify", map[string]any{
		"otp_id": res["otp_id"], "code": m[1], "full_name": name, "kind": kind,
		"consents": []any{map[string]any{"purpose": "privacy_notice", "notice_version": "2026-10", "granted": true}},
	})
	h.tokens[who] = s["token"].(string)
	return s["user"].(map[string]any)["id"].(string)
}

func (h *harness) staff(who, email string, roles ...string) {
	h.t.Helper()
	pw, secret, _, err := h.app.CreateStaff(context.Background(), who, email, roles)
	if err != nil {
		h.t.Fatal(err)
	}
	s := h.expect(200, "", "POST", "/auth/staff/login", map[string]any{"email": email, "password": pw})
	if s["mfa_required"] != true {
		h.t.Fatal("expected mfa_required")
	}
	h.tokens[who] = s["token"].(string)
	// Before the authenticator check a staff session can do nothing else.
	h.expect(403, who, "GET", "/console/dashboard", nil)
	h.expect(401, "", "POST", "/auth/staff/mfa", map[string]any{"code": "123456"})
	h.expect(401, who, "POST", "/auth/staff/mfa", map[string]any{"code": "000000"})
	code, _ := totp.GenerateCode(secret, time.Now())
	h.expect(200, who, "POST", "/auth/staff/mfa", map[string]any{"code": code})
}

func (h *harness) sql(q string, args ...any) {
	h.t.Helper()
	if _, err := h.pool.Exec(context.Background(), q, args...); err != nil {
		h.t.Fatalf("%s: %v", q, err)
	}
}

func TestEndToEnd(t *testing.T) {
	h := newHarness(t)

	// ---- catalogue (seeded by SQL; admin screens come later)
	var areaID, catID, svcID string
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(h.pool.QueryRow(ctx, `INSERT INTO service_areas (slug, name, is_active, centre_lat, centre_lng, radius_km) VALUES ('kilimani','Kilimani',true,-1.2921,36.7872,3) RETURNING id`).Scan(&areaID))
	must(h.pool.QueryRow(ctx, `INSERT INTO service_categories (slug, name, engagement, is_active) VALUES ('cleaning','Cleaning','one_off',true) RETURNING id`).Scan(&catID))
	must(h.pool.QueryRow(ctx, `INSERT INTO services (category_id, slug, name, pricing_unit, scope_items, is_active, default_duration_minutes)
		VALUES ($1,'standard-clean','Standard clean','per_job','["Kitchen","Bathrooms"]',true,180) RETURNING id`, catID).Scan(&svcID))
	h.sql(`INSERT INTO rate_card_entries (service_id, client_price_minor, provider_pay_minor, valid_from) VALUES ($1, 250000, 200000, now() - interval '1 day')`, svcID)
	h.sql(`INSERT INTO faqs (question, answer) VALUES ('How are providers vetted?','Documents, referees and an assessment.')`)

	t.Run("public catalogue", func(t *testing.T) {
		if a := h.expectList(200, "", "GET", "/public/service-areas"); len(a) != 1 {
			t.Fatalf("areas: %v", a)
		}
		cats := h.expectList(200, "", "GET", "/public/categories")
		if len(cats) != 1 {
			t.Fatalf("categories: %v", cats)
		}
		h.expect(200, "", "GET", "/public/categories/cleaning", nil)
		h.expect(404, "", "GET", "/public/categories/nope", nil)
		rc := h.expectList(200, "", "GET", "/public/rate-card?service_area_id="+areaID)
		price := rc[0].(map[string]any)["current_price"].(map[string]any)["client_price_minor"].(float64)
		if price != 250000 {
			t.Fatalf("price %v", price)
		}
		h.expectList(200, "", "GET", "/public/faqs")
		h.expect(201, "", "POST", "/public/leads", map[string]any{"full_name": "Amina Odhiambo", "phone": "+254700111222"})
		h.expect(422, "", "POST", "/public/leads", map[string]any{"full_name": "No contact"})
	})

	clientID := h.register("client", "+254700000001", "Wanjiru Kamau", "client")
	providerID := h.register("provider", "+254700000002", "Peter Otieno", "provider")
	_ = providerID
	h.staff("triage", "triage@homend.test", "triage_officer")
	h.staff("admin", "admin@homend.test", "admin")

	// An approved, trained provider is activated by vetting; here we set that state directly.
	h.sql(`UPDATE providers SET status = 'active', max_concurrent_jobs = 2, home_lat = -1.2900, home_lng = 36.7900, activated_at = now() WHERE user_id = $1`, providerID)
	h.sql(`INSERT INTO provider_categories (provider_id, category_id, status) VALUES ($1,$2,'active')`, providerID, catID)

	t.Run("auth and access rules", func(t *testing.T) {
		me := h.expect(200, "client", "GET", "/me", nil)
		if me["kind"] != "client" || me["id"] != clientID {
			t.Fatalf("me: %v", me)
		}
		h.expect(200, "client", "PATCH", "/me", map[string]any{"preferred_language": "sw"})
		h.expect(401, "", "GET", "/me", nil)
		h.expect(401, "", "GET", "/client/addresses", nil)
		h.expect(403, "client", "GET", "/console/requests", nil)
		h.expect(403, "provider", "GET", "/client/addresses", nil)
		h.expect(403, "triage", "GET", "/client/addresses", nil)
		h.expect(403, "admin", "GET", "/console/requests", nil)      // admin is not implied for triage
		h.expect(403, "triage", "GET", "/console/applications", nil) // not a vetting role
		h.expect(501, "admin", "GET", "/console/applications", nil)  // allowed role, endpoint specified but not built
		h.expect(501, "admin", "GET", "/console/staff", nil)
		// rate limit: a second code to the same phone within a minute
		h.expect(200, "", "POST", "/auth/otp/request", map[string]any{"phone": "+254700000009", "purpose": "register"})
		h.expect(429, "", "POST", "/auth/otp/request", map[string]any{"phone": "+254700000009", "purpose": "register"})
		h.expect(422, "", "POST", "/auth/otp/request", map[string]any{"phone": "0700000000", "purpose": "login"})
	})

	var addrID, reqID, assignID string
	start := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)

	t.Run("client books", func(t *testing.T) {
		a := h.expect(201, "client", "POST", "/client/addresses", map[string]any{
			"label": "Home", "service_area_id": areaID, "estate": "Kilimani", "lat": -1.2921, "lng": 36.7872, "access_details": "Gate code 4471, guard is Joseph",
		})
		addrID = a["id"].(string)
		if a["is_default"] != true || a["access_details"] != "Gate code 4471, guard is Joseph" {
			t.Fatalf("address: %v", a)
		}
		var enc []byte
		must(h.pool.QueryRow(ctx, `SELECT access_details_enc FROM addresses WHERE id = $1`, addrID).Scan(&enc))
		if bytes.Contains(enc, []byte("4471")) {
			t.Fatal("access details are stored in plain text")
		}
		if l := h.expectList(200, "client", "GET", "/client/addresses"); len(l) != 1 {
			t.Fatalf("addresses: %v", l)
		}
		q := h.expect(200, "client", "POST", "/client/quotes", map[string]any{"service_id": svcID, "address_id": addrID})
		if q["subtotal_minor"] != 250000.0 || q["vat_minor"] != 40000.0 || q["total_minor"] != 290000.0 || q["service_area_supported"] != true {
			t.Fatalf("quote: %v", q)
		}
		h.expect(422, "client", "POST", "/client/quotes", map[string]any{"service_id": svcID, "address_id": addrID, "quantity": 0})

		body := map[string]any{"service_id": svcID, "address_id": addrID, "requested_start": start.Format(time.RFC3339), "client_notes": "Please bring own mop"}
		r := h.expect(201, "client", "POST", "/client/requests", body, "Idempotency-Key", "book-1")
		reqID = r["id"].(string)
		if r["status"] != "submitted" || r["total_minor"] != 290000.0 || !strings.HasPrefix(r["reference"].(string), "REQ-") {
			t.Fatalf("request: %v", r)
		}
		// the same key replays the stored response instead of creating a second request
		again := h.expect(201, "client", "POST", "/client/requests", body, "Idempotency-Key", "book-1")
		if again["id"] != reqID {
			t.Fatal("idempotent replay created a new request")
		}
		body["client_notes"] = "different"
		h.expect(422, "client", "POST", "/client/requests", body, "Idempotency-Key", "book-1")
		h.expect(422, "client", "POST", "/client/requests", map[string]any{"service_id": svcID, "address_id": addrID, "requested_start": "2020-01-01T10:00:00Z"})
		code, raw := h.call("client", "GET", "/client/requests?status=submitted", nil)
		if code != 200 || !bytes.Contains(raw, []byte(reqID)) {
			t.Fatalf("list: %d %s", code, raw)
		}
		// another client cannot see it
		h.register("other", "+254700000003", "Other Client", "client")
		h.expect(404, "other", "GET", "/client/requests/"+reqID, nil)
	})

	t.Run("triage and assignment", func(t *testing.T) {
		q := h.expect(200, "triage", "GET", "/console/requests?status=submitted", nil)
		if len(q["data"].([]any)) != 1 {
			t.Fatalf("queue: %v", q)
		}
		c := h.expect(200, "triage", "POST", "/console/requests/"+reqID+"/claim", nil)
		if c["status"] != "triaging" {
			t.Fatalf("claim: %v", c)
		}
		sl := h.expectList(200, "triage", "POST", "/console/requests/"+reqID+"/shortlist")
		if len(sl) != 1 || sl[0].(map[string]any)["rank"] != 1.0 {
			t.Fatalf("shortlist: %v", sl)
		}
		// rule: only active, in-date, unflagged providers can be assigned. Peter's mandatory document is expired.
		var tpl string
		must(h.pool.QueryRow(ctx, `INSERT INTO checklist_templates (category_id, version, is_current) VALUES ($1,1,true) RETURNING id`, catID).Scan(&tpl))
		h.sql(`INSERT INTO checklist_template_items (template_id, kind, document_type, label) VALUES ($1,'document','good_conduct_certificate','Certificate of Good Conduct')`, tpl)
		h.sql(`INSERT INTO documents (owner_user_id, document_type, status, s3_key, mime_type, expires_on) VALUES ($1,'good_conduct_certificate','verified','k/1','application/pdf', current_date - 1)`, providerID)
		bad := h.expect(422, "triage", "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": providerID, "selection_reason": "Closest provider"})
		if bad["code"] != "rule_violation" {
			t.Fatalf("expected rule_violation: %v", bad)
		}
		if l := h.expectList(200, "triage", "POST", "/console/requests/"+reqID+"/shortlist"); len(l) != 0 {
			t.Fatalf("expired-document provider must not be shortlisted: %v", l)
		}
		h.sql(`UPDATE documents SET expires_on = current_date + 200 WHERE owner_user_id = $1`, providerID)
		h.expect(422, "triage", "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": providerID, "selection_reason": ""})

		a := h.expect(201, "triage", "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": providerID, "selection_reason": "Closest provider, 4.8 average"}, "Idempotency-Key", "assign-1")
		assignID = a["id"].(string)
		if a["status"] != "offered" {
			t.Fatalf("assignment: %v", a)
		}
		h.expect(409, "triage", "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": providerID, "selection_reason": "again"})
		r := h.expect(200, "client", "GET", "/client/requests/"+reqID, nil)
		if r["status"] != "awaiting_provider" || r["provider"] != nil {
			t.Fatalf("client view before acceptance must not show a provider: %v", r)
		}
		dash := h.expect(200, "triage", "GET", "/console/dashboard", nil)
		if dash["awaiting_provider"] != 1.0 {
			t.Fatalf("dashboard: %v", dash)
		}
	})

	var visitID string
	t.Run("provider accepts and works the job", func(t *testing.T) {
		offers := h.expectList(200, "provider", "GET", "/provider/offers")
		if len(offers) != 1 {
			t.Fatalf("offers: %v", offers)
		}
		job := h.expect(200, "provider", "POST", "/provider/offers/"+assignID+"/accept", nil, "Idempotency-Key", "acc-1")
		if job["client_display_name"] != "Wanjiru K." {
			t.Fatalf("job: %v", job)
		}
		if job["address"].(map[string]any)["access_details"] == nil {
			t.Fatal("the visit is within 12 hours, so the gate details should be released")
		}
		h.expect(409, "provider", "POST", "/provider/offers/"+assignID+"/accept", nil)
		visits := h.expectList(200, "provider", "GET", "/provider/visits")
		visitID = visits[0].(map[string]any)["id"].(string)

		r := h.expect(200, "client", "GET", "/client/requests/"+reqID, nil)
		prov := r["provider"].(map[string]any)
		if r["status"] != "confirmed" || prov["display_name"] != "Peter O." {
			t.Fatalf("confirmed view: %v", r)
		}
		if _, leaked := prov["phone"]; leaked {
			t.Fatal("client view leaks the provider's phone number")
		}

		// door code check-in: wrong codes are counted, the right one works, a retried event is a no-op
		code := h.expect(200, "client", "GET", "/client/visits/"+visitID+"/checkin-code", nil)["code"].(string)
		wrong := "0000"
		if code == "0000" {
			wrong = "1111"
		}
		now := time.Now().UTC().Format(time.RFC3339)
		ev1 := "6f1c2c6e-6a3b-4c1e-9d6e-0a1b2c3d4e51"
		h.expect(422, "provider", "POST", "/provider/visits/"+visitID+"/check-in", map[string]any{"client_event_id": ev1, "method": "code", "code": wrong, "device_recorded_at": now})
		v := h.expect(200, "provider", "POST", "/provider/visits/"+visitID+"/check-in", map[string]any{"client_event_id": ev1, "method": "code", "code": code, "device_recorded_at": now})
		if v["status"] != "checked_in" {
			t.Fatalf("check-in: %v", v)
		}
		h.expect(200, "provider", "POST", "/provider/visits/"+visitID+"/check-in", map[string]any{"client_event_id": ev1, "method": "code", "code": code, "device_recorded_at": now})
		h.expect(409, "provider", "POST", "/provider/visits/"+visitID+"/check-in", map[string]any{"client_event_id": "6f1c2c6e-6a3b-4c1e-9d6e-0a1b2c3d4e52", "method": "code", "code": code, "device_recorded_at": now})

		ev2 := "6f1c2c6e-6a3b-4c1e-9d6e-0a1b2c3d4e53"
		h.expect(200, "provider", "POST", "/provider/visits/"+visitID+"/check-out", map[string]any{"client_event_id": ev2, "notes": "All done", "device_recorded_at": time.Now().UTC().Format(time.RFC3339)})
		h.expect(200, "provider", "POST", "/provider/visits/"+visitID+"/check-out", map[string]any{"client_event_id": ev2, "device_recorded_at": time.Now().UTC().Format(time.RFC3339)})
		r = h.expect(200, "client", "GET", "/client/requests/"+reqID, nil)
		if r["status"] != "awaiting_rating" || r["rating_required"] != true {
			t.Fatalf("after check-out: %v", r)
		}
	})

	t.Run("client confirms and rates", func(t *testing.T) {
		h.expect(200, "client", "POST", "/client/visits/"+visitID+"/confirm", nil)
		h.expect(422, "client", "POST", "/client/requests/"+reqID+"/rating", map[string]any{"stars": 9})
		rt := h.expect(201, "client", "POST", "/client/requests/"+reqID+"/rating", map[string]any{"stars": 2, "comment": "Left early", "sub_scores": map[string]any{"punctuality": 2}})
		if rt["complaint_id"] == nil {
			t.Fatalf("a 2-star rating must open a complaint: %v", rt)
		}
		h.expect(409, "client", "POST", "/client/requests/"+reqID+"/rating", map[string]any{"stars": 5})
		r := h.expect(200, "client", "GET", "/client/requests/"+reqID, nil)
		if r["status"] != "closed" {
			t.Fatalf("closed: %v", r)
		}
		var status string
		var avg float64
		var jobs int
		must(h.pool.QueryRow(ctx, `SELECT status::text, COALESCE(rating_avg,0), completed_jobs FROM providers WHERE user_id = $1`, providerID).Scan(&status, &avg, &jobs))
		if avg != 2 || jobs != 1 {
			t.Fatalf("provider standing: avg %v jobs %d", avg, jobs)
		}
		var held string
		must(h.pool.QueryRow(ctx, `SELECT status::text FROM provider_earnings WHERE provider_id = $1`, providerID).Scan(&held))
		if held != "held" {
			t.Fatalf("earning should be held while the complaint is open, got %s", held)
		}
		var audits int
		must(h.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action IN ('request.submit','assignment.create','assignment.accept','visit.check_in','request.rate')`).Scan(&audits))
		if audits != 5 {
			t.Fatalf("audit entries: %d", audits)
		}
		if _, err := h.pool.Exec(ctx, `DELETE FROM audit_log`); err == nil {
			t.Fatal("audit_log must be append-only")
		}
	})

	t.Run("cancel and withdraw", func(t *testing.T) {
		// second booking: cancelled by the client while the offer is out
		r := h.expect(201, "client", "POST", "/client/requests", map[string]any{"service_id": svcID, "address_id": addrID, "requested_start": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)})
		id2 := r["id"].(string)
		h.expect(200, "triage", "POST", "/console/requests/"+id2+"/claim", nil)
		a := h.expect(201, "triage", "POST", "/console/requests/"+id2+"/assignments", map[string]any{"provider_id": providerID, "selection_reason": "Next in line"})
		h.expect(422, "client", "POST", "/client/requests/"+id2+"/cancel", map[string]any{})
		c := h.expect(200, "client", "POST", "/client/requests/"+id2+"/cancel", map[string]any{"reason": "Plans changed"})
		if c["status"] != "cancelled" {
			t.Fatalf("cancel: %v", c)
		}
		h.expect(409, "triage", "POST", "/console/assignments/"+a["id"].(string)+"/withdraw", map[string]any{"reason": "request cancelled"})
		h.expect(409, "client", "POST", "/client/requests/"+id2+"/cancel", map[string]any{"reason": "again"})

		// third: provider declines, request returns to triage
		r3 := h.expect(201, "client", "POST", "/client/requests", map[string]any{"service_id": svcID, "address_id": addrID, "requested_start": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)})
		id3 := r3["id"].(string)
		a3 := h.expect(201, "triage", "POST", "/console/requests/"+id3+"/assignments", map[string]any{"provider_id": providerID, "selection_reason": "Available"})
		code, _ := h.call("provider", "POST", "/provider/offers/"+a3["id"].(string)+"/decline", map[string]any{"reason": "Not available"})
		if code != 204 {
			t.Fatalf("decline: %d", code)
		}
		if got := h.expect(200, "client", "GET", "/client/requests/"+id3, nil); got["status"] != "triaging" {
			t.Fatalf("after decline: %v", got["status"])
		}
		h.expect(200, "triage", "POST", "/console/requests/"+id3+"/mark-unfilled", map[string]any{"reason": "No one available that day"})

		// expired offers answer 410 and send the request back to triage
		r4 := h.expect(201, "client", "POST", "/client/requests", map[string]any{"service_id": svcID, "address_id": addrID, "requested_start": time.Now().Add(96 * time.Hour).UTC().Format(time.RFC3339)})
		a4 := h.expect(201, "triage", "POST", "/console/requests/"+r4["id"].(string)+"/assignments", map[string]any{"provider_id": providerID, "selection_reason": "Available", "offer_expires_in_minutes": 5})
		h.sql(`UPDATE assignments SET offer_expires_at = now() - interval '1 minute' WHERE id = $1`, a4["id"])
		h.expect(410, "provider", "POST", "/provider/offers/"+a4["id"].(string)+"/accept", nil)
		if got := h.expect(200, "client", "GET", "/client/requests/"+r4["id"].(string), nil); got["status"] != "triaging" {
			t.Fatalf("after expiry: %v", got["status"])
		}
	})

	t.Run("gate details are held back until the day", func(t *testing.T) {
		r := h.expect(201, "client", "POST", "/client/requests", map[string]any{"service_id": svcID, "address_id": addrID, "requested_start": time.Now().Add(30 * time.Hour).UTC().Format(time.RFC3339)})
		a := h.expect(201, "triage", "POST", "/console/requests/"+r["id"].(string)+"/assignments", map[string]any{"provider_id": providerID, "selection_reason": "Regular"})
		job := h.expect(200, "provider", "POST", "/provider/offers/"+a["id"].(string)+"/accept", nil)
		if job["address"].(map[string]any)["access_details"] != nil {
			t.Fatal("gate details must not be released a day ahead")
		}
	})

	t.Run("sessions", func(t *testing.T) {
		h.expect(204, "client", "POST", "/auth/logout", nil)
		h.expect(401, "client", "GET", "/me", nil)
		// login with a fresh code works for an existing account; an unknown phone gets the same answer as a wrong code
		h.sql(`UPDATE otp_codes SET created_at = created_at - interval '2 minutes'`)
		res := h.expect(200, "", "POST", "/auth/otp/request", map[string]any{"phone": "+254700000001", "purpose": "login"})
		code := codeRE.FindStringSubmatch(h.sms.Last().Body)[1]
		s := h.expect(200, "", "POST", "/auth/otp/verify", map[string]any{"otp_id": res["otp_id"], "code": code})
		if s["user"].(map[string]any)["id"] != clientID {
			t.Fatalf("login: %v", s)
		}
		h.expect(400, "", "POST", "/auth/otp/verify", map[string]any{"otp_id": res["otp_id"], "code": code}) // codes are single use
	})
}

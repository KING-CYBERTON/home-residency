package provider_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/config"
	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	"github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/email"
	"github.com/KING-CYBERTON/home-residency/backend/internal/mpesa"
	"github.com/KING-CYBERTON/home-residency/backend/internal/sms"
	"github.com/KING-CYBERTON/home-residency/backend/internal/storage"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web/provider"
)

// Page-weight budget for provider pages (ADR-0001: light pages for low-end phones on metered data).
// Sizes are gzip-compressed, as sent over the wire.
const (
	maxPageHTML  = 10 << 10 // one portal page's HTML
	maxPortalJS  = 6 << 10  // portal.js + queue.js
	maxFirstLoad = 60 << 10 // first visit: HTML + shared CSS + HTMX + portal scripts
)

// These tests need PostgreSQL, like the API tests: set TEST_DATABASE_URL to a scratch database.
type env struct {
	t     *testing.T
	srv   *httptest.Server
	app   *api.Server
	pool  *pgxpool.Pool
	sms   *sms.Capture
	site  *web.Site
	sizes map[string]int // gzip size of each page fetched
}

func newEnv(t *testing.T) *env {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	// go test runs packages in parallel and the API tests reset TEST_DATABASE_URL, so this package
	// works in its own database next to it.
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(u.Path, "/") + "_provider"
	admin, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	if err == nil {
		_, err = admin.Exec(ctx, `CREATE DATABASE `+name)
	}
	admin.Close()
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	dsn = u.String()
	if err := db.Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
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
	store := &storage.Local{Dir: t.TempDir(), Secret: []byte("test")}
	app := api.NewServer(pool, cfg, capture, cipher, &mpesa.Fake{}, store)
	site, err := web.New(web.Options{BaseURL: "http://test"})
	if err != nil {
		t.Fatal(err)
	}
	portal, err := provider.New(app, site, provider.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// The same wiring as cmd/api/web.go.
	app.Web = func(r chi.Router) {
		r.Handle("/static/*", site.StaticHandler())
		r.Group(func(r chi.Router) {
			r.Use(site.Middleware)
			r.Mount(web.ProviderPortalPath, portal.Routes())
			r.Mount(web.JoinPath, portal.JoinRoutes())
		})
	}
	h, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	store.BaseURL = srv.URL
	return &env{t: t, srv: srv, app: app, pool: pool, sms: capture, site: site, sizes: map[string]int{}}
}

func (e *env) sql(q string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), q, args...); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

func (e *env) one(q string, args ...any) string {
	e.t.Helper()
	var s string
	if err := e.pool.QueryRow(context.Background(), q, args...).Scan(&s); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	return s
}

var codeRE = regexp.MustCompile(`\b(\d{6})\b`)

func (e *env) lastCode(phone string) string {
	e.t.Helper()
	m := e.sms.Last()
	c := codeRE.FindStringSubmatch(m.Body)
	if c == nil || m.To != phone {
		e.t.Fatalf("no code sent to %s: %+v", phone, m)
	}
	return c[1]
}

func gz(b []byte) int {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Len()
}

// ---- a browser with a cookie jar

type browser struct {
	e *env
	c *http.Client
}

func (e *env) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{e: e, c: &http.Client{Jar: jar}}
}

func (b *browser) do(req *http.Request) (int, string, string) {
	b.e.t.Helper()
	resp, err := b.c.Do(req)
	if err != nil {
		b.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw), resp.Request.URL.RequestURI()
}

// get fetches a page, checks it is a 200 and records its size for the budget.
func (b *browser) get(path string) string {
	b.e.t.Helper()
	req, _ := http.NewRequest("GET", b.e.srv.URL+path, nil)
	code, body, _ := b.do(req)
	if code != 200 {
		b.e.t.Fatalf("GET %s: %d\n%s", path, code, body)
	}
	b.e.sizes[path] = gz([]byte(body))
	return body
}

// post submits a form the way a browser does (following the 303) and returns the page it lands on.
func (b *browser) post(path string, form url.Values, headers ...string) (int, string, string) {
	b.e.t.Helper()
	req, _ := http.NewRequest("POST", b.e.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return b.do(req)
}

func (b *browser) postJSON(path string, body any) (int, []byte) {
	b.e.t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", b.e.srv.URL+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	code, out, _ := b.do(req)
	return code, []byte(out)
}

func must(t *testing.T, ok bool, msg string, args ...any) {
	t.Helper()
	if !ok {
		t.Fatalf(msg, args...)
	}
}

// ---- the JSON API, for the client and the console side of a job

func (e *env) api(token, method, path string, body any) map[string]any {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, e.srv.URL+api.BaseURL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		e.t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func (e *env) registerAPI(phone, name, kind string) (token, id string) {
	e.t.Helper()
	res := e.api("", "POST", "/auth/otp/request", map[string]any{"phone": phone, "purpose": "register"})
	s := e.api("", "POST", "/auth/otp/verify", map[string]any{"otp_id": res["otp_id"], "code": e.lastCode(phone), "full_name": name, "kind": kind,
		"consents": []any{map[string]any{"purpose": "privacy_notice", "notice_version": "2026-10", "granted": true}}})
	return s["token"].(string), s["user"].(map[string]any)["id"].(string)
}

func (e *env) staff(email string, roles ...string) string {
	e.t.Helper()
	pw, secret, _, err := e.app.CreateStaff(context.Background(), "Officer", email, roles)
	if err != nil {
		e.t.Fatal(err)
	}
	tok := e.api("", "POST", "/auth/staff/login", map[string]any{"email": email, "password": pw})["token"].(string)
	code, _ := totp.GenerateCode(secret, time.Now())
	e.api(tok, "POST", "/auth/staff/mfa", map[string]any{"code": code})
	return tok
}

func (e *env) seedCatalogue() (areaID, catID, svcID string) {
	areaID = e.one(`INSERT INTO service_areas (slug, name, is_active, centre_lat, centre_lng, radius_km) VALUES ('kilimani','Kilimani',true,-1.2921,36.7872,3) RETURNING id::text`)
	catID = e.one(`INSERT INTO service_categories (slug, name, engagement, is_active, description) VALUES ('cleaning','Cleaning','one_off',true,'Homes and apartments') RETURNING id::text`)
	svcID = e.one(`INSERT INTO services (category_id, slug, name, pricing_unit, scope_items, is_active, default_duration_minutes)
		VALUES ($1,'standard-clean','Standard clean','per_job','["Kitchen","Bathrooms"]',true,180) RETURNING id::text`, catID)
	e.sql(`INSERT INTO rate_card_entries (service_id, client_price_minor, provider_pay_minor, valid_from) VALUES ($1, 250000, 200000, now() - interval '1 day')`, svcID)
	tpl := e.one(`INSERT INTO checklist_templates (category_id, version, is_current) VALUES ($1,1,true) RETURNING id::text`, catID)
	e.sql(`INSERT INTO checklist_template_items (template_id, kind, document_type, label, sort_order, validity_months) VALUES ($1,'document','good_conduct_certificate','Certificate of Good Conduct',1,12)`, tpl)
	e.sql(`INSERT INTO checklist_template_items (template_id, kind, label, min_count, sort_order) VALUES ($1,'referee','One referee',1,2)`, tpl)
	return
}

// ---- tests

func TestSignedOutIsSentToSignIn(t *testing.T) {
	e := newEnv(t)
	b := e.browser()
	req, _ := http.NewRequest("GET", e.srv.URL+"/provider/jobs", nil)
	code, body, at := b.do(req)
	must(t, code == 200 && strings.HasPrefix(at, "/provider/login?next=%2Fprovider%2Fjobs"), "landed on %s (%d)", at, code)
	must(t, strings.Contains(body, "Provider sign in"), "sign-in page not shown")

	// The offline sync answers JSON, so the phone keeps its queue and asks to sign in again.
	c, raw := b.postJSON("/provider/sync", map[string]any{"events": []any{}})
	must(t, c == 401 && strings.Contains(string(raw), "unauthenticated"), "sync signed out: %d %s", c, raw)

	// A form posted from another site is refused.
	c, _, _ = b.post("/provider/logout", url.Values{}, "Sec-Fetch-Site", "cross-site")
	must(t, c == 403, "cross-site post: %d", c)
}

func TestRecruitmentAndApplication(t *testing.T) {
	e := newEnv(t)
	_, catID, _ := e.seedCatalogue()
	b := e.browser()

	join := b.get("/join/")
	must(t, strings.Contains(join, "Work with Homend Residence") && strings.Contains(join, "Cleaning"), "join page lists categories")

	_, body, _ := b.post("/join/apply", url.Values{"full_name": {"Achieng Odhiambo"}, "phone": {"0711 000 111"}, "category": {catID}})
	must(t, strings.Contains(body, "accept the privacy notice"), "consent is required")
	_, body, _ = b.post("/join/apply", url.Values{"full_name": {"Achieng Odhiambo"}, "phone": {"0711 000 111"}, "category": {catID}, "privacy": {"on"}, "vetting": {"on"}})
	otpID := regexp.MustCompile(`name="otp_id" value="([^"]+)"`).FindStringSubmatch(body)
	must(t, otpID != nil, "code form shown: %s", body)
	_, body, at := b.post("/join/apply/verify", url.Values{"full_name": {"Achieng Odhiambo"}, "phone": {"+254711000111"}, "category": {catID},
		"otp_id": {otpID[1]}, "code": {e.lastCode("+254711000111")}})
	must(t, strings.HasPrefix(at, "/provider/applications/new"), "after sign-up landed on %s", at)
	must(t, strings.Contains(body, `value="`+catID+`" required class="mt-1" checked`), "category preselected: %s", body)
	must(t, e.one(`SELECT count(*)::text FROM consents WHERE purpose IN ('privacy_notice','vetting_checks')`) == "2", "both consents recorded")

	_, body, at = b.post("/provider/applications", url.Values{"category_id": {catID}})
	must(t, strings.HasPrefix(at, "/provider/applications/"), "application page: %s", at)
	appID := strings.TrimPrefix(at, "/provider/applications/")
	must(t, strings.Contains(body, "Certificate of Good Conduct") && strings.Contains(body, "Still needed"), "checklist shown")

	// Submitting early is refused with the handler's own list of what is missing.
	_, body, _ = b.post("/provider/applications/"+appID+"/submit", url.Values{})
	must(t, strings.Contains(body, "bg-red-50"), "early submit refused: %s", body)

	// Upload the certificate the way portal.js does: ticket, PUT to the vault, confirm and attach.
	file := []byte("\x89PNG fake certificate")
	code, raw := b.postJSON("/provider/documents/ticket", map[string]any{"document_type": "good_conduct_certificate", "mime_type": "image/png",
		"size_bytes": len(file), "original_filename": "gcc.png", "expires_on": time.Now().AddDate(1, 0, 0).Format("2006-01-02")})
	must(t, code == 201, "ticket: %d %s", code, raw)
	var ticket struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
		UploadURL     string            `json:"upload_url"`
		UploadHeaders map[string]string `json:"upload_headers"`
	}
	_ = json.Unmarshal(raw, &ticket)
	put, _ := http.NewRequest("PUT", ticket.UploadURL, bytes.NewReader(file))
	for k, v := range ticket.UploadHeaders {
		put.Header.Set(k, v)
	}
	put.Header.Set("Content-Type", "image/png")
	resp, err := http.DefaultClient.Do(put)
	must(t, err == nil && resp.StatusCode < 300, "vault upload failed: %v %v", err, resp)
	item := e.one(`SELECT x.id::text FROM application_items x JOIN checklist_template_items i ON i.id = x.template_item_id WHERE x.application_id = $1 AND i.kind = 'document'`, appID)
	_, body, _ = b.post("/provider/documents/"+ticket.Document.ID+"/complete", url.Values{"application_id": {appID}, "item_id": {item}})
	must(t, strings.Contains(body, "Document added to your application"), "attach: %s", body)

	_, body, _ = b.post("/provider/applications/"+appID+"/referees", url.Values{"full_name": {"Mary Wambui"}, "phone": {"0722 111 222"}, "relationship": {"Former employer"}})
	must(t, strings.Contains(body, "Mary Wambui"), "referee listed")

	_, body, _ = b.post("/provider/applications/"+appID+"/submit", url.Values{})
	must(t, strings.Contains(body, "Submitted."), "submit: %s", body)
	must(t, e.one(`SELECT status::text FROM applications WHERE id = $1`, appID) == "submitted", "application submitted")

	for _, p := range []string{"/provider/", "/provider/applications", "/provider/documents", "/provider/training", "/provider/contract", "/provider/profile", "/provider/more"} {
		b.get(p)
	}
	e.checkBudget(t)
}

func TestJobOfferCheckInOfflineAndPay(t *testing.T) {
	e := newEnv(t)
	areaID, catID, svcID := e.seedCatalogue()
	clientTok, _ := e.registerAPI("+254700000001", "Wanjiru Kamau", "client")
	_, providerID := e.registerAPI("+254700000002", "Peter Otieno", "provider")
	e.sql(`UPDATE providers SET status = 'active', max_concurrent_jobs = 3, home_lat = -1.29, home_lng = 36.79, activated_at = now(), payout_msisdn = '+254700000002' WHERE user_id = $1`, providerID)
	e.sql(`INSERT INTO provider_categories (provider_id, category_id, status) VALUES ($1,$2,'active')`, providerID, catID)
	addr := e.api(clientTok, "POST", "/client/addresses", map[string]any{"label": "Home", "service_area_id": areaID, "lat": -1.2921, "lng": 36.7872,
		"estate": "Kilimani Gardens", "access_details": "Gate code 4455, flat B2"})["id"].(string)
	triage := e.staff("triage@example.com", "triage_officer")
	book := func(later time.Duration) (assignID string) {
		r := e.api(clientTok, "POST", "/client/requests", map[string]any{"accept_terms": true, "service_id": svcID, "address_id": addr,
			"requested_start": time.Now().Add(30*time.Minute + later).UTC().Format(time.RFC3339)})
		reqID := r["id"].(string)
		e.api(triage, "POST", "/console/requests/"+reqID+"/claim", nil)
		return e.api(triage, "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": providerID, "selection_reason": "Closest"})["id"].(string)
	}

	// Sign in on the portal with an SMS code (registering above used this phone's one code a minute).
	e.sql(`DELETE FROM otp_codes`)
	b := e.browser()
	_, body, _ := b.post("/provider/login", url.Values{"phone": {"0700 000 002"}})
	otpID := regexp.MustCompile(`name="otp_id" value="([^"]+)"`).FindStringSubmatch(body)
	must(t, otpID != nil, "code form: %s", body)
	_, body, at := b.post("/provider/login/verify", url.Values{"otp_id": {otpID[1]}, "code": {e.lastCode("+254700000002")}, "next": {"/provider/offers"}})
	must(t, at == "/provider/offers", "after sign-in landed on %s", at)

	// An offer arrives and is accepted with HTMX: the card is swapped for the result.
	assign := book(0)
	offers := b.get("/provider/offers")
	must(t, strings.Contains(offers, "Standard clean") && strings.Contains(offers, "KES 2,000") && !strings.Contains(offers, "Gate code"), "offer shown without access details")
	code, frag, _ := b.post("/provider/offers/"+assign+"/accept", url.Values{"idempotency_key": {uuid.NewString()}}, "HX-Request", "true")
	must(t, code == 200 && strings.Contains(frag, "Accepted") && !strings.Contains(frag, "<html"), "accept fragment: %d %s", code, frag)

	jobPage := b.get("/provider/jobs/" + assign)
	must(t, strings.Contains(jobPage, "Gate code 4455") && strings.Contains(jobPage, `data-offline="check_in"`), "job page has access details and the check-in form")
	visitID := e.one(`SELECT id::text FROM visits WHERE assignment_id = $1`, assign)
	doorCode := e.api(clientTok, "GET", "/client/visits/"+visitID+"/checkin-code", nil)["code"].(string)

	// Without JavaScript the check-in form posts directly; a wrong door code is explained.
	_, body, at = b.post("/provider/visits/"+visitID+"/check-in", url.Values{"job": {assign}, "method": {"code"}, "code": {"0000"}, "client_event_id": {uuid.NewString()}})
	must(t, at == "/provider/jobs/"+assign && strings.Contains(body, "That door code is wrong"), "wrong code: %s", at)

	// Check-in and check-out captured with no signal, sent later in one batch (what portal.js and the
	// service worker do). The device times are kept; a replay is recognised as a duplicate.
	in, out := uuid.NewString(), uuid.NewString()
	batch := map[string]any{"events": []any{
		map[string]any{"client_event_id": out, "visit_id": visitID, "event_type": "check_out", "device_recorded_at": time.Now().Add(-1 * time.Minute).UTC().Format(time.RFC3339), "payload": map[string]any{"notes": "All done"}},
		map[string]any{"client_event_id": in, "visit_id": visitID, "event_type": "check_in", "device_recorded_at": time.Now().Add(-20 * time.Minute).UTC().Format(time.RFC3339), "payload": map[string]any{"method": "code", "code": doorCode}},
	}}
	code, raw := b.postJSON("/provider/sync", batch)
	must(t, code == 200 && strings.Count(string(raw), `"applied"`) == 2, "sync: %d %s", code, raw)
	code, raw = b.postJSON("/provider/sync", batch)
	must(t, code == 200 && strings.Count(string(raw), `"duplicate"`) == 2, "replay: %d %s", code, raw)
	must(t, e.one(`SELECT status::text FROM visits WHERE id = $1`, visitID) == "completed", "visit completed")
	must(t, e.one(`SELECT to_char(checked_in_at AT TIME ZONE 'UTC', 'HH24:MI') FROM visits WHERE id = $1`, visitID) == time.Now().Add(-20*time.Minute).UTC().Format("15:04"), "device check-in time kept")

	// The next job, accepted with a plain form, lands on its job page.
	assign2 := book(24 * time.Hour)
	_, _, at = b.post("/provider/offers/"+assign2+"/accept", url.Values{})
	must(t, at == "/provider/jobs/"+assign2, "plain accept landed on %s", at)

	// A third offer is declined with a plain form.
	assign3 := book(48 * time.Hour)
	_, body, _ = b.post("/provider/offers/"+assign3+"/decline", url.Values{"reason": {"Too far"}})
	must(t, strings.Contains(body, "Offer declined"), "decline")

	// Availability and time off.
	_, body, _ = b.post("/provider/availability", url.Values{"on_1": {"on"}, "start_1": {"08:00"}, "end_1": {"17:00"}, "on_6": {"on"}, "start_6": {"09:00"}, "end_6": {"13:00"}})
	must(t, strings.Contains(body, "Saved."), "availability: %s", body)
	must(t, e.one(`SELECT count(*)::text FROM provider_availability WHERE provider_id = $1`, providerID) == "2", "two windows saved")
	from := time.Now().AddDate(0, 0, 10).In(time.FixedZone("EAT", 3*3600))
	_, body, _ = b.post("/provider/time-off", url.Values{"starts_at": {from.Format("2006-01-02T15:04")}, "ends_at": {from.Add(48 * time.Hour).Format("2006-01-02T15:04")}, "reason": {"Family"}})
	must(t, strings.Contains(body, "Time off added"), "time off: %s", body)

	_, body, _ = b.post("/provider/profile", url.Values{"payout_msisdn": {"0700 000 009"}, "years_experience": {"4"}, "languages": {"English, Kiswahili"}})
	must(t, strings.Contains(body, "Profile saved") && strings.Contains(body, "254700000009"), "profile: %s", body)

	for _, p := range []string{"/provider/", "/provider/jobs", "/provider/pay", "/provider/appraisal", "/provider/complaints", "/provider/inbox",
		"/provider/availability", "/provider/more", "/provider/offline", "/provider/login"} {
		b.get(p)
	}
	dash := e.sizes["/provider/"]
	must(t, dash > 0, "dashboard fetched")

	// The PWA files.
	sw := b.get("/provider/sw.js")
	must(t, strings.Contains(sw, "/static/css/app.css?v=") && !strings.Contains(sw, "self.HR_ASSETS"), "service worker lists the hashed assets")
	man := b.get("/provider/manifest.webmanifest")
	must(t, strings.Contains(man, `"scope":"/provider/"`), "manifest scope")

	// Signing out clears the pages kept for offline use.
	req, _ := http.NewRequest("POST", e.srv.URL+"/provider/logout", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	b.c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := b.c.Do(req)
	must(t, err == nil && resp.Header.Get("Clear-Site-Data") == `"cache", "storage"`, "logout clears cache and storage: %v", resp.Header)
	e.checkBudget(t)
}

func (e *env) checkBudget(t *testing.T) {
	t.Helper()
	static := func(name string) int {
		b, err := fs.ReadFile(provider.StaticFS(), "static/"+name)
		if err != nil {
			t.Fatal(err)
		}
		return gz(b)
	}
	js := static("portal.js") + static("queue.js")
	must(t, js <= maxPortalJS, "portal scripts are %d bytes gzipped, budget %d", js, maxPortalJS)
	shared := 0
	for _, a := range []string{"css/app.css", "js/htmx.min.js"} {
		req, _ := http.NewRequest("GET", e.srv.URL+e.site.Asset(a), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		shared += gz(raw)
	}
	for p, n := range e.sizes {
		if strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".webmanifest") {
			continue
		}
		must(t, n <= maxPageHTML, "%s is %d bytes gzipped, budget %d", p, n, maxPageHTML)
		must(t, n+shared+js <= maxFirstLoad, "first load of %s is %d bytes gzipped, budget %d", p, n+shared+js, maxFirstLoad)
	}
	t.Logf("page-weight: shared CSS+HTMX %d B, portal JS %d B (gzip)", shared, js)
}

// TestEmailSignInWithoutSMS runs the provider journey the way production does without an SMS
// gateway: apply with email and password, forget it, reset it from the emailed link, sign the
// contract with the password.
func TestEmailSignInWithoutSMS(t *testing.T) {
	e := newEnv(t)
	_, catID, _ := e.seedCatalogue()
	e.app.SMS = sms.New("none")
	mail := &email.Capture{}
	e.app.Email = mail
	b := e.browser()

	login := b.get("/provider/login")
	must(t, strings.Contains(login, `action="/provider/login/password"`) && !strings.Contains(login, "with=phone"), "email form only: %s", login)
	apply := b.get("/join/apply?with=phone&category=" + catID)
	must(t, strings.Contains(apply, `action="/join/apply/password"`) && !strings.Contains(apply, `name="code"`) && !strings.Contains(apply, "We will send you a code"), "no SMS code form with SMS off")

	_, body, _ := b.post("/join/apply/password", url.Values{"full_name": {"Achieng Odhiambo"}, "email": {"achieng@example.com"}, "phone": {"0711 222 333"}, "password": {"a good password"}, "category": {catID}})
	must(t, strings.Contains(body, "accept the privacy notice"), "consent is required")
	_, body, _ = b.post("/join/apply/password", url.Values{"full_name": {"Achieng Odhiambo"}, "email": {"achieng@example.com"}, "password": {"a good password"},
		"category": {catID}, "privacy": {"on"}, "vetting": {"on"}})
	must(t, strings.Contains(body, "the vetting team needs to call you"), "a phone is required: %s", body)
	_, _, at := b.post("/join/apply/password", url.Values{"full_name": {"Achieng Odhiambo"}, "email": {"achieng@example.com"}, "phone": {"0711 222 333"}, "password": {"a good password"},
		"category": {catID}, "privacy": {"on"}, "vetting": {"on"}})
	must(t, strings.HasPrefix(at, "/provider/applications/new"), "after sign-up landed on %s", at)
	b.post("/provider/logout", url.Values{})

	_, body, _ = b.post("/provider/login/password", url.Values{"email": {"achieng@example.com"}, "password": {"wrong password"}})
	must(t, strings.Contains(body, "Email or password is wrong"), "wrong password refused: %s", body)
	_, _, at = b.post("/provider/login/password", url.Values{"email": {"achieng@example.com"}, "password": {"a good password"}, "next": {"/provider/profile"}})
	must(t, at == "/provider/profile", "after sign-in landed on %s", at)
	b.post("/provider/logout", url.Values{})

	_, body, _ = b.post("/provider/forgot-password", url.Values{"email": {"achieng@example.com"}})
	must(t, strings.Contains(body, "Check your email") && len(mail.Msgs) == 1, "reset email sent: %s", body)
	link := regexp.MustCompile(`/provider/reset-password\?token=\S+`).FindString(mail.Msgs[0].Text)
	must(t, link != "", "reset link: %s", mail.Msgs[0].Text)
	page := b.get(link)
	token := regexp.MustCompile(`name="token" value="([^"]+)"`).FindStringSubmatch(page)
	must(t, token != nil, "reset form: %s", page)
	_, _, at = b.post("/provider/reset-password", url.Values{"token": {token[1]}, "password": {"a better password"}})
	must(t, at == "/provider/login", "after reset landed on %s", at)
	_, _, at = b.post("/provider/login/password", url.Values{"email": {"achieng@example.com"}, "password": {"a better password"}})
	must(t, at == "/provider/", "signed in with the new password: %s", at)

	e.sql(`UPDATE providers SET status = 'approved'`)
	page = b.get("/provider/contract")
	must(t, strings.Contains(page, "Enter your password to sign") && !strings.Contains(page, "contract/code"), "contract signs by password: %s", page)
	versions := regexp.MustCompile(`name="contract_version" value="([^"]*)"`).FindStringSubmatch(page)
	conduct := regexp.MustCompile(`name="code_of_conduct_version" value="([^"]*)"`).FindStringSubmatch(page)
	_, _, at = b.post("/provider/contract/sign", url.Values{"contract_version": {versions[1]}, "code_of_conduct_version": {conduct[1]}, "password": {"a better password"}})
	must(t, at == "/provider/", "after signing landed on %s", at)
	must(t, e.one(`SELECT count(*)::text FROM provider_contracts`) == "1", "contract recorded")
}

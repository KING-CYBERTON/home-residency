package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/config"
	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	"github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
	"github.com/KING-CYBERTON/home-residency/backend/internal/mpesa"
	"github.com/KING-CYBERTON/home-residency/backend/internal/sms"
	"github.com/KING-CYBERTON/home-residency/backend/internal/storage"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web/client"
)

// The pages render without a database: templates parse and the public pieces answer.
func TestPagesRenderWithoutDatabase(t *testing.T) {
	site, err := web.New(web.Options{BaseURL: "https://homend.test"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := client.New(client.Deps{Site: site, API: gen.Unimplemented{}})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Handle("/static/*", site.StaticHandler())
	r.With(site.Middleware).Mount(web.ClientPortalPath, p.Routes())
	srv := httptest.NewServer(r)
	defer srv.Close()

	for path, want := range map[string]string{
		"/account/login":                "Send code",
		"/account/login?mode=register":  "privacy notice",
		"/account/offline":              "You are offline",
		"/account/manifest.webmanifest": `"scope": "/account/"`,
		"/account/sw.js":                `const BASE = "/account"`,
		"/account/static/client.js":     "serviceWorker",
	} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(string(body), want) {
			t.Errorf("GET %s: %d, want %q in\n%s", path, res.StatusCode, want, body)
		}
	}
	var m map[string]any
	res, _ := http.Get(srv.URL + "/account/manifest.webmanifest")
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil || m["display"] != "standalone" {
		t.Fatalf("manifest is not valid JSON: %v %v", err, m)
	}
	for _, icon := range m["icons"].([]any) {
		src := icon.(map[string]any)["src"].(string)
		if res, _ := http.Get(srv.URL + src); res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" {
			t.Errorf("%s: %d %s", src, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
	// Signed-out visitors are sent to sign in, keeping where they were going.
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, _ = c.Get(srv.URL + "/account/requests")
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/account/login?next=%2Faccount%2Frequests" {
		t.Fatalf("signed-out redirect: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
}

// ---- the household journey against Postgres

type env struct {
	t      *testing.T
	srv    *httptest.Server
	app    *api.Server
	pool   *pgxpool.Pool
	sms    *sms.Capture
	mpesa  *mpesa.Fake
	tokens map[string]string
}

func newEnv(t *testing.T) *env {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	// go test runs packages in parallel and the API and public site tests use their own databases,
	// so the portal resets a database of its own next to them.
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(u.Path, "/") + "_client"
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
	capture, fake := &sms.Capture{}, &mpesa.Fake{}
	store := &storage.Local{Dir: t.TempDir(), Secret: []byte("test")}
	app := api.NewServer(pool, cfg, capture, cipher, fake, store)
	apiHandler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	site, err := web.New(web.Options{BaseURL: "https://homend.test"})
	if err != nil {
		t.Fatal(err)
	}
	portal, err := client.New(client.Deps{Site: site, API: app, Auth: app.Auth, PushKey: "BPushTestKey"})
	if err != nil {
		t.Fatal(err)
	}
	// Mounted the way cmd/api does it, behind the shared HTML middleware.
	root := chi.NewRouter()
	root.Use(httpx.RequestIDMiddleware, httpx.Recover)
	root.Handle("/static/*", site.StaticHandler())
	root.With(site.Middleware).Mount(web.ClientPortalPath, portal.Routes())
	root.Mount("/", apiHandler)
	srv := httptest.NewServer(root)
	t.Cleanup(srv.Close)
	store.BaseURL = srv.URL
	return &env{t: t, srv: srv, app: app, pool: pool, sms: capture, mpesa: fake, tokens: map[string]string{}}
}

func (e *env) sql(q string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), q, args...); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

func (e *env) scan(q string, args ...any) string {
	e.t.Helper()
	var s string
	if err := e.pool.QueryRow(context.Background(), q, args...).Scan(&s); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	return s
}

// api calls the JSON API as staff or a provider, for the parts of the journey that are not the household's.
func (e *env) api(who, method, path string, body any) map[string]any {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, e.srv.URL+api.BaseURL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if tok := e.tokens[who]; tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode > 299 {
		e.t.Fatalf("%s %s as %s: %d %s", method, path, who, res.StatusCode, raw)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

var codeRE = regexp.MustCompile(`\b(\d{6})\b`)

func (e *env) lastCode(phone string) string {
	e.t.Helper()
	last := e.sms.Last()
	m := codeRE.FindStringSubmatch(last.Body)
	if m == nil || last.To != phone {
		e.t.Fatalf("no code sent to %s: %+v", phone, last)
	}
	return m[1]
}

func (e *env) provider(phone, name string) string {
	e.t.Helper()
	res := e.api("", "POST", "/auth/otp/request", map[string]any{"phone": phone, "purpose": "register"})
	s := e.api("", "POST", "/auth/otp/verify", map[string]any{"otp_id": res["otp_id"], "code": e.lastCode(phone), "full_name": name, "kind": "provider"})
	e.tokens["provider"] = s["token"].(string)
	return s["user"].(map[string]any)["id"].(string)
}

func (e *env) staff(who, email string, roles ...string) {
	e.t.Helper()
	pw, secret, _, err := e.app.CreateStaff(context.Background(), who, email, roles)
	if err != nil {
		e.t.Fatal(err)
	}
	e.tokens[who] = e.api("", "POST", "/auth/staff/login", map[string]any{"email": email, "password": pw})["token"].(string)
	code, _ := totp.GenerateCode(secret, time.Now())
	e.api(who, "POST", "/auth/staff/mfa", map[string]any{"code": code})
}

// browser is a household's phone: it keeps cookies and sends same-origin form posts.
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
	res, err := b.c.Do(req)
	if err != nil {
		b.e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	body := string(raw)
	if res.StatusCode >= 500 {
		b.e.t.Fatalf("%s %s: %d %s", req.Method, req.URL, res.StatusCode, body)
	}
	return res.StatusCode, body, res.Request.URL.Path
}

func (b *browser) get(path string) (string, string) {
	b.e.t.Helper()
	req, _ := http.NewRequest("GET", b.e.srv.URL+path, nil)
	code, body, at := b.do(req)
	if code != 200 {
		b.e.t.Fatalf("GET %s: %d\n%s", path, code, body)
	}
	return body, at
}

// post submits a form. With htmx set it behaves like an HTMX request and returns the fragment.
func (b *browser) post(path string, form url.Values, htmx bool) (int, string, string) {
	b.e.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	req, _ := http.NewRequest("POST", b.e.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return b.do(req)
}

func must(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Fatalf("want %q in page:\n%s", w, body)
		}
	}
}

var otpIDRE = regexp.MustCompile(`name="otp_id" value="([^"]+)"`)

func (b *browser) signUp(phone, local, name string) {
	t := b.e.t
	t.Helper()
	b.get("/account/login?mode=register")
	_, page, _ := b.post("/account/login", url.Values{"mode": {"register"}, "full_name": {name}, "phone": {local}, "consent": {"yes"}}, true)
	must(t, page, "We sent a code to")
	otpID := otpIDRE.FindStringSubmatch(page)[1]
	code, _, _ := b.post("/account/login/verify", url.Values{"mode": {"register"}, "full_name": {name}, "phone": {phone}, "consent": {"yes"},
		"otp_id": {otpID}, "code": {b.e.lastCode(phone)}}, false)
	if code != 200 {
		t.Fatalf("verify: %d", code)
	}
}

func TestHouseholdJourney(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// Catalogue: one area, one cleaning service at KES 2,500 + 16% VAT.
	area := e.scan(`INSERT INTO service_areas (slug, name, county, is_active, centre_lat, centre_lng, radius_km) VALUES ('kilimani','Kilimani','Nairobi',true,-1.2921,36.7872,3) RETURNING id`)
	cat := e.scan(`INSERT INTO service_categories (slug, name, engagement, is_active) VALUES ('cleaning','Cleaning','one_off',true) RETURNING id`)
	svc := e.scan(`INSERT INTO services (category_id, slug, name, pricing_unit, scope_items, is_active, default_duration_minutes)
		VALUES ($1,'standard-clean','Standard clean','per_job','["Kitchen","Bathrooms"]',true,180) RETURNING id`, cat)
	e.sql(`INSERT INTO rate_card_entries (service_id, client_price_minor, provider_pay_minor, valid_from) VALUES ($1, 250000, 200000, now() - interval '1 day')`, svc)

	// Sign up by phone. A wrong code is refused on the page; the right one signs in and asks for an address.
	b := e.browser()
	b.get("/account/login?mode=register")
	_, page, _ := b.post("/account/login", url.Values{"mode": {"register"}, "full_name": {"Wanjiru Kamau"}, "phone": {"0700 000 001"}, "consent": {"yes"}}, true)
	must(t, page, "0700 000 001")
	otpID := otpIDRE.FindStringSubmatch(page)[1]
	code := e.lastCode("+254700000001")
	form := url.Values{"mode": {"register"}, "full_name": {"Wanjiru Kamau"}, "phone": {"+254700000001"}, "consent": {"yes"}, "otp_id": {otpID}}
	form.Set("code", "000000")
	_, page, _ = b.post("/account/login/verify", form, true)
	must(t, page, "That code is wrong")
	form.Set("code", code)
	_, page, at := b.post("/account/login/verify", form, false)
	if at != "/account/addresses" {
		t.Fatalf("after sign-up landed on %s", at)
	}
	must(t, page, "Welcome, Wanjiru Kamau", "Add your address", "Kilimani")

	// A form posted from another site is refused (shared cross-origin protection).
	forged, _ := http.NewRequest("POST", e.srv.URL+"/account/addresses", strings.NewReader(url.Values{"service_area_id": {area}}.Encode()))
	forged.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	forged.Header.Set("Sec-Fetch-Site", "cross-site")
	if code, _, _ := b.do(forged); code != http.StatusForbidden {
		t.Fatalf("cross-site form post: %d", code)
	}

	_, page, at = b.post("/account/addresses", url.Values{"label": {"Home"}, "service_area_id": {area}, "estate": {"Kilimani Gardens"},
		"house_number": {"B4"}, "lat": {"-1.2921"}, "lng": {"36.7872"}, "access_details": {"Gate code 4411"}, "next": {"/account/book"}}, false)
	if at != "/account/book" {
		t.Fatalf("after address landed on %s", at)
	}
	must(t, page, "Standard clean", "Kilimani Gardens")
	addr := e.scan(`SELECT id FROM addresses WHERE label = 'Home'`)

	// Live quote, then book.
	_, frag, _ := b.post("/account/book/quote", url.Values{"service_id": {svc}, "address_id": {addr}, "quantity": {"1"}}, true)
	must(t, frag, "KES 2,500", "KES 400", "KES 2,900")
	start := time.Now().Add(45 * time.Minute).In(time.FixedZone("EAT", 3*3600)).Format("2006-01-02T15:04")
	key := uuid.NewString()
	bookForm := url.Values{"category": {"cleaning"}, "service_id": {svc}, "address_id": {addr}, "quantity": {"1"}, "start": {start}, "notes": {"Two bedrooms"}, "key": {key}}
	_, page, at = b.post("/account/book", bookForm, false)
	if !strings.HasPrefix(at, "/account/requests/") {
		t.Fatalf("after booking landed on %s\n%s", at, page)
	}
	reqID := strings.TrimPrefix(at, "/account/requests/")
	must(t, page, "Booked. Your reference is", "Received", "Two bedrooms", "Cancel this booking")
	// A double tap on "Book now" replays instead of booking twice.
	b.post("/account/book", bookForm, false)
	if n := e.scan(`SELECT count(*)::text FROM service_requests`); n != "1" {
		t.Fatalf("double submit created %s requests", n)
	}

	// Triage assigns a provider, who accepts.
	prov := e.provider("+254700000002", "Peter Otieno")
	e.sql(`UPDATE providers SET status = 'active', max_concurrent_jobs = 2, home_lat = -1.2900, home_lng = 36.7900, activated_at = now() WHERE user_id = $1`, prov)
	e.sql(`INSERT INTO provider_categories (provider_id, category_id, status) VALUES ($1,$2,'active')`, prov, cat)
	e.staff("triage", "triage@homend.test", "triage_officer")
	e.api("triage", "POST", "/console/requests/"+reqID+"/claim", nil)
	asg := e.api("triage", "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": prov, "selection_reason": "Closest"})
	e.api("provider", "POST", "/provider/offers/"+asg["id"].(string)+"/accept", nil)
	visit := e.scan(`SELECT id FROM visits WHERE request_id = $1`, reqID)

	page, _ = b.get("/account/requests/" + reqID)
	must(t, page, "Peter", "Show door code")

	// Door code at the door, check-in and check-out by the provider.
	_, frag, _ = b.post("/account/visits/"+visit+"/door-code", nil, true)
	doorCode := regexp.MustCompile(`tracking-\[0\.3em\][^>]*>(\d{4})<`).FindStringSubmatch(frag)
	if doorCode == nil {
		t.Fatalf("no door code in %s", frag)
	}
	e.api("provider", "POST", "/provider/visits/"+visit+"/check-in", map[string]any{"client_event_id": uuid.NewString(), "method": "code", "code": doorCode[1],
		"device_recorded_at": time.Now().UTC().Format(time.RFC3339)})
	e.api("provider", "POST", "/provider/visits/"+visit+"/check-out", map[string]any{"client_event_id": uuid.NewString(),
		"device_recorded_at": time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339)})

	// Confirm and rate; the rating closes the job.
	page, _ = b.get("/account/requests/" + reqID)
	must(t, page, "Confirm the work is done", "How did it go?")
	_, page, _ = b.post("/account/visits/"+visit+"/confirm", url.Values{"request_id": {reqID}}, false)
	must(t, page, "Thanks for confirming the visit")
	_, page, _ = b.post("/account/requests/"+reqID+"/rating", url.Values{"stars": {"5"}, "quality": {"5"}, "comment": {"Spotless"}, "visit_id": {visit}}, false)
	must(t, page, "Thank you for rating this job", "Done")

	// Pay the invoice by M-Pesa: the prompt goes out, the panel polls, the callback settles it.
	page, _ = b.get("/account/invoices")
	must(t, page, "KES 2,900", "To pay")
	inv := e.scan(`SELECT id FROM invoices WHERE request_id = $1`, reqID)
	page, _ = b.get("/account/invoices/" + inv)
	must(t, page, "Pay KES 2,900 with M-Pesa", "0700 000 001")
	_, frag, _ = b.post("/account/invoices/"+inv+"/pay", url.Values{"phone": {"0712 345 678"}, "key": {uuid.NewString()}}, true)
	must(t, frag, "Check your phone", `hx-trigger="every 3s"`)
	if len(e.mpesa.STKs) != 1 || e.mpesa.STKs[0].MSISDN != "254712345678" || e.mpesa.STKs[0].AmountKES != 2900 {
		t.Fatalf("stk: %+v", e.mpesa.STKs)
	}
	payID := e.scan(`SELECT id FROM payments WHERE invoice_id = $1`, inv)
	checkout := e.scan(`SELECT mpesa_checkout_request_id FROM payments WHERE id = $1`, payID)
	e.api("", "POST", "/webhooks/mpesa/stk", map[string]any{"Body": map[string]any{"stkCallback": map[string]any{
		"MerchantRequestID": "m1", "CheckoutRequestID": checkout, "ResultCode": 0, "ResultDesc": "ok",
		"CallbackMetadata": map[string]any{"Item": []any{
			map[string]any{"Name": "Amount", "Value": 2900}, map[string]any{"Name": "MpesaReceiptNumber", "Value": "QJK3ABC123"},
			map[string]any{"Name": "PhoneNumber", "Value": 254712345678}}}}}})
	req, _ := http.NewRequest("GET", e.srv.URL+"/account/payments/"+payID, nil)
	req.Header.Set("HX-Request", "true")
	_, frag, _ = b.do(req)
	must(t, frag, "Paid KES 2,900", "QJK3ABC123")
	page, _ = b.get("/account/invoices/" + inv)
	must(t, page, "Paid")

	// Raise a complaint about the booking, and message the team.
	_, page, at = b.post("/account/complaints", url.Values{"severity": {"quality"}, "request_id": {reqID}, "summary": {"Ironing was not done"}, "details": {"Shirts left unironed"}}, false)
	if !strings.HasPrefix(at, "/account/complaints/") {
		t.Fatalf("after complaint landed on %s\n%s", at, page)
	}
	must(t, page, "received", "Ironing was not done")
	_, page, at = b.post("/account/messages", url.Values{"subject": {"Invoice"}, "message": {"Can I get a receipt by email?"}, "request_id": {reqID}}, false)
	must(t, page, "Can I get a receipt by email?")
	_, page, _ = b.post(at, url.Values{"body": {"My number is 0712345678"}}, false)
	if strings.Contains(page, "0712345678") {
		t.Fatal("contact details were not masked in messages")
	}

	// Home shows the finished job; the account page and plans render.
	page, _ = b.get("/account/")
	must(t, page, "Hello, Wanjiru Kamau", "Standard clean", "Done")
	page, _ = b.get("/account/profile")
	must(t, page, "ID verification", "Not verified")
	_, page, _ = b.post("/account/profile", url.Values{"full_name": {"Wanjiru K."}, "email": {"wanjiru@example.com"}, "language": {"sw"}}, false)
	must(t, page, "Saved.", "Wanjiru K.")
	b.get("/account/plans")
	page, _ = b.get("/account/notifications")
	must(t, page, `data-push="BPushTestKey"`)
	req, _ = http.NewRequest("POST", e.srv.URL+"/account/push", strings.NewReader(`{"endpoint":"https://push.example/abc","keys":{"p256dh":"BKey","auth":"secret"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if code, body, _ := b.do(req); code != http.StatusNoContent {
		t.Fatalf("push subscribe: %d %s", code, body)
	}
	if n := e.scan(`SELECT count(*)::text FROM web_push_subscriptions WHERE endpoint = 'https://push.example/abc'`); n != "1" {
		t.Fatalf("push subscriptions stored: %s", n)
	}

	// Another household cannot open this booking or invoice.
	other := e.browser()
	other.signUp("+254700000009", "0700000009", "Other Household")
	req, _ = http.NewRequest("GET", e.srv.URL+"/account/requests/"+reqID, nil)
	if code, _, _ := other.do(req); code != http.StatusNotFound {
		t.Fatalf("other household saw the booking: %d", code)
	}
	req, _ = http.NewRequest("GET", e.srv.URL+"/account/invoices/"+inv, nil)
	if code, _, _ := other.do(req); code != http.StatusNotFound {
		t.Fatalf("other household saw the invoice: %d", code)
	}

	// A provider's session is not a household's.
	pb := e.browser()
	jarURL, _ := url.Parse(e.srv.URL)
	pb.c.Jar.SetCookies(jarURL, []*http.Cookie{{Name: "hr_session", Value: e.tokens["provider"], Path: "/"}})
	req, _ = http.NewRequest("GET", e.srv.URL+"/account/", nil)
	if code, body, _ := pb.do(req); code != http.StatusForbidden || !strings.Contains(body, "This is the household app") {
		t.Fatalf("provider on the household app: %d", code)
	}

	// Signing out ends the session.
	b.get("/account/profile")
	_, _, at = b.post("/account/logout", nil, false)
	if at != "/account/login" {
		t.Fatalf("after sign-out landed on %s", at)
	}
	var revoked int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM sessions s JOIN users u ON u.id = s.user_id WHERE u.full_name = 'Wanjiru K.' AND s.revoked_at IS NOT NULL`).Scan(&revoked)
	if revoked != 1 {
		t.Fatalf("session not revoked")
	}
}

// Childcare needs a verified ID: the booking is turned away to the ID form, and the upload goes to the vault.
func TestIDVerificationGate(t *testing.T) {
	e := newEnv(t)
	area := e.scan(`INSERT INTO service_areas (slug, name, is_active) VALUES ('lavington','Lavington',true) RETURNING id`)
	cat := e.scan(`INSERT INTO service_categories (slug, name, engagement, is_active, requires_client_id_verification, vetting_tier) VALUES ('childcare','Childcare','day_based',true,true,'enhanced') RETURNING id`)
	svc := e.scan(`INSERT INTO services (category_id, slug, name, pricing_unit, is_active) VALUES ($1,'nanny-day','Nanny (day)','per_day',true) RETURNING id`, cat)
	e.sql(`INSERT INTO rate_card_entries (service_id, client_price_minor, provider_pay_minor, valid_from) VALUES ($1, 150000, 120000, now() - interval '1 day')`, svc)

	b := e.browser()
	b.signUp("+254700000003", "0700000003", "Akinyi Odhiambo")
	b.post("/account/addresses", url.Values{"label": {"Home"}, "service_area_id": {area}}, false)
	addr := e.scan(`SELECT id FROM addresses`)

	// The public site's "Book" link may name only the service slug and area.
	page, _ := b.get("/account/book?service=nanny-day&area=lavington")
	must(t, page, "Nanny (day)", "Every weekday", `value="`+svc+`" required checked`)
	start := time.Now().Add(26 * time.Hour).In(time.FixedZone("EAT", 3*3600)).Format("2006-01-02T15:04")
	_, page, at := b.post("/account/book", url.Values{"category": {"childcare"}, "service_id": {svc}, "address_id": {addr}, "quantity": {"1"},
		"start": {start}, "recurrence": {"FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"}, "key": {uuid.NewString()}}, false)
	if at != "/account/profile" {
		t.Fatalf("unverified childcare booking landed on %s\n%s", at, page)
	}
	must(t, page, "Identity verification is required", "Submit for checking")

	// The uploader: ticket, PUT to the presigned URL, complete.
	req, _ := http.NewRequest("POST", e.srv.URL+"/account/uploads", strings.NewReader(url.Values{"document_type": {"national_id"}, "mime_type": {"image/jpeg"},
		"size_bytes": {"5"}, "filename": {"id.jpg"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := b.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var ticket gen.UploadTicket
	_ = json.NewDecoder(res.Body).Decode(&ticket)
	res.Body.Close()
	if res.StatusCode != 201 || ticket.UploadUrl == "" {
		t.Fatalf("upload ticket: %d %+v", res.StatusCode, ticket)
	}
	put, _ := http.NewRequest(http.MethodPut, ticket.UploadUrl, strings.NewReader("hello"))
	for k, v := range *ticket.UploadHeaders {
		put.Header.Set(k, v)
	}
	if res, err := http.DefaultClient.Do(put); err != nil || res.StatusCode > 299 {
		t.Fatalf("put: %v %v", err, res)
	}
	req, _ = http.NewRequest("POST", e.srv.URL+"/account/uploads/"+ticket.Document.Id.String()+"/complete", nil)
	if code, body, _ := b.do(req); code != 200 {
		t.Fatalf("complete: %d %s", code, body)
	}
	_, page, _ = b.post("/account/profile/id-verification", url.Values{"document_id": {ticket.Document.Id.String()}, "id_number": {"12345678"}}, false)
	must(t, page, "We will check your ID", "We are checking your ID")
}

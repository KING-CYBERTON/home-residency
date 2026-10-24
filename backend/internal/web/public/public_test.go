package public_test

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/config"
	"github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web/public"
)

func TestNormalisePhone(t *testing.T) {
	for in, want := range map[string]string{
		"0712 345 678": "+254712345678", "712345678": "+254712345678", "254712345678": "+254712345678",
		"+254 712-345-678": "+254712345678", "0112345678": "+254112345678", "12": "12",
	} {
		if got := public.NormalisePhone(in); got != want {
			t.Errorf("NormalisePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

// newSite migrates a scratch database (TEST_DATABASE_URL, like the API tests), seeds a small
// catalogue and serves the public routes the way cmd/api mounts them.
func newSite(t *testing.T) (*httptest.Server, *api.Server) {
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
	name := strings.TrimPrefix(u.Path, "/") + "_web"
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
	for _, q := range []string{
		`INSERT INTO service_areas (slug, name, is_active, centre_lat, centre_lng, radius_km) VALUES ('kilimani','Kilimani',true,-1.2921,36.7872,3), ('karen','Karen',true,NULL,NULL,NULL), ('hidden','Hidden',false,NULL,NULL,NULL)`,
		`INSERT INTO service_categories (slug, name, description, engagement, is_active) VALUES ('cleaning','Cleaning','Regular and deep cleaning.','one_off',true), ('pest-control','Pest control',NULL,'one_off',false)`,
		`INSERT INTO services (category_id, slug, name, pricing_unit, scope_items, is_active, default_duration_minutes)
		   SELECT id,'standard-clean','Standard clean','per_job','["Kitchen","Bathrooms"]',true,180 FROM service_categories WHERE slug='cleaning'`,
		`INSERT INTO rate_card_entries (service_id, client_price_minor, provider_pay_minor, valid_from) SELECT id, 250000, 200000, now() - interval '1 day' FROM services`,
		`INSERT INTO rate_card_entries (service_id, service_area_id, client_price_minor, provider_pay_minor, valid_from)
		   SELECT s.id, a.id, 300000, 240000, now() - interval '1 day' FROM services s, service_areas a WHERE a.slug='kilimani'`,
		// Gardening has a price in Kilimani only, so it gets no Karen page.
		`INSERT INTO service_categories (slug, name, engagement, is_active) VALUES ('gardening','Gardening','one_off',true)`,
		`INSERT INTO services (category_id, slug, name, pricing_unit, scope_items, is_active, default_duration_minutes)
		   SELECT id,'garden-tidy','Garden tidy','per_job','[]',true,120 FROM service_categories WHERE slug='gardening'`,
		`INSERT INTO rate_card_entries (service_id, service_area_id, client_price_minor, provider_pay_minor, valid_from)
		   SELECT s.id, a.id, 300000, 240000, now() - interval '1 day' FROM services s, service_areas a WHERE s.slug='garden-tidy' AND a.slug='kilimani'`,
		`INSERT INTO faqs (category_id, question, answer) SELECT id, 'Do I need to supply cleaning products?', 'No, the provider brings them.' FROM service_categories WHERE slug='cleaning'`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(q, err)
		}
	}
	srv := api.NewServer(pool, config.Config{Env: "test"}, nil, nil, nil, nil)
	site, err := web.New(web.Options{BaseURL: "https://homend.test"})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := public.New(srv, site)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.NotFound(site.NotFound)
	r.Group(func(r chi.Router) {
		r.Use(site.Middleware)
		pub.Routes(r)
	})
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return ts, srv
}

func get(t *testing.T, ts *httptest.Server, path string, hdr ...string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(b)
}

func mustContain(t *testing.T, where, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s: missing %q", where, w)
		}
	}
}

func TestPublicPages(t *testing.T) {
	ts, _ := newSite(t)

	for _, path := range []string{"/", "/services", "/services/cleaning", "/services/cleaning/kilimani", "/areas", "/areas/karen",
		"/pricing", "/how-it-works", "/faq", "/book", "/contact", "/privacy", "/terms", "/credits"} {
		code, h, body := get(t, ts, path)
		if code != 200 {
			t.Errorf("%s: status %d", path, code)
			continue
		}
		if !strings.HasPrefix(h.Get("Content-Type"), "text/html") || h.Get("Content-Security-Policy") == "" || !strings.Contains(h.Get("Cache-Control"), "public") {
			t.Errorf("%s: headers %v", path, h)
		}
		mustContain(t, path, body, `<link rel="canonical" href="https://homend.test`+path+`">`, `<meta name="description"`, "</html>")
	}

	_, _, body := get(t, ts, "/terms")
	mustContain(t, "terms", body, `id="cancellations"`, `id="plans"`, "include VAT", "nearest shilling")
	_, _, body = get(t, ts, "/credits")
	mustContain(t, "credits", body, `Photo by <a href="https://unsplash.com/@jimmydean?utm_source=homend_residence&amp;utm_medium=referral"`,
		`on <a href="https://unsplash.com/?utm_source=homend_residence&amp;utm_medium=referral"`)
	_, _, body = get(t, ts, "/")
	mustContain(t, "home", body, `alt="A family relaxing together on the sofa at home"`, `href="/credits"`, `href="/terms"`)

	_, _, body = get(t, ts, "/services/cleaning")
	mustContain(t, "category", body, "Vetted cleaning in Nairobi", "KES 2,500", "per visit", "Kitchen", "Do I need to supply cleaning products?",
		`"@type":"Service"`, `"@type":"FAQPage"`, `"@type":"BreadcrumbList"`, `href="/services/cleaning/kilimani"`,
		`href="/account/book?category=cleaning"`)

	// The local page shows the area's own price, and says so in its title and structured data.
	_, _, body = get(t, ts, "/services/cleaning/kilimani")
	mustContain(t, "local", body, "<title>Vetted cleaning in Kilimani | Homend Residence</title>", "KES 3,000", `"addressLocality":"Kilimani"`,
		`"price":"3000.00"`, `href="/account/book?area=kilimani&amp;category=cleaning"`)
	if strings.Contains(body, "KES 2,500") {
		t.Error("local page shows the all-areas price instead of the area price")
	}

	// Inactive or unknown things are 404s with the HTML page.
	for _, path := range []string{"/services/pest-control", "/services/cleaning/hidden", "/areas/nowhere", "/nope"} {
		code, _, body := get(t, ts, path)
		if code != 404 || !strings.Contains(body, "find that page") {
			t.Errorf("%s: want HTML 404, got %d", path, code)
		}
	}

	// Choosing an area on /pricing swaps in just the rate card over HTMX.
	code, _, body := get(t, ts, "/pricing?area=kilimani", "HX-Request", "true")
	if code != 200 || strings.Contains(body, "<html") || !strings.Contains(body, `id="rate-card"`) || !strings.Contains(body, "KES 3,000") {
		t.Errorf("pricing fragment: %d\n%s", code, body)
	}
}

func TestSitemapAndRobots(t *testing.T) {
	ts, _ := newSite(t)
	code, h, body := get(t, ts, "/sitemap.xml")
	if code != 200 || !strings.HasPrefix(h.Get("Content-Type"), "application/xml") {
		t.Fatalf("sitemap: %d %v", code, h)
	}
	var set struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal([]byte(body), &set); err != nil {
		t.Fatal(err)
	}
	locs := map[string]bool{}
	for _, u := range set.URLs {
		locs[u.Loc] = true
	}
	for _, want := range []string{"/", "/services/cleaning", "/services/cleaning/kilimani", "/services/cleaning/karen", "/areas/karen", "/pricing"} {
		if !locs["https://homend.test"+want] {
			t.Errorf("sitemap missing %s", want)
		}
	}
	for _, bad := range []string{"/services/pest-control", "/areas/hidden"} {
		if locs["https://homend.test"+bad] {
			t.Errorf("sitemap lists inactive %s", bad)
		}
	}
	_, _, body = get(t, ts, "/robots.txt")
	mustContain(t, "robots", body, "Sitemap: https://homend.test/sitemap.xml", "Disallow: /account", "Disallow: /provider")
}

func TestContactForm(t *testing.T) {
	ts, srv := newSite(t)
	post := func(form url.Values, hdr ...string) (int, string) {
		req, _ := http.NewRequest("POST", ts.URL+"/contact", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	code, body := post(url.Values{"full_name": {"Wanjiru"}})
	if code != 422 || !strings.Contains(body, "Give us a phone number or an email") {
		t.Errorf("missing contact: %d", code)
	}
	code, body = post(url.Values{"full_name": {"Wanjiru"}, "phone": {"12"}}, "HX-Request", "true")
	if code != 200 || strings.Contains(body, "<html") || !strings.Contains(body, "Enter a Kenyan mobile number") || !strings.Contains(body, `value="Wanjiru"`) {
		t.Errorf("bad phone over HTMX: %d\n%s", code, body)
	}
	code, body = post(url.Values{"full_name": {"Wanjiru Kamau"}, "phone": {"0712 345 678"}, "category": {"cleaning"}, "area": {"kilimani"}, "message": {"Weekly clean"}}, "HX-Request", "true")
	if code != 200 || !strings.Contains(body, "Thank you, Wanjiru Kamau.") {
		t.Fatalf("valid lead: %d\n%s", code, body)
	}
	var phone, source, cat, area string
	err := srv.DB.QueryRow(context.Background(), `SELECT l.phone_e164, l.source, c.slug, a.slug FROM leads l JOIN service_categories c ON c.id = l.category_id
		JOIN service_areas a ON a.id = l.service_area_id`).Scan(&phone, &source, &cat, &area)
	if err != nil || phone != "+254712345678" || source != "website" || cat != "cleaning" || area != "kilimani" {
		t.Errorf("stored lead: %v %s %s %s %s", err, phone, source, cat, area)
	}

	req, _ := http.NewRequest("POST", ts.URL+"/contact", strings.NewReader("full_name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site post: %d", res.StatusCode)
	}
}

func TestAreaPagesOnlyWherePriced(t *testing.T) {
	ts, _ := newSite(t)
	if code, _, _ := get(t, ts, "/services/gardening/kilimani"); code != 200 {
		t.Fatalf("priced area: %d", code)
	}
	if code, _, _ := get(t, ts, "/services/gardening/karen"); code != 404 {
		t.Fatalf("an area without a gardening price should have no page: %d", code)
	}
	_, _, body := get(t, ts, "/services/gardening")
	mustContain(t, "gardening", body, `href="/services/gardening/kilimani"`)
	if strings.Contains(body, `href="/services/gardening/karen"`) {
		t.Error("gardening links to its empty Karen page")
	}
	_, _, body = get(t, ts, "/areas/karen")
	mustContain(t, "karen", body, `href="/services/cleaning/karen"`)
	if strings.Contains(body, "/services/gardening/karen") {
		t.Error("Karen lists gardening, which has no price there")
	}
	_, _, body = get(t, ts, "/sitemap.xml")
	mustContain(t, "sitemap", body, "/services/gardening/kilimani", "/services/cleaning/karen")
	if strings.Contains(body, "/services/gardening/karen") {
		t.Error("sitemap lists an unpriced area page")
	}
}

func TestRenamedSlugsRedirect(t *testing.T) {
	ts, srv := newSite(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO service_areas (slug, name, is_active) VALUES ('kasa','Kasa',true)`,
		`UPDATE service_areas SET slug = 'kasarani', name = 'Kasarani' WHERE slug = 'kasa'`,
		`UPDATE service_categories SET slug = 'home-cleaning' WHERE slug = 'cleaning'`,
	} {
		if _, err := srv.DB.Exec(ctx, q); err != nil {
			t.Fatal(q, err)
		}
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	location := func(path string) (int, string) {
		t.Helper()
		res, err := noFollow.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode, res.Header.Get("Location")
	}
	for path, want := range map[string]string{
		"/areas/kasa":                          "/areas/kasarani",
		"/services/home-cleaning/kasa?utm=sms": "/services/home-cleaning/kasarani?utm=sms",
		"/services/cleaning":                   "/services/home-cleaning",
		"/services/cleaning/kilimani":          "/services/home-cleaning/kilimani",
	} {
		if code, loc := location(path); code != http.StatusMovedPermanently || loc != want {
			t.Errorf("%s: %d to %q, want 301 to %q", path, code, loc, want)
		}
	}
	if code, _, _ := get(t, ts, "/areas/kasarani"); code != 200 {
		t.Errorf("new slug: %d", code)
	}
	if code, _ := location("/areas/nowhere"); code != 404 {
		t.Errorf("unknown slug: %d", code)
	}

	// A second rename points the first old slug straight at the newest one, and a slug taken
	// back is a page again rather than a redirect.
	if _, err := srv.DB.Exec(ctx, `UPDATE service_areas SET slug = 'kasarani-east' WHERE slug = 'kasarani'`); err != nil {
		t.Fatal(err)
	}
	if code, loc := location("/areas/kasa"); code != http.StatusMovedPermanently || loc != "/areas/kasarani-east" {
		t.Errorf("chain: %d to %q", code, loc)
	}
	if _, err := srv.DB.Exec(ctx, `UPDATE service_areas SET slug = 'kasa' WHERE slug = 'kasarani-east'`); err != nil {
		t.Fatal(err)
	}
	if code, _ := location("/areas/kasa"); code != 200 {
		t.Errorf("reused slug: %d", code)
	}
	if code, loc := location("/areas/kasarani"); code != http.StatusMovedPermanently || loc != "/areas/kasa" {
		t.Errorf("after moving back: %d to %q", code, loc)
	}
}

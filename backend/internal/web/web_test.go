package web

import (
	"compress/gzip"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func newSite(t *testing.T) *Site {
	t.Helper()
	s, err := New(Options{BaseURL: "https://example.test/"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMoney(t *testing.T) {
	for in, want := range map[int64]string{0: "KES 0", 250000: "KES 2,500", 123456789: "KES 1,234,567.89", -5000: "-KES 50"} {
		if got := Money(in); got != want {
			t.Errorf("Money(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestAssetAndStatic(t *testing.T) {
	s := newSite(t)
	u := s.Asset("css/app.css")
	if !strings.HasPrefix(u, "/static/css/app.css?v=") {
		t.Fatalf("asset url %q", u)
	}
	rec := httptest.NewRecorder()
	s.StaticHandler().ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("hashed asset: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	rec = httptest.NewRecorder()
	s.StaticHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/static/css/", nil))
	if rec.Code != 404 {
		t.Fatalf("directory listing should 404, got %d", rec.Code)
	}
}

func TestStaticGzip(t *testing.T) {
	s := newSite(t)
	u := s.Asset("js/htmx.min.js")
	req := httptest.NewRequest("GET", u, nil)
	req.Header.Set("Accept-Encoding", "br, gzip;q=0.8")
	rec := httptest.NewRecorder()
	s.StaticHandler().ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("headers %v", rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	if !strings.Contains(string(b), "htmx") {
		t.Fatal("gzipped body does not decode to the file")
	}

	req = httptest.NewRequest("GET", u, nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0")
	rec = httptest.NewRecorder()
	s.StaticHandler().ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "" || !strings.Contains(rec.Body.String(), "htmx") {
		t.Fatal("gzip;q=0 must get the plain file")
	}
}

func TestPhotos(t *testing.T) {
	s := newSite(t)
	hero := s.Photo("hero")
	if hero == nil || hero.Narrow == nil || hero.AVIF == "" || hero.WebP == "" || hero.Alt == "" {
		t.Fatalf("hero photo not indexed: %+v", hero)
	}
	html := string(hero.Img("100vw", "x", true))
	for _, want := range []string{`<picture>`, `type="image/avif"`, `type="image/webp"`, `media="(max-width: 767px)"`, `fetchpriority="high"`, `alt="` + hero.Alt + `"`, `.jpg?v=`} {
		if !strings.Contains(html, want) {
			t.Errorf("hero markup lacks %s:\n%s", want, html)
		}
	}
	if lazy := string(s.Photo("laundry").Img("100px", "", false)); !strings.Contains(lazy, `loading="lazy"`) {
		t.Errorf("non-hero photos load lazily: %s", lazy)
	}
	for _, c := range s.PhotoCredits() {
		if s.Photo(c.Name) == nil || c.Photographer == "" || !strings.HasPrefix(c.Profile, "https://unsplash.com/@") || c.Alt == "" {
			t.Errorf("credit %+v: missing photo or fields", c)
		}
		if !strings.HasSuffix(c.ProfileURL(), "?utm_source=homend_residence&utm_medium=referral") {
			t.Errorf("profile link %q lacks the referral", c.ProfileURL())
		}
	}
	for name := range s.photos {
		if strings.HasSuffix(name, ".narrow") {
			continue
		}
		found := false
		for _, c := range s.PhotoCredits() {
			found = found || c.Name == name
		}
		if !found {
			t.Errorf("photo %q has no entry in credits.json", name)
		}
	}
}

func TestNotFoundPage(t *testing.T) {
	s := newSite(t)
	rec := httptest.NewRecorder()
	s.NotFound(rec, httptest.NewRequest("GET", "/nope", nil))
	body := rec.Body.String()
	if rec.Code != 404 || !strings.Contains(body, "find that page") || !strings.Contains(body, `content="noindex, nofollow"`) {
		t.Fatalf("404 page: %d\n%s", rec.Code, body)
	}
	rec = httptest.NewRecorder()
	s.NotFound(rec, httptest.NewRequest("GET", "/api/v1/nope", nil))
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("api 404 should be JSON, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestMiddlewareRejectsCrossSitePost(t *testing.T) {
	s := newSite(t)
	h := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	req := httptest.NewRequest("POST", "/contact", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST: %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/contact", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("same-origin POST: %d", rec.Code)
	}
}

func TestCSPAllowsStorageOrigin(t *testing.T) {
	const vault = "https://vault.s3.af-south-1.amazonaws.com"
	s, err := New(Options{BaseURL: "https://homend.test", StorageOrigin: vault})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self' "+vault+";") || !strings.Contains(csp, "img-src 'self' data: "+vault+";") {
		t.Fatalf("presigned uploads would be blocked: %s", csp)
	}
	if strings.Contains(newSite(t).csp, "amazonaws") {
		t.Fatal("local storage should keep the policy same-origin")
	}
}

func TestPageTemplatesExtendLayout(t *testing.T) {
	s := newSite(t)
	tm, err := s.Templates(files, "templates/*.html") // base only: proves Clone+ParseFS works
	if err != nil {
		t.Fatal(err)
	}
	tm, err = tm.Parse(`{{define "content"}}<p>hello {{.Data}}</p>{{end}}{{define "head"}}<link rel="manifest" href="/m.json">{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Render(rec, httptest.NewRequest("GET", "/x", nil), tm, 200, &Page{Title: "T", Data: "world", JSONLD: []any{map[string]string{"@type": "Thing", "name": "</script>"}}})
	b := rec.Body.String()
	for _, want := range []string{"<p>hello world</p>", `rel="manifest"`, `<link rel="canonical" href="https://example.test/x">`, `<title>T | Homend Residence</title>`, `</script>`} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %q in\n%s", want, b)
		}
	}
}

func TestTemplatesWithFuncs(t *testing.T) {
	s := newSite(t)
	page := fstest.MapFS{"p.html": {Data: []byte(`{{define "content"}}<p>{{shout .Data}}</p>{{end}}`)}}
	tm, err := s.TemplatesWithFuncs(template.FuncMap{"shout": func(v any) string { return "HI " + v.(string) }}, page, "p.html")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Render(rec, httptest.NewRequest("GET", "/x", nil), tm, 200, &Page{Title: "T", Data: "there"})
	if !strings.Contains(rec.Body.String(), "<p>HI there</p>") {
		t.Fatalf("custom func not applied:\n%s", rec.Body.String())
	}
	if _, err := s.Templates(page, "p.html"); err == nil {
		t.Fatal("the base must not gain the surface's funcs")
	}
}

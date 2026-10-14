package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

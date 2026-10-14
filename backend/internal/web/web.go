// Package web is the shared foundation for the server-rendered surfaces (ADR-0001): the public
// site, the client portal and the provider portal. It owns the base layout, the Tailwind build
// output, static assets (HTMX, icons) and the middleware every HTML route runs behind.
//
// A surface keeps its own templates in its own package and builds them on top of the base layout:
//
//	t, err := site.Templates(myFS, "templates/*.html")
//	site.Render(w, r, t, http.StatusOK, &web.Page{Title: "Bookings", Data: rows})
//
// Page templates define these blocks (all optional except "content"):
//
//	"title"   the <title> text; defaults to Page.Title + " | Homend Residence"
//	"head"    extra tags in <head>: manifest link, theme-color, page CSS
//	"header"  the top bar; defaults to the public site navigation
//	"content" the page body inside <main>
//	"footer"  defaults to the public site footer
//	"scripts" extra scripts at the end of <body> (same-origin only: the CSP blocks inline script)
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

//go:embed templates static
var files embed.FS

// Paths of the HTML surfaces, so the public site and the portals link to each other consistently.
const (
	ClientPortalPath   = "/account"
	ProviderPortalPath = "/provider"
	JoinPath           = "/join" // provider recruitment and application
)

const SiteName = "Homend Residence"

type Options struct {
	// BaseURL is the public origin (no trailing slash) used for canonical links, the sitemap and
	// structured data, e.g. https://homend.co.ke.
	BaseURL string
}

type Site struct {
	BaseURL string
	base    *template.Template
	static  fs.FS
	hashes  map[string]string // static path -> content hash, for cache-busting URLs
}

// Page is the data every full-page render receives. Templates read Data for their own content.
type Page struct {
	Title       string
	Description string // meta description; keep it under ~155 characters
	Path        string // canonical path, e.g. /services/cleaning; empty means the request path
	NoIndex     bool   // portals and form results set this
	Section     string // highlights the matching nav item
	OGType      string // defaults to "website"
	JSONLD      []any  // schema.org objects, written as application/ld+json
	Data        any

	Site *Site // set by Render
	URL  string
}

func New(o Options) (*Site, error) {
	static, err := fs.Sub(files, "static")
	if err != nil {
		return nil, err
	}
	s := &Site{BaseURL: strings.TrimRight(o.BaseURL, "/"), static: static, hashes: map[string]string{}}
	err = fs.WalkDir(static, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(static, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		s.hashes[p] = hex.EncodeToString(sum[:5])
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.base, err = template.New("base").Funcs(s.funcs()).ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Asset returns the cache-busted URL of a file under static/, e.g. Asset("css/app.css").
func (s *Site) Asset(p string) string {
	h, ok := s.hashes[p]
	if !ok {
		slog.Warn("unknown static asset", "path", p)
		return "/static/" + p
	}
	return "/static/" + p + "?v=" + h
}

// AbsURL makes a site path absolute against BaseURL.
func (s *Site) AbsURL(p string) string {
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	return s.BaseURL + p
}

// Templates clones the base layout and parses a surface's own templates on top of it.
func (s *Site) Templates(fsys fs.FS, patterns ...string) (*template.Template, error) {
	t, err := s.base.Clone()
	if err != nil {
		return nil, err
	}
	return t.ParseFS(fsys, patterns...)
}

// Render executes the full layout. Rendering goes to a buffer first so a template error
// becomes a clean 500 instead of half a page.
func (s *Site) Render(w http.ResponseWriter, r *http.Request, t *template.Template, status int, p *Page) {
	p.Site = s
	if p.Path == "" {
		p.Path = r.URL.Path
	}
	p.URL = s.AbsURL(p.Path)
	s.execute(w, r, t, "layout", status, p)
}

// RenderBlock executes one named template, for HTMX fragment responses.
func (s *Site) RenderBlock(w http.ResponseWriter, r *http.Request, t *template.Template, name string, status int, data any) {
	s.execute(w, r, t, name, status, data)
}

func (s *Site) execute(w http.ResponseWriter, r *http.Request, t *template.Template, name string, status int, data any) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		slog.ErrorContext(r.Context(), "template", "err", err, "template", name, "request_id", httpx.RequestID(r.Context()))
		http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// Cacheable marks a response as shareable by CloudFront and browsers. Only use it on pages that
// look the same for every visitor.
func Cacheable(w http.ResponseWriter, maxAge time.Duration) {
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(maxAge.Seconds())))
}

// IsHTMX reports whether the request came from HTMX (and so wants a fragment).
func IsHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func (s *Site) funcs() template.FuncMap {
	return template.FuncMap{
		"asset":    s.Asset,
		"absURL":   s.AbsURL,
		"siteName": func() string { return SiteName },
		"year":     func() int { return time.Now().Year() },
		"money":    Money,
		"jsonld": func(v any) (template.HTML, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			// json.Marshal escapes <, > and & so the payload cannot close the script element.
			return template.HTML(`<script type="application/ld+json">` + string(b) + `</script>`), nil
		},
		"clientPath":   func() string { return ClientPortalPath },
		"providerPath": func() string { return ProviderPortalPath },
		"joinPath":     func() string { return JoinPath },
		"deref": func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		},
	}
}

// Money formats integer minor units (cents) as "KES 2,500" (or "KES 2,500.50" when there are cents).
func Money(minor int64, currency ...string) string {
	cur := "KES"
	if len(currency) > 0 && currency[0] != "" {
		cur = currency[0]
	}
	neg := minor < 0
	if neg {
		minor = -minor
	}
	whole, cents := minor/100, minor%100
	digits := fmt.Sprintf("%d", whole)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := cur + " " + b.String()
	if cents != 0 {
		out += fmt.Sprintf(".%02d", cents)
	}
	if neg {
		out = "-" + out
	}
	return out
}

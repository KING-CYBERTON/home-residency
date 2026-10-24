package web

import (
	"bytes"
	"compress/gzip"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// contentSecurityPolicy allows same-origin resources, plus the document vault's origin for the
// browser's direct presigned uploads and downloads. HTMX is configured (in the layout) not to inject
// inline styles or use eval, so no 'unsafe-*' source is needed.
func contentSecurityPolicy(storageOrigin string) string {
	extra := ""
	if storageOrigin != "" {
		extra = " " + storageOrigin
	}
	return "default-src 'self'; img-src 'self' data:" + extra + "; style-src 'self'; script-src 'self'; " +
		"connect-src 'self'" + extra + "; font-src 'self'; manifest-src 'self'; worker-src 'self'; " +
		"frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"
}

// Middleware wraps every HTML route: security headers, and protection against cross-site form
// posts (the portals authenticate with a cookie). http.CrossOriginProtection rejects unsafe
// requests whose Sec-Fetch-Site or Origin header shows they came from another site, so no
// per-form token is needed. The global middleware already sets Cache-Control: no-store; public
// pages opt in to caching with Cacheable.
func (s *Site) Middleware(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "This form was sent from another site and was not accepted.", http.StatusForbidden)
	}))
	return cop.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", s.csp)
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(self), geolocation=(self), microphone=()")
		h.Set("Vary", "HX-Request")
		next.ServeHTTP(w, r)
	}))
}

// compressible are the static file types worth sending gzipped. Images and fonts are already
// compressed.
var compressible = map[string]bool{".css": true, ".js": true, ".svg": true, ".json": true, ".txt": true}

// gzipStatic compresses the text files under static/ once, at startup, so StaticHandler can send
// them gzipped (the load balancer in front of the app does not compress). HTML pages are left
// uncompressed: they can carry secrets next to reflected input.
func gzipStatic(name string, b []byte) []byte {
	if !compressible[path.Ext(name)] {
		return nil
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(b)
	_ = zw.Close()
	if buf.Len() >= len(b) {
		return nil
	}
	return buf.Bytes()
}

// acceptsGzip reports whether the request's Accept-Encoding allows gzip.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if name != "gzip" && name != "*" {
			continue
		}
		q := strings.ReplaceAll(params, " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

// StaticHandler serves files under /static/, gzipped when the browser accepts it. A request
// carrying the current content hash (as produced by Asset) is cached for a year; anything else
// for an hour.
func (s *Site) StaticHandler() http.Handler {
	files := http.StripPrefix("/static/", http.FileServerFS(s.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/static/")
		if strings.HasSuffix(p, "/") || p == "" {
			http.NotFound(w, r)
			return
		}
		if v := r.URL.Query().Get("v"); v != "" && v == s.hashes[p] {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		if gz := s.gzipped[p]; gz != nil {
			h := w.Header()
			h.Add("Vary", "Accept-Encoding")
			if acceptsGzip(r) {
				h.Set("Content-Encoding", "gzip")
				h.Set("Content-Type", mime.TypeByExtension(path.Ext(p)))
				http.ServeContent(w, r, p, time.Time{}, bytes.NewReader(gz))
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}

// NotFound answers unknown paths: the JSON error envelope under /api/, the HTML 404 page elsewhere.
func (s *Site) NotFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/webhooks/") {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	w.Header().Set("Content-Security-Policy", s.csp)
	// The base layout's default "content" block is the 404 body.
	s.Render(w, r, s.base, http.StatusNotFound, &Page{Title: "Page not found", NoIndex: true})
}

package client

import (
	"net/http"
	"strings"
)

// The portal installs as a PWA (ADR-0001): a manifest, a service worker scoped to Base, and an
// offline page. Bookings and payments need the network, so the worker only keeps the shell and the
// last pages seen; it never caches form posts or API answers.

func (p *Portal) manifest(w http.ResponseWriter, r *http.Request) {
	b := p.Base
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(`{
  "name": "Homend Residence",
  "short_name": "Homend",
  "description": "Book vetted cleaners, nannies and home help, pay by M-Pesa and track every visit.",
  "id": "` + b + `/",
  "start_url": "` + b + `/?source=pwa",
  "scope": "` + b + `/",
  "display": "standalone",
  "orientation": "portrait",
  "background_color": "#ffffff",
  "theme_color": "#0f766e",
  "lang": "en-KE",
  "categories": ["lifestyle", "productivity"],
  "icons": [
    {"src": "` + p.Site.Asset("img/icon-192.png") + `", "sizes": "192x192", "type": "image/png"},
    {"src": "` + p.Site.Asset("img/icon-512.png") + `", "sizes": "512x512", "type": "image/png"},
    {"src": "` + b + `/static/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"}
  ],
  "shortcuts": [
    {"name": "Book a service", "url": "` + b + `/book"},
    {"name": "My bookings", "url": "` + b + `/requests"},
    {"name": "Payments", "url": "` + b + `/invoices"}
  ]
}`))
}

func (p *Portal) serviceWorker(w http.ResponseWriter, r *http.Request) {
	src, err := assets.ReadFile("static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(strings.ReplaceAll(string(src), "__BASE__", p.Base)))
}

func (p *Portal) offline(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, "offline", &view{Title: "You are offline"})
}

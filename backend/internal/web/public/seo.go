package public

import (
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
)

// Structured data (schema.org JSON-LD) for local-service search results.

type crumb struct{ Name, Path string }

func breadcrumbs(s *web.Site, cs ...crumb) map[string]any {
	items := make([]map[string]any, 0, len(cs))
	for i, c := range cs {
		items = append(items, map[string]any{"@type": "ListItem", "position": i + 1, "name": c.Name, "item": s.AbsURL(c.Path)})
	}
	return map[string]any{"@context": "https://schema.org", "@type": "BreadcrumbList", "itemListElement": items}
}

func places(areas []api.ServiceArea) []map[string]any {
	out := make([]map[string]any, 0, len(areas))
	for _, a := range areas {
		out = append(out, place(a))
	}
	return out
}

func place(a api.ServiceArea) map[string]any {
	p := map[string]any{"@type": "Place", "name": a.Name + ", " + a.County,
		"address": map[string]any{"@type": "PostalAddress", "addressLocality": a.Name, "addressRegion": a.County, "addressCountry": "KE"}}
	if a.CentreLat != nil && a.CentreLng != nil {
		p["geo"] = map[string]any{"@type": "GeoCoordinates", "latitude": *a.CentreLat, "longitude": *a.CentreLng}
	}
	return p
}

func (p *Site) organization(areas []api.ServiceArea) map[string]any {
	return map[string]any{
		"@context": "https://schema.org", "@type": "HomeAndConstructionBusiness", "@id": p.site.AbsURL("/#organization"),
		"name": web.SiteName, "url": p.site.AbsURL("/"), "logo": p.site.AbsURL(p.site.Asset("img/icon-512.png")),
		"image":      p.site.AbsURL(p.site.Asset("img/og.png")),
		"address":    map[string]any{"@type": "PostalAddress", "addressLocality": "Nairobi", "addressCountry": "KE"},
		"areaServed": places(areas), "priceRange": "KES",
	}
}

func (p *Site) website() map[string]any {
	return map[string]any{"@context": "https://schema.org", "@type": "WebSite", "name": web.SiteName, "url": p.site.AbsURL("/"),
		"publisher": map[string]any{"@id": p.site.AbsURL("/#organization")}}
}

// serviceLD describes a category (optionally in one area) as a Service with an OfferCatalog of the
// rate-card prices. Prices are shown before VAT, matching the page.
func (p *Site) serviceLD(cat api.Category, area *api.ServiceArea, all []api.ServiceArea, offers []Offer, path string) map[string]any {
	items := []map[string]any{}
	for _, o := range offers {
		item := map[string]any{"@type": "Offer", "itemOffered": map[string]any{"@type": "Service", "name": o.Name}}
		if o.Price != nil {
			item["priceSpecification"] = map[string]any{"@type": "UnitPriceSpecification", "price": fmt.Sprintf("%.2f", float64(o.Price.ClientPriceMinor)/100),
				"priceCurrency": o.Price.Currency, "unitText": o.UnitLabel, "valueAddedTaxIncluded": false}
		}
		items = append(items, item)
	}
	ld := map[string]any{
		"@context": "https://schema.org", "@type": "Service", "name": cat.Name, "serviceType": cat.Name, "url": p.site.AbsURL(path),
		"provider":         map[string]any{"@id": p.site.AbsURL("/#organization"), "@type": "HomeAndConstructionBusiness", "name": web.SiteName},
		"hasOfferCatalog":  map[string]any{"@type": "OfferCatalog", "name": cat.Name, "itemListElement": items},
		"availableChannel": map[string]any{"@type": "ServiceChannel", "serviceUrl": p.site.AbsURL("/book")},
	}
	if cat.Description != nil {
		ld["description"] = *cat.Description
	}
	if area != nil {
		ld["areaServed"] = place(*area)
	} else {
		ld["areaServed"] = places(all)
	}
	return ld
}

func faqPage(faqs []api.FAQ) map[string]any {
	qs := make([]map[string]any, 0, len(faqs))
	for _, f := range faqs {
		qs = append(qs, map[string]any{"@type": "Question", "name": f.Question, "acceptedAnswer": map[string]any{"@type": "Answer", "text": f.Answer}})
	}
	return map[string]any{"@context": "https://schema.org", "@type": "FAQPage", "mainEntity": qs}
}

// ---- sitemap and robots

type urlset struct {
	XMLName xml.Name `xml:"urlset"`
	NS      string   `xml:"xmlns,attr"`
	URLs    []sitemapURL
}

type sitemapURL struct {
	XMLName  xml.Name `xml:"url"`
	Loc      string   `xml:"loc"`
	Priority string   `xml:"priority,omitempty"`
}

// sitemapPaths lists every indexable page: the fixed pages, each active category, area, and
// category-in-area landing page.
func (p *Site) sitemapPaths(c catalogue) []sitemapURL {
	var out []sitemapURL
	add := func(path, prio string) { out = append(out, sitemapURL{Loc: p.site.AbsURL(path), Priority: prio}) }
	add("/", "1.0")
	for _, path := range []string{"/services", "/areas", "/pricing", "/how-it-works", "/faq", "/book", "/contact", web.JoinPath} {
		add(path, "0.8")
	}
	for _, cat := range c.Categories {
		add("/services/"+cat.Slug, "0.9")
		for _, a := range c.Areas {
			add("/services/"+cat.Slug+"/"+a.Slug, "0.7")
		}
	}
	for _, a := range c.Areas {
		add("/areas/"+a.Slug, "0.6")
	}
	add("/privacy", "0.2")
	return out
}

func (p *Site) sitemap(w http.ResponseWriter, r *http.Request) {
	c, err := p.catalogue(r.Context())
	if err != nil {
		p.fail(w, r, err)
		return
	}
	b, err := xml.MarshalIndent(urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: p.sitemapPaths(c)}, "", "  ")
	if err != nil {
		p.fail(w, r, err)
		return
	}
	web.Cacheable(w, cacheFor)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(b)
}

func (p *Site) robots(w http.ResponseWriter, r *http.Request) {
	web.Cacheable(w, cacheFor)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /api/\nDisallow: %s\nDisallow: %s\n\nSitemap: %s\n",
		web.ClientPortalPath, web.ProviderPortalPath, p.site.AbsURL("/sitemap.xml"))
}

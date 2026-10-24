package public

import (
	"html/template"
	"strings"
)

// Photographs live under internal/web/static/img/photos/ (see web.Photo). Pages fall back to a
// plain panel when a photo is missing, so a category added in the console never breaks the site.

// servicePhoto names the photo for a service category: service-<slug>.
func servicePhoto(slug string) string { return "service-" + slug }

// offerPhotos names the photo for each service on a rate card. A service that isn't listed has
// no photo of its own and shows its category's line drawing.
var offerPhotos = map[string]string{
	"standard-clean-1br":  "service-cleaning",
	"standard-clean-3br":  "service-cleaning",
	"deep-clean":          "deep-clean",
	"move-out-clean":      "deep-clean",
	"live-in-general":     "service-live-in-house-help",
	"live-in-nanny":       "nanny",
	"day-worker-full-day": "service-day-worker",
	"laundry-ironing":     "laundry",
	"plumbing-callout":    "service-plumbing",
	"plumbing-hourly":     "service-plumbing",
	"electrical-callout":  "service-electrical",
	"elderly-care-day":    "service-caregiver",
}

func offerPhoto(slug string) string { return offerPhotos[slug] }

// orgImages lists the organisation's images for structured data, best first.
func (p *Site) orgImages() []string {
	out := []string{}
	for _, name := range []string{"hero", "deep-clean"} {
		if ph := p.site.Photo(name); ph != nil {
			out = append(out, ph.Abs)
		}
	}
	return append(out, p.site.AbsURL(p.site.Asset("img/og.png")))
}

// serviceIcon is the line drawing shown in place of a category's photo until one is added.
func serviceIcon(slug string) template.HTML {
	d := "M3 11l9-7 9 7v9a1 1 0 01-1 1h-5v-6h-6v6H4a1 1 0 01-1-1z" // a house
	for _, k := range []struct{ word, path string }{
		{"clean", "M9 3h5v3H9zM14 4h3l1 2M10 6v2M8 8h7l1 13H7zM10 13h3"},
		{"laundry", "M5 3h14v18H5zM5 7h14M12 17a4 4 0 100-8 4 4 0 000 8z"},
		{"plumb", "M4 10h6V6h4v4h2a4 4 0 014 4v2h-4v-2H4zM18 19a1.5 1.5 0 01-3 0c0-1 1.5-3 1.5-3s1.5 2 1.5 3z"},
		{"electr", "M13 2L4 14h7l-1 8 9-12h-7z"},
		{"care", "M12 21s-7-4.5-9-9a5 5 0 019-3 5 5 0 019 3c-2 4.5-9 9-9 9z"},
		{"nanny", "M12 7a3 3 0 100-6 3 3 0 000 6zM6 22v-6l-2-5h16l-2 5v6M9 22v-4h6v4"},
		{"day", "M4 5h16v16H4zM4 10h16M9 3v4m6-4v4M8 14h3v3H8z"},
		{"pest", "M12 8a3 3 0 00-3 3v4a3 3 0 006 0v-4a3 3 0 00-3-3zM9 6l-2-2m8 2l2-2M9 12H5m10 0h4M9 16l-3 2m9-2l3 2"},
		{"garden", "M12 21v-9m0 0c0-4 3-7 7-7 0 4-3 7-7 7zm0 0C12 8 9 5 5 5c0 4 3 7 7 7"},
	} {
		if strings.Contains(slug, k.word) {
			d = k.path
			break
		}
	}
	return template.HTML(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="0.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" class="h-24 w-24"><path d="` + d + `"/></svg>`)
}

var templateFuncs = template.FuncMap{"servicePhoto": servicePhoto, "offerPhoto": offerPhoto, "serviceIcon": serviceIcon}

// Package public is the marketing and SEO site (ADR-0001): home, service and local area pages,
// pricing from the live rate card, enquiries, the sitemap and structured data. Every page is the
// same for every visitor, so it is cacheable at CloudFront.
package public

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
)

//go:embed templates
var templates embed.FS

// cacheFor is how long CloudFront and browsers may keep a public page. Rate-card changes show
// within this window.
const cacheFor = 5 * time.Minute

var pageNames = []string{"home", "services", "category", "areas", "area", "pricing", "how-it-works", "privacy", "terms", "faq", "book", "contact", "credits", "sign-in"}

type Site struct {
	srv   *api.Server
	site  *web.Site
	t     *template.Template            // all public templates, for fragments
	pages map[string]*template.Template // one per page, with "content" bound to "<name>-content"
}

func New(srv *api.Server, site *web.Site) (*Site, error) {
	t, err := site.TemplatesWithFuncs(templateFuncs, templates, "templates/*.html")
	if err != nil {
		return nil, err
	}
	p := &Site{srv: srv, site: site, t: t, pages: map[string]*template.Template{}}
	for _, name := range pageNames {
		// Each page gets its own clone so the "content" definitions don't overwrite each other.
		c, err := t.Clone()
		if err == nil {
			c, err = c.Parse(`{{define "content"}}{{template "` + name + `-content" .}}{{end}}`)
		}
		if err != nil {
			return nil, err
		}
		p.pages[name] = c
	}
	return p, nil
}

// Routes registers the public pages on r, which is expected to run web.Site.Middleware.
func (p *Site) Routes(r chi.Router) {
	r.Get("/", p.home)
	r.Get("/services", p.services)
	r.Get("/services/{category}", p.category)
	r.Get("/services/{category}/{area}", p.categoryInArea)
	r.Get("/areas", p.areas)
	r.Get("/areas/{area}", p.area)
	r.Get("/pricing", p.pricing)
	r.Get("/how-it-works", p.static("how-it-works", "How it works", "how",
		"Every Homend Residence provider is vetted, trained and supervised. See how we check providers, match them to your request and handle complaints."))
	r.Get("/privacy", p.static("privacy", "Privacy", "",
		"How Homend Residence collects, protects and uses personal data under Kenya's Data Protection Act, 2019."))
	r.Get("/terms", p.static("terms", "Terms of service", "",
		"The terms for booking Homend Residence services and household plans: prices and VAT, payment, cancellations, refunds and complaints."))
	r.Get("/credits", p.static("credits", "Photo credits", "",
		"The photographers whose work appears on the Homend Residence website."))
	r.Get("/sign-in", p.static("sign-in", "Sign in", "",
		"Sign in to Homend Residence: households to book and manage visits, providers to see jobs, earnings and documents."))
	r.Get("/faq", p.faq)
	r.Get("/book", p.book)
	r.Get("/contact", p.contact)
	r.Post("/contact", p.contactSubmit)
	r.Get("/sitemap.xml", p.sitemap)
	r.Get("/robots.txt", p.robots)
}

// ---- view models

// Offer is a service with its price, ready for display.
type Offer struct {
	api.Service
	Price     *api.RateCardRate
	UnitLabel string
}

type catalogue struct {
	Categories []api.Category
	Areas      []api.ServiceArea
	Priced     map[uuid.UUID]map[uuid.UUID]bool // category -> areas where it has a current price
}

func (p *Site) catalogue(ctx context.Context) (catalogue, error) {
	cats, err := p.srv.PublicCategories(ctx)
	if err != nil {
		return catalogue{}, err
	}
	areas, err := p.srv.PublicAreas(ctx)
	if err != nil {
		return catalogue{}, err
	}
	priced, err := p.srv.PublicPricedAreas(ctx)
	return catalogue{Categories: cats, Areas: areas, Priced: priced}, err
}

// pricedAreas are the areas with a price for the category: the only ones that get a
// /services/{category}/{area} page.
func (c catalogue) pricedAreas(catID uuid.UUID) []api.ServiceArea {
	out := []api.ServiceArea{}
	for _, a := range c.Areas {
		if c.Priced[catID][a.ID] {
			out = append(out, a)
		}
	}
	return out
}

// pricedCategories are the categories with a price in the area.
func (c catalogue) pricedCategories(areaID uuid.UUID) []api.Category {
	out := []api.Category{}
	for _, cat := range c.Categories {
		if c.Priced[cat.ID][areaID] {
			out = append(out, cat)
		}
	}
	return out
}

// redirectRenamed sends a request for a renamed area or category slug to its page under the new
// slug with a 301, keeping the query string. It reports whether it did.
func (p *Site) redirectRenamed(w http.ResponseWriter, r *http.Request, kind, slug string, to func(newSlug string) string) bool {
	next, err := p.srv.PublicSlugRedirect(r.Context(), kind, slug)
	if err != nil || next == "" {
		return false
	}
	u := to(next)
	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, u, http.StatusMovedPermanently)
	return true
}

func (c catalogue) category(slug string) (api.Category, bool) {
	for _, x := range c.Categories {
		if x.Slug == slug {
			return x, true
		}
	}
	return api.Category{}, false
}

func (c catalogue) area(slug string) (api.ServiceArea, bool) {
	for _, x := range c.Areas {
		if x.Slug == slug {
			return x, true
		}
	}
	return api.ServiceArea{}, false
}

func (p *Site) offers(ctx context.Context, categoryID, areaID *uuid.UUID) ([]Offer, error) {
	svcs, err := p.srv.PublicServices(ctx, categoryID, areaID)
	if err != nil {
		return nil, err
	}
	out := make([]Offer, 0, len(svcs))
	for _, s := range svcs {
		out = append(out, Offer{Service: s, Price: s.CurrentPrice, UnitLabel: unitLabel(s.PricingUnit)})
	}
	return out, nil
}

func unitLabel(u string) string {
	switch u {
	case "per_job":
		return "per visit"
	case "per_hour":
		return "per hour"
	case "per_day":
		return "per day"
	case "per_month":
		return "per month"
	case "per_room":
		return "per room"
	}
	return strings.ReplaceAll(u, "_", " ")
}

// lowestPrice is the cheapest current price among offers, for "from KES …" labels.
func lowestPrice(offers []Offer) *api.RateCardRate {
	var lo *api.RateCardRate
	for _, o := range offers {
		if o.Price != nil && (lo == nil || o.Price.ClientPriceMinor < lo.ClientPriceMinor) {
			lo = o.Price
		}
	}
	return lo
}

// ---- handlers

func (p *Site) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := p.catalogue(ctx)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	type card struct {
		api.Category
		From *api.RateCardRate
	}
	cards := make([]card, 0, len(c.Categories))
	for _, cat := range c.Categories {
		id := cat.ID
		offers, err := p.offers(ctx, &id, nil)
		if err != nil {
			p.fail(w, r, err)
			return
		}
		cards = append(cards, card{Category: cat, From: lowestPrice(offers)})
	}
	faqs, err := p.srv.PublicFAQs(ctx, nil, "en")
	if err != nil {
		p.fail(w, r, err)
		return
	}
	general := faqs[:0]
	for _, f := range faqs {
		if f.CategoryID == nil && len(general) < 4 {
			general = append(general, f)
		}
	}
	p.render(w, r, &web.Page{
		Title:       "Vetted home services in Nairobi",
		Description: "Book vetted, trained and supervised cleaners, domestic workers, caregivers, plumbers and electricians in Nairobi. Fixed prices, managed by our team.",
		Path:        "/",
		Section:     "home",
		Image:       p.site.Photo("hero"),
		JSONLD:      []any{p.organization(c.Areas, c.Categories), p.website()},
		Data:        map[string]any{"Categories": cards, "Areas": c.Areas, "FAQs": general},
	}, "home")
}

func (p *Site) services(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := p.catalogue(ctx)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	type group struct {
		Category api.Category
		Offers   []Offer
	}
	groups := make([]group, 0, len(c.Categories))
	for _, cat := range c.Categories {
		id := cat.ID
		offers, err := p.offers(ctx, &id, nil)
		if err != nil {
			p.fail(w, r, err)
			return
		}
		groups = append(groups, group{Category: cat, Offers: offers})
	}
	p.render(w, r, &web.Page{
		Title:       "Home services",
		Description: "Cleaning, domestic workers, caregivers and home repairs in Nairobi, each delivered by a vetted provider at a fixed rate-card price.",
		Section:     "services",
		JSONLD:      []any{breadcrumbs(p.site, crumb{"Home", "/"}, crumb{"Services", "/services"})},
		Data:        map[string]any{"Groups": groups},
	}, "services")
}

func (p *Site) category(w http.ResponseWriter, r *http.Request) {
	p.categoryPage(w, r, chi.URLParam(r, "category"), "")
}

func (p *Site) categoryInArea(w http.ResponseWriter, r *http.Request) {
	p.categoryPage(w, r, chi.URLParam(r, "category"), chi.URLParam(r, "area"))
}

// categoryPage renders /services/{category} and its local variant /services/{category}/{area},
// which shows area-specific prices and is the page that ranks for "<service> in <area>".
func (p *Site) categoryPage(w http.ResponseWriter, r *http.Request, catSlug, areaSlug string) {
	ctx := r.Context()
	c, err := p.catalogue(ctx)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	cat, ok := c.category(catSlug)
	if !ok {
		if !p.redirectRenamed(w, r, "category", catSlug, func(s string) string { return strings.TrimSuffix("/services/"+s+"/"+areaSlug, "/") }) {
			p.site.NotFound(w, r)
		}
		return
	}
	var area *api.ServiceArea
	var areaID *uuid.UUID
	if areaSlug != "" {
		a, ok := c.area(areaSlug)
		if !ok {
			if !p.redirectRenamed(w, r, "area", areaSlug, func(s string) string { return "/services/" + cat.Slug + "/" + s }) {
				p.site.NotFound(w, r)
			}
			return
		}
		// Only areas with a price for this category have a page; the rest would be empty.
		if !c.Priced[cat.ID][a.ID] {
			p.site.NotFound(w, r)
			return
		}
		area, areaID = &a, &a.ID
	}
	catID := cat.ID
	offers, err := p.offers(ctx, &catID, areaID)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	faqs, err := p.srv.PublicFAQs(ctx, &catID, "en")
	if err != nil {
		p.fail(w, r, err)
		return
	}
	vat := p.srv.PublicVATPct(ctx)

	title := "Vetted " + strings.ToLower(cat.Name) + " in Nairobi"
	desc := "Book " + strings.ToLower(cat.Name) + " from vetted, trained providers in Nairobi"
	path := "/services/" + cat.Slug
	crumbs := []crumb{{"Home", "/"}, {"Services", "/services"}, {cat.Name, path}}
	if area != nil {
		title = "Vetted " + strings.ToLower(cat.Name) + " in " + area.Name
		desc = "Book " + strings.ToLower(cat.Name) + " in " + area.Name + ", " + area.County + " from vetted, trained providers"
		path += "/" + area.Slug
		crumbs = append(crumbs, crumb{area.Name, path})
	}
	if lo := lowestPrice(offers); lo != nil {
		desc += ", from " + web.Money(lo.ClientPriceMinor, lo.Currency)
	}
	desc += ". Fixed prices, managed bookings."

	areas := c.pricedAreas(cat.ID)
	ld := []any{breadcrumbs(p.site, crumbs...), p.serviceLD(cat, area, areas, offers, path)}
	if len(faqs) > 0 {
		ld = append(ld, faqPage(faqs))
	}
	p.render(w, r, &web.Page{
		Title: title, Description: desc, Path: path, Section: "services", JSONLD: ld,
		Image: p.site.Photo(servicePhoto(cat.Slug)),
		Data: map[string]any{"Category": cat, "Area": area, "Areas": areas, "Offers": offers, "FAQs": faqs, "VAT": vat,
			"BookURL": bookURL(cat.Slug, "", areaSlug)},
	}, "category")
}

func (p *Site) areas(w http.ResponseWriter, r *http.Request) {
	c, err := p.catalogue(r.Context())
	if err != nil {
		p.fail(w, r, err)
		return
	}
	p.render(w, r, &web.Page{
		Title:       "Areas we serve",
		Description: "Neighbourhoods in Nairobi where Homend Residence providers work. Find vetted cleaners, domestic workers and trades near you.",
		Section:     "areas",
		JSONLD:      []any{breadcrumbs(p.site, crumb{"Home", "/"}, crumb{"Areas", "/areas"})},
		Data:        c,
	}, "areas")
}

func (p *Site) area(w http.ResponseWriter, r *http.Request) {
	c, err := p.catalogue(r.Context())
	if err != nil {
		p.fail(w, r, err)
		return
	}
	a, ok := c.area(chi.URLParam(r, "area"))
	if !ok {
		if !p.redirectRenamed(w, r, "area", chi.URLParam(r, "area"), func(s string) string { return "/areas/" + s }) {
			p.site.NotFound(w, r)
		}
		return
	}
	path := "/areas/" + a.Slug
	p.render(w, r, &web.Page{
		Title:       "Home services in " + a.Name,
		Description: "Vetted cleaners, domestic workers, caregivers and trades in " + a.Name + ", " + a.County + ". Book at fixed prices; our team manages every job.",
		Path:        path,
		Section:     "areas",
		JSONLD:      []any{breadcrumbs(p.site, crumb{"Home", "/"}, crumb{"Areas", "/areas"}, crumb{a.Name, path})},
		Data:        map[string]any{"Area": a, "Categories": c.pricedCategories(a.ID)},
	}, "area")
}

func (p *Site) pricing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := p.catalogue(ctx)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	var area *api.ServiceArea
	var areaID *uuid.UUID
	if slug := r.URL.Query().Get("area"); slug != "" {
		if a, ok := c.area(slug); ok {
			area, areaID = &a, &a.ID
		}
	}
	type group struct {
		Category api.Category
		Offers   []Offer
	}
	var groups []group
	for _, cat := range c.Categories {
		id := cat.ID
		offers, err := p.offers(ctx, &id, areaID)
		if err != nil {
			p.fail(w, r, err)
			return
		}
		groups = append(groups, group{cat, offers})
	}
	plans, err := p.srv.PublicPlans(ctx)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	data := map[string]any{"Groups": groups, "Plans": plans, "Areas": c.Areas, "Area": area, "VAT": p.srv.PublicVATPct(ctx)}
	if web.IsHTMX(r) {
		web.Cacheable(w, cacheFor)
		p.site.RenderBlock(w, r, p.t, "rate-card", http.StatusOK, data)
		return
	}
	p.render(w, r, &web.Page{
		Title:       "Prices",
		Description: "Fixed rate-card prices for every Homend Residence service in Nairobi. No haggling: you pay the published price.",
		Path:        "/pricing", // ?area= variants share one canonical URL
		Section:     "pricing",
		Data:        data,
	}, "pricing")
}

func (p *Site) faq(w http.ResponseWriter, r *http.Request) {
	faqs, err := p.srv.PublicFAQs(r.Context(), nil, "en")
	if err != nil {
		p.fail(w, r, err)
		return
	}
	var ld []any
	if len(faqs) > 0 {
		ld = append(ld, faqPage(faqs))
	}
	p.render(w, r, &web.Page{
		Title:       "Questions and answers",
		Description: "Answers to common questions about booking, vetting, prices, payment and complaints at Homend Residence.",
		JSONLD:      ld,
		Data:        map[string]any{"FAQs": faqs},
	}, "faq")
}

func (p *Site) book(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := p.catalogue(ctx)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	offers, err := p.offers(ctx, nil, nil)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	type item struct {
		Offer
		BookURL string
	}
	type group struct {
		Category api.Category
		Items    []item
	}
	groups := []group{}
	for _, cat := range c.Categories {
		g := group{Category: cat}
		for _, o := range offers {
			if o.CategoryID == cat.ID {
				g.Items = append(g.Items, item{o, bookURL(cat.Slug, o.Slug, "")})
			}
		}
		groups = append(groups, g)
	}
	p.render(w, r, &web.Page{
		Title:       "Book a service",
		Description: "Choose a service and book a vetted provider in Nairobi. Sign in with your phone number to confirm the date, time and address.",
		Data:        map[string]any{"Groups": groups},
	}, "book")
}

// bookURL links into the client portal's booking flow with the choice preselected.
func bookURL(category, service, area string) string {
	q := url.Values{}
	for k, v := range map[string]string{"category": category, "service": service, "area": area} {
		if v != "" {
			q.Set(k, v)
		}
	}
	u := web.ClientPortalPath + "/book"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// ---- enquiries

type contactForm struct {
	FullName, Phone, Email, Area, AreaName, Category, Message string
	Errors                                                    map[string]string
	Sent                                                      bool
}

func (p *Site) contact(w http.ResponseWriter, r *http.Request) {
	c, err := p.catalogue(r.Context())
	if err != nil {
		p.fail(w, r, err)
		return
	}
	f := contactForm{Category: r.URL.Query().Get("category"), Area: r.URL.Query().Get("area")}
	p.render(w, r, &web.Page{
		Title:       "Contact us",
		Description: "Ask Homend Residence about home services, live-in placements or estate partnerships. We reply by phone or email.",
		Path:        "/contact",
		Data:        map[string]any{"Form": f, "Catalogue": c},
	}, "contact")
}

func (p *Site) contactSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "The form could not be read.", http.StatusBadRequest)
		return
	}
	c, err := p.catalogue(ctx)
	if err != nil {
		p.fail(w, r, err)
		return
	}
	f := contactForm{
		FullName: strings.TrimSpace(r.PostFormValue("full_name")),
		Phone:    strings.TrimSpace(r.PostFormValue("phone")),
		Email:    strings.TrimSpace(r.PostFormValue("email")),
		Area:     r.PostFormValue("area"),
		AreaName: strings.TrimSpace(r.PostFormValue("area_name")),
		Category: r.PostFormValue("category"),
		Message:  strings.TrimSpace(r.PostFormValue("message")),
		Errors:   map[string]string{},
	}
	lead := api.Lead{FullName: f.FullName}
	if f.Phone != "" {
		phone := NormalisePhone(f.Phone)
		lead.Phone = &phone
	}
	if f.Email != "" {
		if !strings.Contains(f.Email, "@") {
			f.Errors["email"] = "Enter a valid email address."
		}
		lead.Email = &f.Email
	}
	if len(f.Message) > 2000 {
		f.Errors["message"] = "Please keep the message under 2,000 characters."
	}
	if f.Message != "" {
		lead.Message = &f.Message
	}
	if a, ok := c.area(f.Area); ok {
		lead.ServiceAreaID = &a.ID
	} else if f.Area == "other" || f.AreaName != "" {
		lead.AreaNotListed = true
		if f.AreaName != "" {
			lead.AreaName = &f.AreaName
		}
	}
	if cat, ok := c.category(f.Category); ok {
		lead.CategoryID = &cat.ID
	} else if f.Category == "not_sure" {
		lead.CategoryNotSure = true
	}
	source := "website"
	if utm := r.URL.Query().Get("utm_source"); utm != "" && len(utm) <= 64 {
		source = "website:" + utm
	}
	lead.Source = &source

	status := http.StatusOK
	if len(f.Errors) == 0 {
		if _, _, err := p.srv.CreateLead(ctx, lead); err != nil {
			var he *httpx.Error
			if !errors.As(err, &he) || he.Status >= 500 {
				p.fail(w, r, err)
				return
			}
			switch {
			case strings.Contains(he.Message, "full_name"):
				f.Errors["full_name"] = "Please tell us your name."
			case strings.Contains(he.Message, "Phone"):
				f.Errors["phone"] = "Enter a Kenyan mobile number like 0712 345 678, or an international number starting with +."
			case strings.Contains(he.Message, "phone number or an email"):
				f.Errors["phone"] = "Give us a phone number or an email so we can reply."
			default:
				f.Errors["form"] = he.Message
			}
		}
	}
	if len(f.Errors) > 0 {
		status = http.StatusUnprocessableEntity
	} else {
		f = contactForm{Sent: true, FullName: f.FullName}
	}
	data := map[string]any{"Form": f, "Catalogue": c}
	if web.IsHTMX(r) {
		// HTMX swaps 2xx responses only by default; send 200 so field errors replace the form.
		p.site.RenderBlock(w, r, p.t, "contact-form", http.StatusOK, data)
		return
	}
	p.site.Render(w, r, p.pages["contact"], status, &web.Page{Title: "Contact us", NoIndex: true, Path: "/contact", Data: data})
}

// NormalisePhone turns common Kenyan formats (0712…, 712…, 254712…) into E.164. Anything else is
// returned trimmed for the shared validator to judge.
func NormalisePhone(in string) string {
	s := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(in))
	switch {
	case strings.HasPrefix(s, "+"):
		return s
	case strings.HasPrefix(s, "254") && len(s) == 12:
		return "+" + s
	case strings.HasPrefix(s, "0") && len(s) == 10:
		return "+254" + s[1:]
	case (strings.HasPrefix(s, "7") || strings.HasPrefix(s, "1")) && len(s) == 9:
		return "+254" + s
	}
	return s
}

// ---- static content pages

func (p *Site) static(name, title, section, desc string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.render(w, r, &web.Page{Title: title, Description: desc, Section: section}, name)
	}
}

// ---- helpers

// render executes a full page; its body is the template "<name>-content".
func (p *Site) render(w http.ResponseWriter, r *http.Request, page *web.Page, name string) {
	web.Cacheable(w, cacheFor)
	p.site.Render(w, r, p.pages[name], http.StatusOK, page)
}

func (p *Site) fail(w http.ResponseWriter, r *http.Request, err error) {
	// The request log line (httpx.Logging) carries the path; this one is joined to it by request_id.
	slog.ErrorContext(r.Context(), "public page", "err", err, "request_id", httpx.RequestID(r.Context()))
	http.Error(w, "Something went wrong on our side. Please try again in a minute.", http.StatusInternalServerError)
}

// Package provider is the provider portal and recruitment pages (ADR-0001): Go html/template + HTMX on
// the shared web foundation (internal/web), installable as a PWA, light enough for low-end Android
// phones on metered data. Check-in and check-out queue on the phone when offline and sync later.
//
// Every action runs the same handler as the JSON route in-process (api.Invoke), so the HTML pages,
// USSD, SMS and the console all go through one copy of each rule. This package only translates forms
// into request bodies and responses into pages.
package provider

import (
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Options struct {
	// CookieSecure marks the flash cookie Secure (always in production).
	CookieSecure bool
}

type Portal struct {
	api  *api.Server
	site *web.Site
	opts Options
	t    *template.Template            // every template, for fragments
	page map[string]*template.Template // per page, with "content" pointing at the page
}

// New builds the portal's templates on the shared base layout; a template error fails at start-up.
func New(srv *api.Server, site *web.Site, opts Options) (*Portal, error) {
	t, err := site.TemplatesWithFuncs(funcs, templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	files, err := fs.Glob(templateFS, "templates/[a-z]*.html")
	if err != nil {
		return nil, err
	}
	p := &Portal{api: srv, site: site, opts: opts, t: t, page: map[string]*template.Template{}}
	// Each page file defines a template named after the file; the layout's "content" block points at it.
	for _, f := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(f, "templates/"), ".html")
		pt, err := t.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := pt.New("content").Parse(`{{template "` + name + `" .}}`); err != nil {
			return nil, err
		}
		p.page[name] = pt
	}
	return p, nil
}

// Routes is the provider portal, mounted at web.ProviderPortalPath. Sign-in and PWA files are public.
func (p *Portal) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/sw.js", p.serviceWorker)
	r.Get("/manifest.webmanifest", p.manifest)
	r.Handle("/static/*", p.static())
	r.Get("/offline", p.offline)
	r.Get("/login", p.loginForm)
	r.Post("/login", p.loginRequest)
	r.Post("/login/password", p.loginPassword)
	r.Get("/forgot-password", p.forgotForm)
	r.Post("/forgot-password", p.forgot)
	r.Get("/reset-password", p.resetForm)
	r.Post("/reset-password", p.reset)
	r.Post("/login/verify", p.loginVerify)
	r.Post("/logout", p.logout)

	r.Group(func(r chi.Router) {
		r.Use(p.requireProvider)
		r.Get("/", p.dashboard)
		r.Get("/more", p.more)

		r.Get("/applications", p.applications)
		r.Get("/applications/new", p.newApplication)
		r.Post("/applications", p.createApplication)
		r.Get("/applications/{id}", p.application)
		r.Post("/applications/{id}/referees", p.addReferee)
		r.Post("/applications/{id}/referees/{rid}/delete", p.removeReferee)
		r.Post("/applications/{id}/submit", p.submitApplication)
		r.Post("/applications/{id}/withdraw", p.withdrawApplication)
		r.Post("/applications/{id}/attach", p.attachDocument)
		r.Get("/documents", p.documents)
		r.Post("/documents/ticket", p.uploadTicket)
		r.Post("/documents/{id}/complete", p.completeUpload)
		r.Get("/training", p.training)
		r.Get("/contract", p.contract)
		r.Post("/contract/code", p.contractCode)
		r.Post("/contract/sign", p.contractSign)
		r.Get("/profile", p.profile)
		r.Post("/profile", p.saveProfile)
		r.Post("/password", p.savePassword)

		r.Get("/availability", p.availability)
		r.Post("/availability", p.saveAvailability)
		r.Post("/time-off", p.addTimeOff)
		r.Post("/time-off/{id}/delete", p.removeTimeOff)
		r.Get("/offers", p.offers)
		r.Post("/offers/{id}/accept", p.acceptOffer)
		r.Post("/offers/{id}/decline", p.declineOffer)
		r.Get("/jobs", p.jobs)
		r.Get("/jobs/{id}", p.job)
		r.Post("/jobs/{id}/variations", p.proposeVariation)
		r.Post("/visits/{id}/check-in", p.checkIn)
		r.Post("/visits/{id}/check-out", p.checkOut)
		r.Post("/sync", p.sync)

		r.Get("/pay", p.pay)
		r.Get("/appraisal", p.appraisal)
		r.Get("/complaints", p.complaints)
		r.Post("/complaints/{id}/account", p.complaintAccount)
		r.Get("/inbox", p.inbox)
		r.Post("/inbox/read-all", p.inboxReadAll)
	})
	return r
}

// JoinRoutes is the public recruitment funnel, mounted at web.JoinPath.
func (p *Portal) JoinRoutes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", p.join)
	r.Get("/apply", p.applyForm)
	r.Post("/apply", p.applyRequest)
	r.Post("/apply/password", p.applyPassword)
	r.Post("/apply/verify", p.applyVerify)
	return r
}

// ---- request plumbing

const portal = web.ProviderPortalPath

func (p *Portal) requireProvider(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pr := auth.FromContext(r.Context())
		if pr == nil || pr.Kind != auth.KindProvider {
			if r.URL.Path == portal+"/sync" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"Sign in again to send what is saved on this phone"}`))
				return
			}
			p.redirect(w, r, portal+"/login?next="+url.QueryEscape(r.URL.RequestURI()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// redirect answers a plain form post with 303 and an HTMX request with HX-Redirect, so both land on a full page.
func (p *Portal) redirect(w http.ResponseWriter, r *http.Request, to string) {
	if web.IsHTMX(r) {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

const flashCookie = "hr_flash"

// done keeps one message across the redirect (post, redirect, get); failed does the same with the
// handler's own error message.
func (p *Portal) done(w http.ResponseWriter, r *http.Request, to, msg string) {
	if msg != "" {
		http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: url.QueryEscape(msg), Path: "/", HttpOnly: true, Secure: p.opts.CookieSecure,
			SameSite: http.SameSiteLaxMode, MaxAge: 60})
	}
	p.redirect(w, r, to)
}

func (p *Portal) failed(w http.ResponseWriter, r *http.Request, to string, err error) {
	p.done(w, r, to, "! "+message(err))
}

func (p *Portal) takeFlash(w http.ResponseWriter, r *http.Request) (string, bool) {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return "", false
	}
	// Cleared with the same attributes it was set with.
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", HttpOnly: true, Secure: p.opts.CookieSecure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
	msg, _ := url.QueryUnescape(c.Value)
	return strings.TrimPrefix(msg, "! "), strings.HasPrefix(msg, "! ")
}

// ---- rendering

// view is web.Page.Data for every portal page: the portal chrome plus the page's own data in V.
type view struct {
	Nav      string // highlights the bottom-bar tab
	Flash    string
	IsError  bool
	SignedIn bool
	Public   bool // recruitment pages keep the public header and footer
	V        any
}

type page struct {
	name, title, nav string
	public           bool
}

func (p *Portal) render(w http.ResponseWriter, r *http.Request, status int, pg page, data any) {
	v := &view{Nav: pg.nav, SignedIn: auth.FromContext(r.Context()) != nil, Public: pg.public, V: data}
	v.Flash, v.IsError = p.takeFlash(w, r)
	t, ok := p.page[pg.name]
	if !ok {
		http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
		return
	}
	p.site.Render(w, r, t, status, &web.Page{Title: pg.title, NoIndex: !pg.public, Section: section(pg), Data: v})
}

func section(pg page) string {
	if pg.public {
		return "join"
	}
	return ""
}

// fragment renders one named template for an HTMX swap.
func (p *Portal) fragment(w http.ResponseWriter, r *http.Request, name string, data any) {
	p.site.RenderBlock(w, r, p.t, name, http.StatusOK, data)
}

func (p *Portal) problem(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	var ae *apiError
	if errors.As(err, &ae) {
		status = ae.Status
	}
	if status == http.StatusUnauthorized {
		p.redirect(w, r, portal+"/login")
		return
	}
	p.render(w, r, status, page{name: "problem", title: "Something went wrong"}, message(err))
}

// ---- calling the handlers

type apiError struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func message(err error) string {
	var ae *apiError
	if errors.As(err, &ae) && ae.Message != "" && ae.Status < 500 {
		return ae.Message
	}
	return "Something went wrong on our side. Please try again, or call us if it keeps happening."
}

func invoke(r *http.Request, body any, h func(http.ResponseWriter, *http.Request)) (http.Header, json.RawMessage, error) {
	code, hdr, raw := api.Invoke(r, body, h)
	if code >= 200 && code < 300 {
		return hdr, raw, nil
	}
	e := &apiError{Status: code}
	if json.Unmarshal(raw, e) != nil || e.Code == "" {
		e.Code, e.Message = "internal", ""
	}
	return hdr, raw, e
}

// call runs a handler as the signed-in user and decodes its JSON answer into T.
func call[T any](r *http.Request, body any, h func(http.ResponseWriter, *http.Request)) (T, error) {
	var out T
	_, raw, err := invoke(r, body, h)
	if err != nil {
		return out, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

// do runs a handler whose answer the page does not need.
func do(r *http.Request, body any, h func(http.ResponseWriter, *http.Request)) error {
	_, _, err := invoke(r, body, h)
	return err
}

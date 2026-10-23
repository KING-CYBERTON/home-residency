// Package client is the household portal (ADR-0001): Go html/template pages with HTMX on the shared
// web foundation, installable as a PWA. Pages call the API handlers in process, so every business
// rule, permission check and audit entry is the one the JSON API and the Flutter app go through.
// Nothing here talks to the database directly.
package client

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
)

//go:embed templates static
var assets embed.FS

// Deps are what the portal needs from the rest of the service.
type Deps struct {
	Site         *web.Site           // shared layout, assets and template helpers
	API          gen.ServerInterface // the API handlers, called in process
	Auth         *auth.Service       // resolves the session cookie when the portal is mounted outside the API middleware
	CookieSecure bool
	PushKey      string // VAPID public key; empty hides the push sign-up
	// NoPhoneSignIn hides sign-in by SMS code, for when the service runs without SMS
	// (SMS_PROVIDER=none); email and password always work.
	NoPhoneSignIn bool
	Base          string // mount path, default web.ClientPortalPath
	// Invoke runs one API handler in process. It defaults to an in-memory recorder; the service can
	// pass api.Invoke so every HTML portal shares one implementation.
	// Same signature as api.Invoke (Provider portal).
	Invoke func(parent *http.Request, body any, h func(http.ResponseWriter, *http.Request)) (int, http.Header, json.RawMessage)
	Now    func() time.Time
}

type Portal struct {
	Deps
	pages map[string]*template.Template
}

func New(d Deps) (*Portal, error) {
	if d.API == nil || d.Site == nil {
		return nil, errors.New("client portal: Site and API are required")
	}
	if d.Base == "" {
		d.Base = web.ClientPortalPath
	}
	if d.Invoke == nil {
		d.Invoke = invoke
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	p := &Portal{Deps: d, pages: map[string]*template.Template{}}
	names, err := fs.Glob(assets, "templates/pages/*.html")
	if err != nil {
		return nil, err
	}
	for _, f := range names {
		t, err := d.Site.TemplatesWithFuncs(p.funcs(), assets, "templates/portal.html", "templates/partials/*.html", f)
		if err != nil {
			return nil, fmt.Errorf("client portal: %s: %w", f, err)
		}
		p.pages[strings.TrimSuffix(strings.TrimPrefix(f, "templates/pages/"), ".html")] = t
	}
	return p, nil
}

// Routes returns the portal's router, to be mounted at Base.
func (p *Portal) Routes() chi.Router {
	r := chi.NewRouter()
	// Cross-site form protection and security headers come from web.Site.Middleware, which the
	// mount point applies to every HTML surface.
	r.Use(p.session)

	// Installable app: these must sit under Base so the service worker's scope covers the portal.
	r.Get("/manifest.webmanifest", p.manifest)
	r.Get("/sw.js", p.serviceWorker)
	r.Get("/offline", p.offline)
	static, _ := fs.Sub(assets, "static")
	r.Handle("/static/*", http.StripPrefix(p.Base+"/static/", cacheFor(http.FileServerFS(static))))

	r.Get("/login", p.loginPage)
	r.Post("/login", p.loginStart)
	r.Post("/login/verify", p.loginVerify)
	r.Post("/login/password", p.loginPassword)
	r.Get("/forgot-password", p.forgotPage)
	r.Post("/forgot-password", p.forgot)
	r.Get("/reset-password", p.resetPage)
	r.Post("/reset-password", p.reset)

	r.Group(func(r chi.Router) {
		r.Use(p.requireClient)
		r.Get("/", p.home)
		r.Post("/logout", p.logout)

		r.Get("/book", p.bookPage)
		r.Get("/book/services", p.bookServices)
		r.Post("/book/quote", p.bookQuote)
		r.Post("/book", p.bookSubmit)

		r.Get("/requests", p.requestsPage)
		r.Get("/requests/{id}", p.requestPage)
		r.Post("/requests/{id}/cancel", p.requestCancel)
		r.Post("/requests/{id}/shortlist/{entry}", p.shortlistSelect)
		r.Post("/requests/{id}/rating", p.rate)
		r.Post("/variations/{id}/{decision}", p.variationDecide)
		r.Post("/visits/{id}/door-code", p.doorCode)
		r.Post("/visits/{id}/confirm", p.visitConfirm)

		r.Get("/invoices", p.invoicesPage)
		r.Get("/invoices/{id}", p.invoicePage)
		r.Post("/invoices/{id}/pay", p.invoicePay)
		r.Get("/payments/{id}", p.paymentStatus)

		r.Get("/complaints", p.complaintsPage)
		r.Get("/complaints/new", p.complaintNew)
		r.Post("/complaints", p.complaintCreate)
		r.Get("/complaints/{id}", p.complaintPage)

		r.Get("/profile", p.accountPage)
		r.Post("/profile", p.profileSave)
		r.Post("/profile/id-verification", p.idVerification)
		r.Post("/profile/password", p.passwordSave)
		r.Post("/uploads", p.uploadTicket)
		r.Post("/uploads/{id}/complete", p.uploadComplete)

		r.Get("/addresses", p.addressesPage)
		r.Post("/addresses", p.addressCreate)
		r.Post("/addresses/{id}/default", p.addressDefault)
		r.Post("/addresses/{id}/archive", p.addressArchive)

		r.Get("/plans", p.plansPage)
		r.Post("/plans/subscribe", p.subscribe)
		r.Post("/plans/cancel", p.subscriptionCancel)

		r.Get("/notifications", p.notificationsPage)
		r.Post("/notifications/read-all", p.notificationsReadAll)
		r.Post("/push", p.pushSubscribe)
		r.Get("/messages", p.messagesPage)
		r.Post("/messages", p.conversationCreate)
		r.Get("/messages/{id}", p.conversationPage)
		r.Post("/messages/{id}", p.messageSend)
	})
	return r
}

// ---- calling the API in process

// apiError is the API's error envelope, as returned by a handler that refused the call.
type apiError struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *apiError) Error() string { return e.Message }

// call runs h as the signed-in user and decodes a 2xx answer into out (which may be nil).
func (p *Portal) call(r *http.Request, body any, out any, h func(http.ResponseWriter, *http.Request)) error {
	code, _, raw := p.Invoke(r, body, h)
	if code < 200 || code > 299 {
		e := &apiError{Status: code}
		if json.Unmarshal(raw, e) != nil || e.Message == "" {
			e.Message = "Something went wrong. Please try again."
		}
		if code >= 500 {
			slog.ErrorContext(r.Context(), "client portal: api call failed", "status", code, "request_id", httpx.RequestID(r.Context()))
			e.Message = "Something went wrong on our side. Please try again in a moment."
		}
		return e
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// invoke is the default in-process runner: it hands the handler a JSON request and records the answer.
func invoke(parent *http.Request, body any, h func(http.ResponseWriter, *http.Request)) (int, http.Header, json.RawMessage) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(parent.Context(), http.MethodPost, parent.URL.Path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = parent.RemoteAddr
	if v := parent.Header.Get("User-Agent"); v != "" {
		req.Header.Set("User-Agent", v)
	}
	rec := &recorder{h: http.Header{}}
	h(rec, req)
	if rec.code == 0 {
		rec.code = http.StatusOK
	}
	return rec.code, rec.h, rec.buf.Bytes()
}

type recorder struct {
	h    http.Header
	code int
	buf  bytes.Buffer
}

func (m *recorder) Header() http.Header { return m.h }
func (m *recorder) WriteHeader(c int) {
	if m.code == 0 {
		m.code = c
	}
}
func (m *recorder) Write(b []byte) (int, error) {
	if m.code == 0 {
		m.code = http.StatusOK
	}
	return m.buf.Write(b)
}

// ---- sessions and access

// session attaches the principal from the session cookie when the portal is mounted outside the
// API's authentication middleware. When it is mounted behind it, the principal is already there.
func (p *Portal) session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.FromContext(r.Context()) == nil && p.Auth != nil {
			if c, err := r.Cookie(auth.CookieName); err == nil && c.Value != "" {
				if pr, err := p.Auth.Lookup(r.Context(), c.Value); err == nil {
					r = r.WithContext(auth.WithPrincipal(r.Context(), pr))
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (p *Portal) requireClient(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pr := auth.FromContext(r.Context())
		if pr == nil {
			target := p.Base + "/login"
			if r.Method == http.MethodGet && r.URL.Path != p.Base+"/" {
				target += "?next=" + url.QueryEscape(r.URL.RequestURI())
			}
			p.redirect(w, r, target)
			return
		}
		if pr.Kind != auth.KindClient {
			// The policy middleware does this for /client/* API routes; pages call handlers directly, so check here.
			p.renderStatus(w, r, "wrong-account", http.StatusForbidden, &view{Title: "Household account needed", Data: string(pr.Kind)})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- rendering

// view is what every portal template receives (as web.Page.Data on full pages).
type view struct {
	Title  string
	Nav    string // which tab is active
	Me     *gen.Me
	Base   string
	Flash  string
	Error  string
	Unread int
	Push   string // VAPID public key, when push is configured
	Data   any
}

func (p *Portal) render(w http.ResponseWriter, r *http.Request, page string, v *view) {
	p.renderStatus(w, r, page, http.StatusOK, v)
}

// renderStatus renders a full page inside the shared layout.
func (p *Portal) renderStatus(w http.ResponseWriter, r *http.Request, page string, status int, v *view) {
	t, ok := p.pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	v.Base = p.Base
	v.Push = p.PushKey
	if v.Me == nil {
		if pr := auth.FromContext(r.Context()); pr != nil && pr.Kind == auth.KindClient {
			var me gen.Me
			if p.call(r, nil, &me, p.API.GetMe) == nil {
				v.Me = &me
			}
			var unread struct {
				Data []gen.Notification `json:"data"`
			}
			yes, lim := true, 20
			if p.call(r, nil, &unread, func(w http.ResponseWriter, r *http.Request) {
				p.API.GetNotifications(w, r, gen.GetNotificationsParams{Unread: &yes, Limit: &lim})
			}) == nil {
				v.Unread = len(unread.Data)
			}
		}
	}
	if v.Flash == "" {
		if c, err := r.Cookie("hr_flash"); err == nil {
			if msg, err := url.QueryUnescape(c.Value); err == nil {
				v.Flash = msg
			}
			http.SetCookie(w, &http.Cookie{Name: "hr_flash", Path: p.Base, MaxAge: -1, HttpOnly: true, Secure: p.CookieSecure, SameSite: http.SameSiteLaxMode})
		}
	}
	p.Site.Render(w, r, t, status, &web.Page{Title: v.Title, NoIndex: true, Section: "account", Data: v})
}

// renderBlock renders one named template of a page as an HTMX fragment.
func (p *Portal) renderBlock(w http.ResponseWriter, r *http.Request, page, block string, v *view) {
	t, ok := p.pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	v.Base = p.Base
	p.Site.RenderBlock(w, r, t, block, http.StatusOK, v)
}

// flash shows a one-line message on the next page.
func (p *Portal) flash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{Name: "hr_flash", Value: url.QueryEscape(msg), Path: p.Base, MaxAge: 60, HttpOnly: true, Secure: p.CookieSecure, SameSite: http.SameSiteLaxMode})
}

// redirect sends the browser to target after a POST, as a full navigation for HTMX requests too.
func (p *Portal) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// failPage shows an API refusal as a page: not found becomes 404, anything else an error banner.
func (p *Portal) failPage(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		p.renderStatus(w, r, "message", http.StatusNotFound, &view{Title: "Not found", Error: "We could not find that. It may belong to another account or have been removed."})
		return
	}
	if !errors.As(err, &ae) {
		slog.ErrorContext(r.Context(), "client portal: page failed", "err", err)
	}
	p.render(w, r, "message", &view{Title: "Something went wrong", Error: errText(err)})
}

func errText(err error) string {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Message
	}
	return "Something went wrong. Please try again."
}

// cacheFor lets browsers keep the portal's own static files for an hour; the service worker
// versions them itself.
func cacheFor(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

package provider

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
)

// Pay, appraisal, complaints, inbox, and the PWA files.

type payPage struct {
	Summary  gen.EarningsSummary
	Earnings gen.EarningPage
	Payouts  gen.PayoutPage
	Cursor   string
}

func (p *Portal) pay(w http.ResponseWriter, r *http.Request) {
	sum, err := call[gen.EarningsSummary](r, nil, p.api.GetProviderEarningsSummary)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	limit := 30
	params := gen.GetProviderEarningsParams{Limit: &limit}
	if c := r.URL.Query().Get("cursor"); c != "" {
		params.Cursor = &c
	}
	earn, err := call[gen.EarningPage](r, nil, func(w http.ResponseWriter, rq *http.Request) { p.api.GetProviderEarnings(w, rq, params) })
	if err != nil {
		p.problem(w, r, err)
		return
	}
	ten := 10
	pays, _ := call[gen.PayoutPage](r, nil, func(w http.ResponseWriter, rq *http.Request) {
		p.api.GetProviderPayouts(w, rq, gen.GetProviderPayoutsParams{Limit: &ten})
	})
	p.render(w, r, http.StatusOK, page{name: "pay", title: "My pay", nav: "pay"}, payPage{Summary: sum, Earnings: earn, Payouts: pays, Cursor: r.URL.Query().Get("cursor")})
}

func (p *Portal) appraisal(w http.ResponseWriter, r *http.Request) {
	a, err := call[gen.Appraisal](r, nil, p.api.GetProviderAppraisal)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "appraisal", title: "My score", nav: "more"}, a)
}

func (p *Portal) complaints(w http.ResponseWriter, r *http.Request) {
	list, err := call[[]gen.Complaint](r, nil, p.api.GetProviderComplaints)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "complaints", title: "Complaints", nav: "more"}, list)
}

// complaintAccount records the provider's side before any decision (report §5.4 step 3).
func (p *Portal) complaintAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	in := gen.ProviderAccountInput{Account: strings.TrimSpace(r.PostFormValue("account"))}
	if d, err := uuid.Parse(r.PostFormValue("document_id")); err == nil {
		in.DocumentIds = &[]openapi_types.UUID{d}
	}
	if err := do(r, in, func(w http.ResponseWriter, rq *http.Request) { p.api.PostProviderComplaintsAccount(w, rq, id) }); err != nil {
		p.failed(w, r, portal+"/complaints", err)
		return
	}
	p.done(w, r, portal+"/complaints", "Thank you. Your account has been recorded and will be read before any decision.")
}

func (p *Portal) inbox(w http.ResponseWriter, r *http.Request) {
	limit := 30
	list, err := call[gen.NotificationPage](r, nil, func(w http.ResponseWriter, rq *http.Request) {
		p.api.GetNotifications(w, rq, gen.GetNotificationsParams{Limit: &limit})
	})
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "inbox", title: "Messages", nav: "more"}, list.Data)
}

func (p *Portal) inboxReadAll(w http.ResponseWriter, r *http.Request) {
	if err := do(r, nil, p.api.PostNotificationsReadAll); err != nil {
		p.failed(w, r, portal+"/inbox", err)
		return
	}
	p.done(w, r, portal+"/inbox", "")
}

// ---- PWA

// static serves the portal's own scripts. The shared CSS, HTMX and icons come from the foundation.
func (p *Portal) static() http.Handler {
	sub, _ := fs.Sub(staticFS, "static")
	files := http.StripPrefix(portal+"/static/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}

// serviceWorker is served from the portal root so its scope covers every portal page. The asset
// list is filled in here so a new CSS or HTMX build replaces the cached copies.
func (p *Portal) serviceWorker(w http.ResponseWriter, r *http.Request) {
	src, err := fs.ReadFile(staticFS, "static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	assets := []string{portal + "/offline", p.site.Asset("css/app.css"), p.site.Asset("js/htmx.min.js"), p.site.Asset("img/favicon.svg"),
		portal + "/static/portal.js", portal + "/static/queue.js"}
	list := `["` + strings.Join(assets, `","`) + `"]`
	out := strings.Replace(string(src), "self.HR_ASSETS", list, 1)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Service-Worker-Allowed", portal+"/")
	_, _ = w.Write([]byte(out))
}

func (p *Portal) manifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	web.Cacheable(w, 3600e9)
	_, _ = w.Write([]byte(`{"name":"Homend Residence for providers","short_name":"Homend Work","start_url":"` + portal + `/","scope":"` + portal + `/",` +
		`"display":"standalone","background_color":"#d8cbb8","theme_color":"#d8cbb8","icons":[` +
		`{"src":"` + p.site.Asset("img/icon-192.png") + `","sizes":"192x192","type":"image/png"},` +
		`{"src":"` + p.site.Asset("img/icon-512.png") + `","sizes":"512x512","type":"image/png","purpose":"any maskable"}]}`))
}

func (p *Portal) offline(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, http.StatusOK, page{name: "offline", title: "You are offline"}, nil)
}

// StaticFS exposes the portal's scripts, for the page-weight test.
func StaticFS() fs.FS { return staticFS }

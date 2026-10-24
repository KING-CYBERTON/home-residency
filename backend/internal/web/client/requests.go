package client

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/domain"
)

type requestPage struct {
	Data []gen.ServiceRequest `json:"data"`
	Meta gen.PageMeta         `json:"meta"`
}

type invoicePage struct {
	Data []gen.Invoice `json:"data"`
	Meta gen.PageMeta  `json:"meta"`
}

// ---- home

func (p *Portal) home(w http.ResponseWriter, r *http.Request) {
	var reqs requestPage
	lim := 20
	if err := p.call(r, nil, &reqs, func(w http.ResponseWriter, r *http.Request) {
		p.API.GetClientRequests(w, r, gen.GetClientRequestsParams{Limit: &lim})
	}); err != nil {
		p.failPage(w, r, err)
		return
	}
	var unpaid invoicePage
	issued := gen.InvoiceStatusIssued
	_ = p.call(r, nil, &unpaid, func(w http.ResponseWriter, r *http.Request) {
		p.API.GetClientInvoices(w, r, gen.GetClientInvoicesParams{Status: &issued, Limit: &lim})
	})
	type homeData struct {
		Action, Active, Recent []gen.ServiceRequest
		Unpaid                 []gen.Invoice
	}
	d := homeData{Unpaid: unpaid.Data}
	for _, q := range reqs.Data {
		switch q.Status {
		case gen.RequestStatusAwaitingClientChoice, gen.RequestStatusAwaitingRating:
			d.Action = append(d.Action, q)
		case gen.RequestStatusClosed, gen.RequestStatusCancelled, gen.RequestStatusUnfilled:
			if len(d.Recent) < 3 {
				d.Recent = append(d.Recent, q)
			}
		default:
			d.Active = append(d.Active, q)
		}
	}
	p.render(w, r, "home", &view{Title: "Home", Nav: "home", Data: d})
}

// ---- booking

type bookData struct {
	Categories []gen.Category
	Category   *gen.Category
	Services   []gen.Service
	Addresses  []gen.Address
	ServiceID  string
	AddressID  string
	Quantity   string
	Start      string
	Notes      string
	Recurrence string
	Key        string
	Area       string // slug or id from a public "Book" link; picks a matching address
	Terms      bool   // the terms box was ticked; kept ticked when the form comes back with an error
}

type categoryDetail struct {
	Category gen.Category  `json:"category"`
	Services []gen.Service `json:"services"`
}

func (p *Portal) loadCategory(r *http.Request, slug string) (*categoryDetail, error) {
	if slug == "" {
		return nil, nil
	}
	var cd categoryDetail
	if err := p.call(r, nil, &cd, func(w http.ResponseWriter, r *http.Request) { p.API.GetPublicCategoriesById(w, r, slug) }); err != nil {
		return nil, err
	}
	active := cd.Services[:0]
	for _, s := range cd.Services {
		if s.IsActive {
			active = append(active, s)
		}
	}
	cd.Services = active
	return &cd, nil
}

func (p *Portal) bookData(r *http.Request, d *bookData, slug string) error {
	if err := p.call(r, nil, &d.Categories, p.API.GetPublicCategories); err != nil {
		return err
	}
	if err := p.call(r, nil, &d.Addresses, p.API.GetClientAddresses); err != nil {
		return err
	}
	if slug == "" && len(d.Categories) == 1 {
		slug = d.Categories[0].Slug
	}
	// A public "Book" link may name only the service, by slug or id: find its category.
	if slug == "" && d.ServiceID != "" {
		for _, c := range d.Categories {
			if cd, err := p.loadCategory(r, c.Slug); err == nil && cd != nil && findService(cd.Services, d.ServiceID) != nil {
				slug = c.Slug
				break
			}
		}
	}
	cd, err := p.loadCategory(r, slug)
	if err != nil {
		return err
	}
	if cd != nil {
		d.Category, d.Services = &cd.Category, cd.Services
		if svc := findService(cd.Services, d.ServiceID); svc != nil {
			d.ServiceID = svc.Id.String()
		} else if len(cd.Services) == 1 {
			d.ServiceID = cd.Services[0].Id.String()
		}
	}
	if d.AddressID == "" && d.Area != "" {
		var areas []gen.ServiceArea
		if p.call(r, nil, &areas, p.API.GetPublicServiceAreas) == nil {
			for _, ar := range areas {
				if ar.Slug != d.Area && ar.Id.String() != d.Area {
					continue
				}
				for _, a := range d.Addresses {
					if a.ServiceAreaId != nil && *a.ServiceAreaId == ar.Id {
						d.AddressID = a.Id.String()
						break
					}
				}
			}
		}
	}
	if d.AddressID == "" {
		for _, a := range d.Addresses {
			if a.IsDefault {
				d.AddressID = a.Id.String()
			}
		}
	}
	return nil
}

func (p *Portal) bookPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := &bookData{ServiceID: q.Get("service"), Area: q.Get("area"), Quantity: "1", Key: newKey()}
	if err := p.bookData(r, d, q.Get("category")); err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "book", &view{Title: "Book a service", Nav: "book", Data: d})
}

// findService matches a service by id or slug.
func findService(services []gen.Service, key string) *gen.Service {
	if key == "" {
		return nil
	}
	for i := range services {
		if services[i].Id.String() == key || services[i].Slug == key {
			return &services[i]
		}
	}
	return nil
}

// bookServices swaps in the services of the chosen category.
func (p *Portal) bookServices(w http.ResponseWriter, r *http.Request) {
	d := &bookData{}
	cd, err := p.loadCategory(r, r.URL.Query().Get("category"))
	if err != nil {
		p.renderBlock(w, r, "book", "services", &view{Error: errText(err), Data: d})
		return
	}
	if cd != nil {
		d.Category, d.Services = &cd.Category, cd.Services
		if len(cd.Services) == 1 {
			d.ServiceID = cd.Services[0].Id.String()
		}
	}
	p.renderBlock(w, r, "book", "services", &view{Data: d})
}

func formQuantity(r *http.Request) *float32 {
	f, err := strconv.ParseFloat(strings.TrimSpace(r.PostFormValue("quantity")), 32)
	if err != nil || f <= 0 {
		return nil
	}
	q := float32(f)
	return &q
}

// bookQuote prices the booking from the rate card as the form changes.
func (p *Portal) bookQuote(w http.ResponseWriter, r *http.Request) {
	svc, ok1 := formUUID(r, "service_id")
	addr, ok2 := formUUID(r, "address_id")
	if !ok1 || !ok2 {
		p.renderBlock(w, r, "book", "quote", &view{Data: nil})
		return
	}
	var quote gen.Quote
	if err := p.call(r, gen.QuoteRequest{ServiceId: svc, AddressId: addr, Quantity: formQuantity(r)}, &quote, p.API.PostClientQuotes); err != nil {
		p.renderBlock(w, r, "book", "quote", &view{Error: errText(err)})
		return
	}
	p.renderBlock(w, r, "book", "quote", &view{Data: &quote})
}

func (p *Portal) bookSubmit(w http.ResponseWriter, r *http.Request) {
	d := &bookData{
		ServiceID: r.PostFormValue("service_id"), AddressID: r.PostFormValue("address_id"), Quantity: r.PostFormValue("quantity"),
		Start: r.PostFormValue("start"), Notes: r.PostFormValue("notes"), Recurrence: r.PostFormValue("recurrence"), Key: r.PostFormValue("key"),
		Terms: r.PostFormValue("terms") != "",
	}
	fail := func(msg string) {
		if err := p.bookData(r, d, r.PostFormValue("category")); err != nil {
			p.failPage(w, r, err)
			return
		}
		p.render(w, r, "book", &view{Title: "Book a service", Nav: "book", Error: msg, Data: d})
	}
	svc, ok := formUUID(r, "service_id")
	if !ok {
		fail("Choose a service.")
		return
	}
	addr, ok := formUUID(r, "address_id")
	if !ok {
		fail("Choose the address for this visit.")
		return
	}
	start, ok := parseLocal(d.Start)
	if !ok {
		fail("Choose the day and time you want the provider to arrive.")
		return
	}
	in := gen.ServiceRequestCreate{ServiceId: svc, AddressId: addr, Quantity: formQuantity(r), RequestedStart: start,
		ClientNotes: optString(d.Notes), RecurrenceRule: optString(d.Recurrence), AcceptTerms: d.Terms}
	key := gen.IdempotencyKey(d.Key)
	var created gen.ServiceRequest
	if err := p.call(r, in, &created, func(w http.ResponseWriter, r *http.Request) {
		p.API.PostClientRequests(w, r, gen.PostClientRequestsParams{IdempotencyKey: &key})
	}); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Details["id_verification"] != nil {
			p.flash(w, ae.Message+".")
			p.redirect(w, r, p.Base+"/profile#id")
			return
		}
		d.Key = newKey() // the failed attempt released the key; a fresh one keeps retries clean
		fail(errText(err))
		return
	}
	p.flash(w, "Booked. Your reference is "+created.Reference+". We are finding the right provider for you.")
	p.redirect(w, r, p.Base+"/requests/"+created.Id.String())
}

// ---- requests

func (p *Portal) requestsPage(w http.ResponseWriter, r *http.Request) {
	params := gen.GetClientRequestsParams{}
	if c := r.URL.Query().Get("cursor"); c != "" {
		cur := gen.Cursor(c)
		params.Cursor = &cur
	}
	var page requestPage
	if err := p.call(r, nil, &page, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientRequests(w, r, params) }); err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "requests", &view{Title: "My bookings", Nav: "requests", Data: page})
}

type requestDetail struct {
	Req        gen.ServiceRequest
	Variations []gen.Variation
	Shortlist  []gen.ShortlistEntryClient
	Invoice    *gen.Invoice
	CanCancel  bool
	RateVisit  string
}

func (p *Portal) loadRequestDetail(r *http.Request, id openapi_types.UUID) (*requestDetail, error) {
	d := &requestDetail{}
	if err := p.call(r, nil, &d.Req, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientRequestsById(w, r, id) }); err != nil {
		return nil, err
	}
	d.CanCancel = domain.CanClientCancel(string(d.Req.Status))
	_ = p.call(r, nil, &d.Variations, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientRequestsVariations(w, r, id) })
	if d.Req.Status == gen.RequestStatusAwaitingClientChoice {
		_ = p.call(r, nil, &d.Shortlist, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientRequestsShortlist(w, r, id) })
	}
	if d.Req.InvoiceId != nil {
		var inv gen.Invoice
		if p.call(r, nil, &inv, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientInvoicesById(w, r, *d.Req.InvoiceId) }) == nil {
			d.Invoice = &inv
		}
	}
	if d.Req.Visits != nil {
		for _, v := range *d.Req.Visits {
			if v.Status == gen.VisitStatusCompleted || v.Status == gen.VisitStatusClientConfirmed {
				d.RateVisit = v.Id.String()
			}
		}
	}
	return d, nil
}

func (p *Portal) requestPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "id")
	if !ok {
		p.failPage(w, r, &apiError{Status: http.StatusNotFound})
		return
	}
	p.showRequest(w, r, id, "")
}

func (p *Portal) showRequest(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, errMsg string) {
	d, err := p.loadRequestDetail(r, id)
	if err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "request", &view{Title: d.Req.Service.Name + " · " + d.Req.Reference, Nav: "requests", Error: errMsg, Data: d})
}

// afterAction redirects back to the request with a message, or shows it again with the API's reason.
func (p *Portal) afterAction(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, err error, ok string) {
	if err != nil {
		p.showRequest(w, r, id, errText(err))
		return
	}
	p.flash(w, ok)
	p.redirect(w, r, p.Base+"/requests/"+id.String())
}

func (p *Portal) requestCancel(w http.ResponseWriter, r *http.Request) {
	id, _ := pathUUID(r, "id")
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if reason == "" {
		p.showRequest(w, r, id, "Tell us why you are cancelling.")
		return
	}
	err := p.call(r, gen.CancelInput{Reason: reason}, nil, func(w http.ResponseWriter, r *http.Request) { p.API.PostClientRequestsCancel(w, r, id) })
	p.afterAction(w, r, id, err, "Booking cancelled.")
}

func (p *Portal) shortlistSelect(w http.ResponseWriter, r *http.Request) {
	id, _ := pathUUID(r, "id")
	entry, ok := pathUUID(r, "entry")
	if !ok {
		p.showRequest(w, r, id, "Choose one of the providers listed.")
		return
	}
	err := p.call(r, nil, nil, func(w http.ResponseWriter, r *http.Request) { p.API.PostClientRequestsShortlistSelect(w, r, id, entry) })
	p.afterAction(w, r, id, err, "Thanks. We will confirm with the provider you chose.")
}

func (p *Portal) rate(w http.ResponseWriter, r *http.Request) {
	id, _ := pathUUID(r, "id")
	stars, _ := strconv.Atoi(r.PostFormValue("stars"))
	if stars < 1 || stars > 5 {
		p.showRequest(w, r, id, "Choose from 1 to 5 stars.")
		return
	}
	in := gen.RatingCreate{Stars: stars, Comment: optString(r.PostFormValue("comment"))}
	sub := func(k string) *int {
		n, err := strconv.Atoi(r.PostFormValue(k))
		if err != nil || n < 1 || n > 5 {
			return nil
		}
		return &n
	}
	if pu, q, c := sub("punctuality"), sub("quality"), sub("conduct"); pu != nil || q != nil || c != nil {
		in.SubScores = &struct {
			Conduct     *int `json:"conduct,omitempty"`
			Punctuality *int `json:"punctuality,omitempty"`
			Quality     *int `json:"quality,omitempty"`
		}{Conduct: c, Punctuality: pu, Quality: q}
	}
	if v, ok := formUUID(r, "visit_id"); ok {
		in.VisitId = &v
	}
	var rating gen.Rating
	err := p.call(r, in, &rating, func(w http.ResponseWriter, r *http.Request) { p.API.PostClientRequestsRating(w, r, id) })
	msg := "Thank you for rating this job."
	if err == nil && rating.ComplaintId != nil {
		msg = "Thank you. We are sorry it did not go well: our quality team has opened a case and will contact you."
	}
	p.afterAction(w, r, id, err, msg)
}

func (p *Portal) variationDecide(w http.ResponseWriter, r *http.Request) {
	vid, _ := pathUUID(r, "id")
	reqID, _ := formUUID(r, "request_id")
	var err error
	switch chiParam(r, "decision") {
	case "approve":
		err = p.call(r, nil, nil, func(w http.ResponseWriter, r *http.Request) { p.API.PostClientVariationsApprove(w, r, vid) })
	case "reject":
		reason := strings.TrimSpace(r.PostFormValue("reason"))
		if reason == "" {
			reason = "Declined by the household"
		}
		err = p.call(r, gen.ReasonInput{Reason: reason}, nil, func(w http.ResponseWriter, r *http.Request) { p.API.PostClientVariationsReject(w, r, vid) })
	default:
		http.NotFound(w, r)
		return
	}
	p.afterAction(w, r, reqID, err, "Thanks, we have recorded your answer.")
}

// doorCode shows the four-digit code the household reads to the provider at the door.
func (p *Portal) doorCode(w http.ResponseWriter, r *http.Request) {
	vid, _ := pathUUID(r, "id")
	var code gen.CheckinCode
	if err := p.call(r, nil, &code, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientVisitsCheckinCode(w, r, vid) }); err != nil {
		p.renderBlock(w, r, "request", "door-code", &view{Error: errText(err)})
		return
	}
	p.renderBlock(w, r, "request", "door-code", &view{Data: code})
}

func (p *Portal) visitConfirm(w http.ResponseWriter, r *http.Request) {
	vid, _ := pathUUID(r, "id")
	reqID, _ := formUUID(r, "request_id")
	err := p.call(r, nil, nil, func(w http.ResponseWriter, r *http.Request) { p.API.PostClientVisitsConfirm(w, r, vid) })
	p.afterAction(w, r, reqID, err, "Thanks for confirming the visit.")
}

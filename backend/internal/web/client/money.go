package client

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
)

func newKey() string { return uuid.NewString() }

func (p *Portal) invoicesPage(w http.ResponseWriter, r *http.Request) {
	params := gen.GetClientInvoicesParams{}
	if c := r.URL.Query().Get("cursor"); c != "" {
		cur := gen.Cursor(c)
		params.Cursor = &cur
	}
	var page invoicePage
	if err := p.call(r, nil, &page, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientInvoices(w, r, params) }); err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "invoices", &view{Title: "Payments", Nav: "invoices", Data: page})
}

type invoiceData struct {
	Inv   gen.Invoice
	Phone string // prefilled M-Pesa number
	Key   string
}

func (p *Portal) invoiceData(r *http.Request, id openapi_types.UUID) (*invoiceData, error) {
	d := &invoiceData{Key: newKey()}
	if err := p.call(r, nil, &d.Inv, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientInvoicesById(w, r, id) }); err != nil {
		return nil, err
	}
	var me gen.Me
	if p.call(r, nil, &me, p.API.GetMe) == nil && me.Phone != nil {
		d.Phone = localPhone(*me.Phone)
	}
	return d, nil
}

func (p *Portal) invoicePage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "id")
	if !ok {
		p.failPage(w, r, &apiError{Status: http.StatusNotFound})
		return
	}
	d, err := p.invoiceData(r, id)
	if err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "invoice", &view{Title: "Invoice " + d.Inv.Reference, Nav: "invoices", Data: d})
}

// invoicePay sends the M-Pesa STK prompt to the phone, then shows a status panel that polls until
// Safaricom's callback settles the payment.
func (p *Portal) invoicePay(w http.ResponseWriter, r *http.Request) {
	id, _ := pathUUID(r, "id")
	phone := phoneE164(r.PostFormValue("phone"))
	key := gen.IdempotencyKey(r.PostFormValue("key"))
	var pay gen.Payment
	err := p.call(r, gen.StkPushInput{Phone: phone}, &pay, func(w http.ResponseWriter, r *http.Request) {
		p.API.PostClientInvoicesPay(w, r, id, gen.PostClientInvoicesPayParams{IdempotencyKey: &key})
	})
	if err != nil {
		d, lerr := p.invoiceData(r, id)
		if lerr != nil {
			p.failPage(w, r, lerr)
			return
		}
		d.Phone = r.PostFormValue("phone")
		v := &view{Title: "Invoice " + d.Inv.Reference, Nav: "invoices", Error: errText(err), Data: d}
		if isHTMX(r) {
			p.renderBlock(w, r, "invoice", "pay", v)
			return
		}
		p.render(w, r, "invoice", v)
		return
	}
	if isHTMX(r) {
		p.renderBlock(w, r, "invoice", "payment-status", &view{Data: pay})
		return
	}
	p.redirect(w, r, p.Base+"/payments/"+pay.Id.String())
}

// paymentStatus is polled by the status panel; without HTMX it is a page that refreshes itself.
func (p *Portal) paymentStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "id")
	if !ok {
		p.failPage(w, r, &apiError{Status: http.StatusNotFound})
		return
	}
	var pay gen.Payment
	if err := p.call(r, nil, &pay, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientPaymentsById(w, r, id) }); err != nil {
		p.failPage(w, r, err)
		return
	}
	if isHTMX(r) {
		if pay.Status == gen.PaymentStatusSucceeded {
			// Reload the invoice so it shows as paid.
			w.Header().Set("HX-Refresh", "true")
		}
		p.renderBlock(w, r, "invoice", "payment-status", &view{Data: pay})
		return
	}
	p.render(w, r, "payment", &view{Title: "Payment", Nav: "invoices", Data: pay})
}

// ---- household subscription plans

type plansData struct {
	Plans   []gen.SubscriptionPlan
	Current *gen.Subscription
	Phone   string
	Key     string
}

func (p *Portal) plansData(r *http.Request) (*plansData, error) {
	d := &plansData{Key: newKey()}
	if err := p.call(r, nil, &d.Plans, p.API.GetPublicSubscriptionPlans); err != nil {
		return nil, err
	}
	var sub gen.Subscription
	if p.call(r, nil, &sub, p.API.GetClientSubscription) == nil {
		d.Current = &sub
	}
	var me gen.Me
	if p.call(r, nil, &me, p.API.GetMe) == nil && me.Phone != nil {
		d.Phone = localPhone(*me.Phone)
	}
	return d, nil
}

func (p *Portal) plansPage(w http.ResponseWriter, r *http.Request) {
	p.showPlans(w, r, "")
}

func (p *Portal) showPlans(w http.ResponseWriter, r *http.Request, errMsg string) {
	d, err := p.plansData(r)
	if err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "plans", &view{Title: "Plans", Nav: "account", Error: errMsg, Data: d})
}

func (p *Portal) subscribe(w http.ResponseWriter, r *http.Request) {
	plan, ok := formUUID(r, "plan_id")
	if !ok {
		p.showPlans(w, r, "Choose a plan.")
		return
	}
	in := gen.SubscriptionCreate{PlanId: plan, AcceptTerms: r.PostFormValue("terms") != ""}
	if ph := strings.TrimSpace(r.PostFormValue("phone")); ph != "" {
		e := phoneE164(ph)
		in.PayPhone = &e
	}
	key := gen.IdempotencyKey(r.PostFormValue("key"))
	if err := p.call(r, in, nil, func(w http.ResponseWriter, r *http.Request) {
		p.API.PostClientSubscription(w, r, gen.PostClientSubscriptionParams{IdempotencyKey: &key})
	}); err != nil {
		p.showPlans(w, r, errText(err))
		return
	}
	msg := "Plan started. It becomes active when the first payment arrives."
	if in.PayPhone != nil {
		msg = "Plan started. Approve the M-Pesa prompt on your phone to activate it."
	}
	p.flash(w, msg)
	p.redirect(w, r, p.Base+"/plans")
}

func (p *Portal) subscriptionCancel(w http.ResponseWriter, r *http.Request) {
	if err := p.call(r, nil, nil, p.API.PostClientSubscriptionCancel); err != nil {
		p.showPlans(w, r, errText(err))
		return
	}
	p.flash(w, "Your plan is cancelled.")
	p.redirect(w, r, p.Base+"/plans")
}

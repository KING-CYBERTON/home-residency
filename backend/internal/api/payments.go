package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/domain"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// M-Pesa moves whole shillings, so a payment amount is a multiple of 100 cents and an invoice counts
// as settled once less than one shilling is outstanding.
const settledTolerance = 100

// ---- ledger

type ledgerLine struct {
	account string
	amount  int64 // debit positive, credit negative
}

type ledgerRef struct {
	client, provider, invoice, payment, payout, refund *uuid.UUID
}

// postJournal writes one balanced journal. A journal that does not sum to zero is a programming error.
func postJournal(ctx context.Context, q db, memo string, ref ledgerRef, lines ...ledgerLine) error {
	var sum int64
	for _, l := range lines {
		sum += l.amount
	}
	if sum != 0 {
		return fmt.Errorf("unbalanced journal %q: %d", memo, sum)
	}
	journal := uuid.New()
	for _, l := range lines {
		if l.amount == 0 {
			continue
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO ledger_entries (journal_id, account, amount_minor, client_id, provider_id, invoice_id, payment_id, payout_id, refund_id, memo)
			VALUES ($1,$2::ledger_account,$3,$4,$5,$6,$7,$8,$9,$10)`,
			journal, l.account, l.amount, ref.client, ref.provider, ref.invoice, ref.payment, ref.payout, ref.refund, memo); err != nil {
			return err
		}
	}
	return nil
}

func journalExists(ctx context.Context, q db, memo string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ledger_entries WHERE memo = $1)`, memo).Scan(&ok)
	return ok, err
}

// ---- invoices

type invoiceLine struct {
	kind, description string
	quantity          float64
	unit, amount      int64
	variationID       *uuid.UUID
}

// createRequestInvoice issues the invoice for a finished job from the price snapshot on the request
// plus any approved variations. It is safe to call twice.
func (s *Server) createRequestInvoice(ctx context.Context, q db, requestID uuid.UUID) (uuid.UUID, error) {
	var existing uuid.UUID
	err := q.QueryRow(ctx, `SELECT id FROM invoices WHERE request_id = $1 AND status <> 'void' ORDER BY created_at LIMIT 1`, requestID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	var clientID uuid.UUID
	var svcName string
	var qty float64
	var unit, placement int64
	if err := q.QueryRow(ctx, `
		SELECT r.client_id, sv.name, r.quantity::float8, r.client_price_minor, r.placement_fee_minor
		FROM service_requests r JOIN services sv ON sv.id = r.service_id WHERE r.id = $1`, requestID).
		Scan(&clientID, &svcName, &qty, &unit, &placement); err != nil {
		return uuid.Nil, mapErr(err)
	}
	work := int64(math.Round(float64(unit) * qty))
	lines := []invoiceLine{{"service", svcName, qty, unit, work, nil}}
	if placement > 0 {
		lines = append(lines, invoiceLine{"placement_fee", "Placement fee", 1, placement, placement, nil})
	}
	rows, err := q.Query(ctx, `SELECT id, description, client_delta_minor FROM variations WHERE request_id = $1 AND status = 'approved' ORDER BY created_at`, requestID)
	if err != nil {
		return uuid.Nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var desc string
		var delta int64
		if err := rows.Scan(&id, &desc, &delta); err != nil {
			rows.Close()
			return uuid.Nil, err
		}
		vid := id
		lines = append(lines, invoiceLine{"variation", desc, 1, delta, delta, &vid})
		work += delta
	}
	rows.Close()
	return s.insertInvoice(ctx, q, clientID, &requestID, nil, lines, work)
}

// insertInvoice issues an invoice for VAT-inclusive lines. workMinor is the part of them that is the
// provider's work (service and variations); the rest belongs to the platform. The total is rounded to
// a whole shilling with a rounding line, and the VAT included in it is recorded.
func (s *Server) insertInvoice(ctx context.Context, q db, clientID uuid.UUID, requestID, subscriptionID *uuid.UUID, lines []invoiceLine, workMinor int64) (uuid.UUID, error) {
	var sum int64
	for _, l := range lines {
		sum += l.amount
	}
	pct := s.setting(ctx, q, "vat.rate_pct", 16)
	price := domain.PriceOf(sum, 0, pct)
	if price.RoundingMinor != 0 {
		lines = append(lines, invoiceLine{"rounding", "Rounding to the nearest shilling", 1, price.RoundingMinor, price.RoundingMinor, nil})
	}
	var id uuid.UUID
	if err := q.QueryRow(ctx, `
		INSERT INTO invoices (client_id, request_id, subscription_id, status, subtotal_minor, vat_minor, total_minor, vat_rate_pct, work_net_minor, issued_at, due_at)
		VALUES ($1,$2,$3,'issued',$4,$5,$6,$7,$8, now(), now() + interval '7 days') RETURNING id`,
		clientID, requestID, subscriptionID, price.SubtotalMinor, price.VATMinor, price.TotalMinor, pct, domain.NetOfVAT(workMinor, pct)).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	for _, l := range lines {
		if _, err := q.Exec(ctx, `
			INSERT INTO invoice_lines (invoice_id, kind, description, quantity, unit_minor, amount_minor, variation_id)
			VALUES ($1,$2::invoice_line_kind,$3,$4,$5,$6,$7)`, id, l.kind, l.description, l.quantity, l.unit, l.amount, l.variationID); err != nil {
			return uuid.Nil, err
		}
	}
	if err := notify(ctx, q, clientID, "invoice.issued", "Invoice "+"issued", "Your invoice of "+fmtMoney(price.TotalMinor)+" is ready to pay.", map[string]any{"invoice_id": id}); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

const invoiceJSON = `
	SELECT to_jsonb(i) - 'created_at' - 'updated_at' || jsonb_build_object('lines',
	  COALESCE((SELECT jsonb_agg(jsonb_build_object('id', l.id, 'kind', l.kind, 'description', l.description, 'quantity', l.quantity,
	    'unit_minor', l.unit_minor, 'amount_minor', l.amount_minor) ORDER BY l.kind, l.id) FROM invoice_lines l WHERE l.invoice_id = i.id), '[]'::jsonb))
	FROM invoices i`

// ---- applying a payment

// applyPayment books a succeeded payment against its invoice and runs what follows from the invoice
// becoming paid. The payment row must already be marked succeeded; the caller holds the transaction.
func (s *Server) applyPayment(ctx context.Context, q db, paymentID uuid.UUID) error {
	var invoiceID, clientID uuid.UUID
	var amount int64
	if err := q.QueryRow(ctx, `SELECT invoice_id, client_id, amount_minor FROM payments WHERE id = $1`, paymentID).Scan(&invoiceID, &clientID, &amount); err != nil {
		return err
	}
	if err := postJournal(ctx, q, "payment:"+paymentID.String(), ledgerRef{client: &clientID, invoice: &invoiceID, payment: &paymentID},
		ledgerLine{"mpesa_clearing", amount}, ledgerLine{"client_funds", -amount}); err != nil {
		return err
	}
	var total, paid int64
	var status string
	if err := q.QueryRow(ctx, `SELECT total_minor, paid_minor, status::text FROM invoices WHERE id = $1 FOR UPDATE`, invoiceID).Scan(&total, &paid, &status); err != nil {
		return err
	}
	paid += amount
	newStatus := "partially_paid"
	if total-paid < settledTolerance {
		newStatus = "paid"
	}
	if status == "void" {
		// Money arrived for a voided invoice: keep it on the books and let finance refund it.
		newStatus = "void"
	}
	if _, err := q.Exec(ctx, `UPDATE invoices SET paid_minor = $2, status = $3::invoice_status, paid_at = CASE WHEN $3 = 'paid' THEN now() ELSE paid_at END WHERE id = $1`,
		invoiceID, paid, newStatus); err != nil {
		return err
	}
	if err := notify(ctx, q, clientID, "payment.received", "Payment received", "We received "+fmtMoney(amount)+". Thank you.", map[string]any{"invoice_id": invoiceID, "payment_id": paymentID}); err != nil {
		return err
	}
	if newStatus == "paid" && status != "paid" {
		return s.onInvoicePaid(ctx, q, invoiceID)
	}
	return nil
}

// onInvoicePaid books the platform's share of the invoice, activates a paid subscription and lets the
// provider's earnings become payable.
func (s *Server) onInvoicePaid(ctx context.Context, q db, invoiceID uuid.UUID) error {
	var clientID uuid.UUID
	var requestID, subscriptionID *uuid.UUID
	var vat, total, workNet int64
	if err := q.QueryRow(ctx, `SELECT client_id, request_id, subscription_id, vat_minor, total_minor, work_net_minor FROM invoices WHERE id = $1`, invoiceID).
		Scan(&clientID, &requestID, &subscriptionID, &vat, &total, &workNet); err != nil {
		return err
	}
	// All the VAT is due now. The provider's work, without its VAT, is split per earning when the job
	// is closed (work_net_minor, the same figure the earning uses); everything else is platform revenue.
	platform := total - vat - workNet
	if err := postJournal(ctx, q, "invoice.settle:"+invoiceID.String(), ledgerRef{client: &clientID, invoice: &invoiceID},
		ledgerLine{"client_funds", platform + vat}, ledgerLine{"platform_revenue", -platform}, ledgerLine{"vat_payable", -vat}); err != nil {
		return err
	}
	if subscriptionID != nil {
		if _, err := q.Exec(ctx, `UPDATE subscriptions SET status = 'active' WHERE id = $1 AND status = 'past_due'`, *subscriptionID); err != nil {
			return err
		}
	}
	if requestID != nil {
		return s.releaseEarnings(ctx, q, *requestID)
	}
	return nil
}

// releaseEarnings makes a closed, fully paid job's pending earnings payable and books the split.
func (s *Server) releaseEarnings(ctx context.Context, q db, requestID uuid.UUID) error {
	var closed bool
	var unpaid int
	if err := q.QueryRow(ctx, `
		SELECT r.status = 'closed', (SELECT count(*) FROM invoices i WHERE i.request_id = r.id AND i.status NOT IN ('void','paid','refunded'))
		FROM service_requests r WHERE r.id = $1`, requestID).Scan(&closed, &unpaid); err != nil {
		return err
	}
	var invoices int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE request_id = $1 AND status = 'paid'`, requestID).Scan(&invoices); err != nil {
		return err
	}
	if !closed || unpaid > 0 || invoices == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `SELECT id FROM provider_earnings WHERE request_id = $1 AND status = 'pending'`, requestID)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.makeEarningPayable(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) makeEarningPayable(ctx context.Context, q db, earningID uuid.UUID) error {
	var provider uuid.UUID
	var net, commission, withholding int64
	if err := q.QueryRow(ctx, `UPDATE provider_earnings SET status = 'payable', hold_reason = NULL WHERE id = $1
		RETURNING provider_id, net_minor, commission_minor, withholding_minor`, earningID).Scan(&provider, &net, &commission, &withholding); err != nil {
		return err
	}
	memo := "earning.payable:" + earningID.String()
	done, err := journalExists(ctx, q, memo)
	if err != nil || done {
		return err
	}
	return postJournal(ctx, q, memo, ledgerRef{provider: &provider},
		ledgerLine{"client_funds", net + commission + withholding},
		ledgerLine{"provider_payable", -net}, ledgerLine{"platform_revenue", -commission}, ledgerLine{"withholding_payable", -withholding})
}

// ---- phone numbers

// daraja turns +2547XXXXXXXX into the 2547XXXXXXXX form Daraja expects.
func daraja(phone string) string { return strings.TrimPrefix(phone, "+") }

func shillings(minor int64) int64 {
	k := int64(math.Round(float64(minor) / 100))
	if k < 1 {
		k = 1
	}
	return k
}

func (s *Server) callbackURL(path string) string {
	u := strings.TrimRight(s.Cfg.PublicBaseURL, "/") + BaseURL + path
	if s.Cfg.WebhookToken != "" {
		u += "?token=" + s.Cfg.WebhookToken
	}
	return u
}

func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

var _ = httpx.ErrNotFound
var _ = time.Now

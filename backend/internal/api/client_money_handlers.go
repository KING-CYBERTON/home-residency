package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
	"github.com/KING-CYBERTON/home-residency/backend/internal/mpesa"
)

const paymentJSON = `
	SELECT jsonb_build_object('id', p.id, 'invoice_id', p.invoice_id, 'method', p.method, 'status', p.status, 'amount_minor', p.amount_minor,
	  'payer_msisdn', p.payer_msisdn, 'mpesa_receipt_number', p.mpesa_receipt_number, 'failure_reason', p.failure_reason,
	  'initiated_at', p.initiated_at, 'completed_at', p.completed_at)
	FROM payments p`

// ---- invoices

func (s *Server) GetClientInvoices(w http.ResponseWriter, r *http.Request, params gen.GetClientInvoicesParams) {
	ctx := r.Context()
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	b.add("i.client_id = ?", auth.MustPrincipal(ctx).UserID)
	if params.Status != nil {
		b.add("i.status = ?::invoice_status", string(*params.Status))
	}
	p, err := pagedRaw(ctx, s.DB, `SELECT i.id, i.reference, i.client_id, i.request_id, i.subscription_id, i.status, i.subtotal_minor, i.vat_minor, i.total_minor,
		i.paid_minor, i.currency, i.due_at, i.issued_at, i.paid_at, i.created_at AS _t, i.id AS _id
		FROM invoices i WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (s *Server) GetClientInvoicesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	raw, err := rawOne(r.Context(), s.DB, invoiceJSON+` WHERE i.id = $1 AND i.client_id = $2`, uuid.UUID(id), auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// ---- paying

// startSTK records a payment attempt and sends the STK prompt. Money moves only when the callback arrives.
func (s *Server) startSTK(ctx context.Context, clientID, invoiceID uuid.UUID, phone string, amountMinor *int64) (uuid.UUID, error) {
	if !phoneRE.MatchString(phone) {
		return uuid.Nil, httpx.Invalid("phone must be in international format, e.g. +254712345678")
	}
	var paymentID uuid.UUID
	var ref string
	var amount int64
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var total, paid int64
		var status string
		if err := tx.QueryRow(ctx, `SELECT reference, total_minor, paid_minor, status::text FROM invoices WHERE id = $1 AND client_id = $2 FOR UPDATE`, invoiceID, clientID).
			Scan(&ref, &total, &paid, &status); err != nil {
			return err
		}
		if status != "issued" && status != "partially_paid" {
			return httpx.Conflict("This invoice cannot be paid (status: " + status + ")")
		}
		due := total - paid
		amount = due
		if amountMinor != nil {
			if *amountMinor <= 0 || *amountMinor > due+settledTolerance {
				return httpx.Invalid("amount_minor must be between 1 and the amount outstanding")
			}
			amount = *amountMinor
		}
		amount = shillings(amount) * 100
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM payments WHERE invoice_id = $1 AND status IN ('initiated','pending') AND initiated_at > now() - interval '90 seconds'`, invoiceID).Scan(&recent); err != nil {
			return err
		}
		if recent > 0 {
			return httpx.Conflict("A payment prompt for this invoice is already waiting on the phone")
		}
		return tx.QueryRow(ctx, `
			INSERT INTO payments (invoice_id, client_id, method, status, amount_minor, payer_msisdn)
			VALUES ($1,$2,'mpesa_stk','initiated',$3,$4) RETURNING id`, invoiceID, clientID, amount, phone).Scan(&paymentID)
	})
	if err != nil {
		return uuid.Nil, mapErr(err)
	}
	res, err := s.Mpesa.STKPush(ctx, mpesa.STKPush{MSISDN: daraja(phone), AmountKES: amount / 100, Reference: ref,
		Description: "Homend Residence " + ref, CallbackURL: s.callbackURL("/webhooks/mpesa/stk")})
	if err != nil {
		_, _ = s.DB.Exec(ctx, `UPDATE payments SET status = 'failed', failure_reason = $2 WHERE id = $1`, paymentID, "Could not reach M-Pesa: "+err.Error())
		return uuid.Nil, httpx.NewError(http.StatusBadGateway, "payment_provider_unavailable", "Could not send the M-Pesa prompt. Try again in a moment.")
	}
	_, err = s.DB.Exec(ctx, `UPDATE payments SET status = 'pending', mpesa_checkout_request_id = $2 WHERE id = $1`, paymentID, res.CheckoutRequestID)
	return paymentID, err
}

func (s *Server) PostClientInvoicesPay(w http.ResponseWriter, r *http.Request, id gen.Id, params gen.PostClientInvoicesPayParams) {
	s.idempotent(w, r, (*string)(params.IdempotencyKey), func(_ []byte) (int, any, error) {
		in, err := httpx.Decode[gen.StkPushInput](r)
		if err != nil {
			return 0, nil, err
		}
		pid, err := s.startSTK(r.Context(), auth.MustPrincipal(r.Context()).UserID, uuid.UUID(id), in.Phone, (*int64)(in.AmountMinor))
		if err != nil {
			return 0, nil, err
		}
		raw, err := rawOne(r.Context(), s.DB, paymentJSON+` WHERE p.id = $1`, pid)
		return http.StatusAccepted, raw, err
	})
}

func (s *Server) GetClientPaymentsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	raw, err := rawOne(r.Context(), s.DB, paymentJSON+` WHERE p.id = $1 AND p.client_id = $2`, uuid.UUID(id), auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// ---- subscriptions

const subscriptionJSON = `
	SELECT jsonb_build_object('id', s.id, 'status', s.status, 'current_period_start', s.current_period_start,
	  'current_period_end', s.current_period_end, 'cancel_at_period_end', s.cancel_at_period_end,
	  'plan', jsonb_build_object('id', pl.id, 'code', pl.code, 'name', pl.name, 'description', pl.description, 'price_minor', pl.price_minor,
	    'billing_period', pl.billing_period, 'benefits', pl.benefits, 'is_active', pl.is_active))
	FROM subscriptions s JOIN subscription_plans pl ON pl.id = s.plan_id`

func (s *Server) GetClientSubscription(w http.ResponseWriter, r *http.Request) {
	raw, err := rawOne(r.Context(), s.DB, subscriptionJSON+` WHERE s.client_id = $1 AND s.status IN ('active','past_due') ORDER BY s.created_at DESC LIMIT 1`,
		auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PostClientSubscription(w http.ResponseWriter, r *http.Request, params gen.PostClientSubscriptionParams) {
	s.idempotent(w, r, (*string)(params.IdempotencyKey), func(_ []byte) (int, any, error) {
		in, err := httpx.Decode[gen.SubscriptionCreate](r)
		if err != nil {
			return 0, nil, err
		}
		if in.PayPhone != nil && !phoneRE.MatchString(*in.PayPhone) {
			return 0, nil, httpx.Invalid("pay_phone must be in international format")
		}
		ctx := r.Context()
		cid := auth.MustPrincipal(ctx).UserID
		var subID, invoiceID uuid.UUID
		err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			if err := acceptTerms(ctx, tx, r, cid, in.AcceptTerms); err != nil {
				return err
			}
			var name, period string
			var price int64
			if err := tx.QueryRow(ctx, `SELECT name, price_minor, billing_period FROM subscription_plans WHERE id = $1 AND is_active`, uuid.UUID(in.PlanId)).Scan(&name, &price, &period); err != nil {
				return err
			}
			start := s.Now().UTC()
			end := start.AddDate(0, 1, 0)
			switch period {
			case "quarterly":
				end = start.AddDate(0, 3, 0)
			case "annual":
				end = start.AddDate(1, 0, 0)
			}
			// Starts past_due: it turns active when the first invoice is paid.
			if err := tx.QueryRow(ctx, `
				INSERT INTO subscriptions (client_id, plan_id, status, current_period_start, current_period_end)
				VALUES ($1,$2,'past_due',$3,$4) RETURNING id`, cid, uuid.UUID(in.PlanId), start, end).Scan(&subID); err != nil {
				return err
			}
			invoiceID, err = s.insertInvoice(ctx, tx, cid, nil, &subID,
				[]invoiceLine{{"subscription", name + " subscription (" + period + ")", 1, price, price, nil}}, 0)
			if err != nil {
				return err
			}
			return audit(ctx, tx, "subscription.start", "subscription", subID, nil, map[string]any{"plan_id": in.PlanId}, "")
		})
		if err != nil {
			return 0, nil, mapErr(err)
		}
		if in.PayPhone != nil && price0(ctx, s.DB, invoiceID) > 0 {
			// The subscription exists either way; a failed prompt can be retried from the invoice.
			if _, err := s.startSTK(ctx, cid, invoiceID, *in.PayPhone, nil); err != nil {
				return 0, nil, err
			}
		}
		raw, err := rawOne(ctx, s.DB, subscriptionJSON+` WHERE s.id = $1`, subID)
		return http.StatusCreated, raw, err
	})
}

func price0(ctx context.Context, q db, invoiceID uuid.UUID) int64 {
	var t int64
	_ = q.QueryRow(ctx, `SELECT total_minor FROM invoices WHERE id = $1`, invoiceID).Scan(&t)
	return t
}

func (s *Server) PostClientSubscriptionCancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	var subID uuid.UUID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT id, status::text FROM subscriptions WHERE client_id = $1 AND status IN ('active','past_due') FOR UPDATE`, cid).Scan(&subID, &status); err != nil {
			return err
		}
		if status == "past_due" {
			// Never paid, so there is no period to run out: cancel now and void the unpaid invoice.
			if _, err := tx.Exec(ctx, `UPDATE subscriptions SET status = 'cancelled', cancelled_at = now() WHERE id = $1`, subID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE invoices SET status = 'void' WHERE subscription_id = $1 AND status = 'issued'`, subID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE subscriptions SET cancel_at_period_end = true WHERE id = $1`, subID); err != nil {
			return err
		}
		return audit(ctx, tx, "subscription.cancel", "subscription", subID, nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, subscriptionJSON+` WHERE s.id = $1`, subID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

var (
	_ = json.Marshal
	_ = time.Now
)

package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
	"github.com/KING-CYBERTON/home-residency/backend/internal/mpesa"
)

func reasonOf(r *http.Request) (string, error) {
	in, err := httpx.Decode[gen.ReasonInput](r)
	if err != nil {
		return "", err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.Reason) < 3 {
		return "", httpx.Invalid("reason is required")
	}
	return in.Reason, nil
}

// ---- invoices

func (s *Server) GetConsoleInvoices(w http.ResponseWriter, r *http.Request, params gen.GetConsoleInvoicesParams) {
	ctx := r.Context()
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("i.status = ?::invoice_status", string(*params.Status))
	}
	if params.ClientId != nil {
		b.add("i.client_id = ?", uuid.UUID(*params.ClientId))
	}
	if params.From != nil {
		b.add("i.created_at >= ?", params.From.Time)
	}
	if params.To != nil {
		b.add("i.created_at < ?", params.To.Time.AddDate(0, 0, 1))
	}
	p, err := pagedRaw(ctx, s.DB, `SELECT i.id, i.reference, i.client_id, i.request_id, i.subscription_id, i.status, i.subtotal_minor, i.vat_minor, i.total_minor,
		i.paid_minor, i.currency, i.due_at, i.issued_at, i.paid_at, u.full_name AS client_name, i.created_at AS _t, i.id AS _id
		FROM invoices i JOIN users u ON u.id = i.client_id WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (s *Server) GetConsoleInvoicesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	raw, err := rawOne(r.Context(), s.DB, invoiceJSON+` WHERE i.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PostConsoleInvoicesVoid(w http.ResponseWriter, r *http.Request, id gen.Id) {
	reason, err := reasonOf(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var paid int64
		if err := tx.QueryRow(ctx, `SELECT status::text, paid_minor FROM invoices WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status, &paid); err != nil {
			return err
		}
		if (status != "issued" && status != "draft") || paid > 0 {
			return httpx.Conflict("Only an unpaid invoice can be voided. Refund a paid one instead.")
		}
		if _, err := tx.Exec(ctx, `UPDATE invoices SET status = 'void' WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		return audit(ctx, tx, "invoice.void", "invoice", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": "void"}, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.GetConsoleInvoicesById(w, r, id)
}

// ---- payments

func (s *Server) GetConsolePayments(w http.ResponseWriter, r *http.Request, params gen.GetConsolePaymentsParams) {
	ctx := r.Context()
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("p.status = ?::payment_status", string(*params.Status))
	}
	if params.Method != nil {
		b.add("p.method = ?::payment_method", string(*params.Method))
	}
	if params.InvoiceId != nil {
		b.add("p.invoice_id = ?", uuid.UUID(*params.InvoiceId))
	}
	if params.ClientId != nil {
		b.add("i.client_id = ?", uuid.UUID(*params.ClientId))
	}
	if params.From != nil {
		b.add("p.initiated_at >= ?", params.From.Time)
	}
	if params.To != nil {
		b.add("p.initiated_at < ?", params.To.Time.AddDate(0, 0, 1))
	}
	pg, err := pagedRaw(ctx, s.DB, `SELECT p.id, p.invoice_id, p.method, p.status, p.amount_minor, p.payer_msisdn, p.mpesa_receipt_number, p.failure_reason,
		p.initiated_at, p.completed_at, i.reference AS invoice_reference, i.client_id, u.full_name AS client_name, p.initiated_at AS _t, p.id AS _id
		FROM payments p JOIN invoices i ON i.id = p.invoice_id JOIN users u ON u.id = i.client_id WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

// PostConsolePaymentsManual records money that arrived outside an STK prompt: a paybill payment the
// callback missed, a bank transfer or cash. The reference makes the entry idempotent.
func (s *Server) PostConsolePaymentsManual(w http.ResponseWriter, r *http.Request, params gen.PostConsolePaymentsManualParams) {
	s.idempotent(w, r, (*string)(params.IdempotencyKey), func(_ []byte) (int, any, error) {
		in, err := httpx.Decode[gen.ManualPaymentInput](r)
		if err != nil {
			return 0, nil, err
		}
		ref := strings.TrimSpace(in.Reference)
		if in.AmountMinor <= 0 || ref == "" {
			return 0, nil, httpx.Invalid("amount_minor and reference are required")
		}
		ctx := r.Context()
		var pid uuid.UUID
		err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			var clientID uuid.UUID
			var total, paid int64
			var status string
			if err := tx.QueryRow(ctx, `SELECT client_id, total_minor, paid_minor, status::text FROM invoices WHERE id = $1 FOR UPDATE`, uuid.UUID(in.InvoiceId)).Scan(&clientID, &total, &paid, &status); err != nil {
				return err
			}
			if status != "issued" && status != "partially_paid" {
				return httpx.Conflict("This invoice cannot take a payment (status: " + status + ")")
			}
			if in.AmountMinor > total-paid+settledTolerance {
				return httpx.Invalid("Amount is more than the invoice has outstanding")
			}
			completed := s.Now()
			if in.ReceivedAt != nil {
				completed = *in.ReceivedAt
			}
			var receipt, idem *string
			if in.Method == "mpesa_paybill" {
				receipt = &ref
			} else {
				k := string(in.Method) + ":" + ref
				idem = &k
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO payments (invoice_id, client_id, method, status, amount_minor, mpesa_receipt_number, idempotency_key, recorded_by, completed_at)
				VALUES ($1,$2,$3::payment_method,'succeeded',$4,$5,$6,$7,$8) RETURNING id`,
				uuid.UUID(in.InvoiceId), clientID, string(in.Method), in.AmountMinor, receipt, idem, actorID(ctx), completed).Scan(&pid); err != nil {
				return err
			}
			if err := s.applyPayment(ctx, tx, pid); err != nil {
				return err
			}
			return audit(ctx, tx, "payment.manual", "payment", pid, nil, map[string]any{"method": in.Method, "reference": ref, "amount_minor": in.AmountMinor}, "")
		})
		if err != nil {
			return 0, nil, mapErr(err)
		}
		raw, err := rawOne(ctx, s.DB, paymentJSON+` WHERE p.id = $1`, pid)
		return http.StatusCreated, raw, err
	})
}

// PostConsolePaymentsRequery asks Daraja what happened to a prompt whose callback never arrived.
func (s *Server) PostConsolePaymentsRequery(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	var checkout *string
	var status string
	if err := s.DB.QueryRow(ctx, `SELECT mpesa_checkout_request_id, status::text FROM payments WHERE id = $1`, uuid.UUID(id)).Scan(&checkout, &status); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if checkout == nil || (status != "initiated" && status != "pending") {
		httpx.Fail(w, r, httpx.Conflict("Only a payment still waiting on M-Pesa can be re-queried (status: "+status+")"))
		return
	}
	st, err := s.Mpesa.STKQuery(ctx, *checkout)
	if err != nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadGateway, "payment_provider_unavailable", "Could not reach M-Pesa"))
		return
	}
	if st.Done {
		err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			if st.Success {
				if err := s.succeedPayment(ctx, tx, uuid.UUID(id), nil, nil, nil); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'failed', failure_reason = $2, completed_at = now() WHERE id = $1 AND status IN ('initiated','pending')`, uuid.UUID(id), st.ResultDesc); err != nil {
				return err
			}
			return audit(ctx, tx, "payment.requery", "payment", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"success": st.Success}, "")
		})
		if err != nil {
			httpx.Fail(w, r, mapErr(err))
			return
		}
	}
	raw, err := rawOne(ctx, s.DB, paymentJSON+` WHERE p.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// succeedPayment marks a payment paid (once) and books it. receipt, amount and msisdn come from the callback when there is one.
func (s *Server) succeedPayment(ctx context.Context, q db, paymentID uuid.UUID, receipt *string, amountMinor *int64, msisdn *string) error {
	var status string
	if err := q.QueryRow(ctx, `SELECT status::text FROM payments WHERE id = $1 FOR UPDATE`, paymentID).Scan(&status); err != nil {
		return err
	}
	if status == "succeeded" || status == "reversed" {
		return nil
	}
	if _, err := q.Exec(ctx, `
		UPDATE payments SET status = 'succeeded', completed_at = now(), failure_reason = NULL,
		  mpesa_receipt_number = COALESCE($2, mpesa_receipt_number), amount_minor = COALESCE($3, amount_minor), payer_msisdn = COALESCE($4, payer_msisdn)
		WHERE id = $1`, paymentID, receipt, amountMinor, msisdn); err != nil {
		return err
	}
	return s.applyPayment(ctx, q, paymentID)
}

// ---- refunds

const refundJSON = `
	SELECT jsonb_build_object('id', f.id, 'payment_id', f.payment_id, 'amount_minor', f.amount_minor, 'reason', f.reason, 'complaint_id', f.complaint_id,
	  'status', f.status, 'requested_by', f.requested_by, 'approved_by', f.approved_by, 'completed_at', f.completed_at, 'created_at', f.created_at)
	FROM refunds f`

func (s *Server) GetConsoleRefunds(w http.ResponseWriter, r *http.Request, params gen.GetConsoleRefundsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("f.status = ?::refund_status", string(*params.Status))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT f.id, f.payment_id, f.amount_minor, f.reason, f.complaint_id, f.status, f.requested_by, f.approved_by, f.completed_at,
		f.created_at, f.created_at AS _t, f.id AS _id FROM refunds f WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) PostConsoleRefunds(w http.ResponseWriter, r *http.Request, params gen.PostConsoleRefundsParams) {
	s.idempotent(w, r, (*string)(params.IdempotencyKey), func(_ []byte) (int, any, error) {
		in, err := httpx.Decode[gen.RefundCreate](r)
		if err != nil {
			return 0, nil, err
		}
		if in.AmountMinor <= 0 || len(strings.TrimSpace(in.Reason)) < 3 {
			return 0, nil, httpx.Invalid("amount_minor and reason are required")
		}
		ctx := r.Context()
		var id uuid.UUID
		err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			var amount int64
			var status string
			if err := tx.QueryRow(ctx, `SELECT amount_minor, status::text FROM payments WHERE id = $1 FOR UPDATE`, uuid.UUID(in.PaymentId)).Scan(&amount, &status); err != nil {
				return err
			}
			if status != "succeeded" {
				return httpx.Conflict("Only a successful payment can be refunded (status: " + status + ")")
			}
			var taken int64
			if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor),0) FROM refunds WHERE payment_id = $1 AND status IN ('requested','approved','processing','completed')`, uuid.UUID(in.PaymentId)).Scan(&taken); err != nil {
				return err
			}
			if in.AmountMinor > amount-taken {
				return httpx.RuleViolation(fmt.Sprintf("Only %s of this payment can still be refunded", fmtMoney(amount-taken)))
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO refunds (payment_id, amount_minor, reason, complaint_id, requested_by) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
				uuid.UUID(in.PaymentId), in.AmountMinor, strings.TrimSpace(in.Reason), in.ComplaintId, *actorID(ctx)).Scan(&id); err != nil {
				return err
			}
			return audit(ctx, tx, "refund.request", "refund", id, nil, map[string]any{"amount_minor": in.AmountMinor}, in.Reason)
		})
		if err != nil {
			return 0, nil, mapErr(err)
		}
		raw, err := rawOne(ctx, s.DB, refundJSON+` WHERE f.id = $1`, id)
		return http.StatusCreated, raw, err
	})
}

func (s *Server) PostConsoleRefundsApprove(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var msisdn *string
	var amount int64
	var method string
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var requester uuid.UUID
		var paymentID, clientID uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT f.status::text, f.requested_by, f.amount_minor, p.payer_msisdn, p.method::text, p.id, p.client_id
			FROM refunds f JOIN payments p ON p.id = f.payment_id WHERE f.id = $1 FOR UPDATE OF f`, uuid.UUID(id)).
			Scan(&status, &requester, &amount, &msisdn, &method, &paymentID, &clientID); err != nil {
			return err
		}
		if status != "requested" {
			return httpx.Conflict("This refund is already " + status)
		}
		if requester == me {
			return httpx.RuleViolation("A refund must be approved by someone other than the person who requested it")
		}
		next := "processing"
		if msisdn == nil || method != "mpesa_stk" && method != "mpesa_paybill" {
			// No M-Pesa number on record: finance returns the money out of band, then it is booked as done.
			next = "completed"
		}
		if _, err := tx.Exec(ctx, `UPDATE refunds SET status = $2::refund_status, approved_by = $3, completed_at = CASE WHEN $2 = 'completed' THEN now() END WHERE id = $1`,
			uuid.UUID(id), next, me); err != nil {
			return err
		}
		if next == "completed" {
			if err := s.bookRefund(ctx, tx, uuid.UUID(id)); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "refund.approve", "refund", uuid.UUID(id), map[string]any{"status": status}, map[string]any{"status": next}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if msisdn != nil && (method == "mpesa_stk" || method == "mpesa_paybill") {
		res, err := s.Mpesa.B2C(ctx, mpesa.B2C{MSISDN: daraja(*msisdn), AmountKES: shillings(amount), Remarks: "Homend Residence refund",
			OriginatorConv: id.String(), ResultURL: s.callbackURL("/webhooks/mpesa/b2c/result"), TimeoutURL: s.callbackURL("/webhooks/mpesa/b2c/timeout")})
		if err != nil {
			_, _ = s.DB.Exec(ctx, `UPDATE refunds SET status = 'failed' WHERE id = $1`, uuid.UUID(id))
		} else {
			_, _ = s.DB.Exec(ctx, `UPDATE refunds SET mpesa_conversation_id = $2 WHERE id = $1`, uuid.UUID(id), res.ConversationID)
		}
	}
	raw, err := rawOne(ctx, s.DB, refundJSON+` WHERE f.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

func (s *Server) PostConsoleRefundsReject(w http.ResponseWriter, r *http.Request, id gen.Id) {
	reason, err := reasonOf(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM refunds WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status); err != nil {
			return err
		}
		if status != "requested" {
			return httpx.Conflict("This refund is already " + status)
		}
		if _, err := tx.Exec(ctx, `UPDATE refunds SET status = 'rejected' WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		return audit(ctx, tx, "refund.reject", "refund", uuid.UUID(id), nil, nil, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, refundJSON+` WHERE f.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// bookRefund records a completed refund in the ledger and updates the payment and invoice it came from.
func (s *Server) bookRefund(ctx context.Context, q db, refundID uuid.UUID) error {
	var amount, paymentAmount, refunded, invoicePaid int64
	var paymentID, invoiceID, clientID uuid.UUID
	if err := q.QueryRow(ctx, `
		SELECT f.amount_minor, p.amount_minor, p.id, p.invoice_id, p.client_id, i.paid_minor
		FROM refunds f JOIN payments p ON p.id = f.payment_id JOIN invoices i ON i.id = p.invoice_id WHERE f.id = $1`, refundID).
		Scan(&amount, &paymentAmount, &paymentID, &invoiceID, &clientID, &invoicePaid); err != nil {
		return err
	}
	if err := postJournal(ctx, q, "refund:"+refundID.String(), ledgerRef{client: &clientID, invoice: &invoiceID, payment: &paymentID, refund: &refundID},
		ledgerLine{"client_funds", amount}, ledgerLine{"mpesa_clearing", -amount}); err != nil {
		return err
	}
	if err := q.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor),0) FROM refunds WHERE payment_id = $1 AND status = 'completed'`, paymentID).Scan(&refunded); err != nil {
		return err
	}
	if refunded >= paymentAmount {
		if _, err := q.Exec(ctx, `UPDATE payments SET status = 'reversed' WHERE id = $1`, paymentID); err != nil {
			return err
		}
		if refunded >= invoicePaid {
			if _, err := q.Exec(ctx, `UPDATE invoices SET status = 'refunded' WHERE id = $1`, invoiceID); err != nil {
				return err
			}
		}
	}
	return notify(ctx, q, clientID, "refund.completed", "Refund sent", "We refunded "+fmtMoney(amount)+".", map[string]any{"refund_id": refundID})
}

// ---- earnings

const earningJSON = `
	SELECT jsonb_build_object('id', e.id, 'request_reference', r.reference, 'visit_id', e.visit_id, 'gross_minor', e.gross_minor,
	  'commission_minor', e.commission_minor, 'withholding_minor', e.withholding_minor, 'net_minor', e.net_minor, 'status', e.status,
	  'hold_reason', e.hold_reason, 'payout_id', e.payout_id, 'earned_at', e.earned_at)
	FROM provider_earnings e JOIN service_requests r ON r.id = e.request_id`

func (s *Server) GetConsoleEarnings(w http.ResponseWriter, r *http.Request, params gen.GetConsoleEarningsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("e.status = ?::earning_status", string(*params.Status))
	}
	if params.ProviderId != nil {
		b.add("e.provider_id = ?", uuid.UUID(*params.ProviderId))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT e.id, r.reference AS request_reference, e.visit_id, e.gross_minor, e.commission_minor, e.withholding_minor, e.net_minor,
		e.status, e.hold_reason, e.payout_id, e.earned_at, e.earned_at AS _t, e.id AS _id
		FROM provider_earnings e JOIN service_requests r ON r.id = e.request_id WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) PostConsoleEarningsHold(w http.ResponseWriter, r *http.Request, id gen.Id) {
	reason, err := reasonOf(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM provider_earnings WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status); err != nil {
			return err
		}
		if status != "pending" && status != "payable" {
			return httpx.Conflict("Only a pending or payable earning can be held (status: " + status + ")")
		}
		if _, err := tx.Exec(ctx, `UPDATE provider_earnings SET status = 'held', hold_reason = $2 WHERE id = $1`, uuid.UUID(id), reason); err != nil {
			return err
		}
		return audit(ctx, tx, "earning.hold", "provider_earning", uuid.UUID(id), map[string]any{"status": status}, nil, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondEarning(w, r, uuid.UUID(id))
}

func (s *Server) PostConsoleEarningsRelease(w http.ResponseWriter, r *http.Request, id gen.Id) {
	reason, err := reasonOf(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var requestID uuid.UUID
		var ready bool
		if err := tx.QueryRow(ctx, `
			SELECT e.status::text, e.request_id,
			  r.status = 'closed' AND EXISTS (SELECT 1 FROM invoices i WHERE i.request_id = r.id AND i.status = 'paid')
			  AND NOT EXISTS (SELECT 1 FROM invoices i WHERE i.request_id = r.id AND i.status IN ('issued','partially_paid'))
			FROM provider_earnings e JOIN service_requests r ON r.id = e.request_id WHERE e.id = $1 FOR UPDATE OF e`, uuid.UUID(id)).Scan(&status, &requestID, &ready); err != nil {
			return err
		}
		if status != "held" {
			return httpx.Conflict("Only a held earning can be released (status: " + status + ")")
		}
		if ready {
			if err := s.makeEarningPayable(ctx, tx, uuid.UUID(id)); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE provider_earnings SET status = 'pending', hold_reason = NULL WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		return audit(ctx, tx, "earning.release", "provider_earning", uuid.UUID(id), map[string]any{"status": status}, nil, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.respondEarning(w, r, uuid.UUID(id))
}

func (s *Server) respondEarning(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	raw, err := rawOne(r.Context(), s.DB, earningJSON+` WHERE e.id = $1`, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// ---- payout batches

const payoutJSON = `jsonb_build_object('id', po.id, 'batch_id', po.batch_id, 'provider_id', po.provider_id, 'amount_minor', po.amount_minor, 'msisdn', po.msisdn,
	'status', po.status, 'mpesa_receipt_number', po.mpesa_receipt_number, 'failure_reason', po.failure_reason, 'paid_at', po.paid_at)`

const batchJSON = `
	SELECT jsonb_build_object('id', b.id, 'cycle_start', b.cycle_start, 'cycle_end', b.cycle_end, 'status', b.status, 'total_minor', b.total_minor,
	  'payout_count', (SELECT count(*) FROM payouts po WHERE po.batch_id = b.id), 'prepared_by', b.prepared_by, 'approved_by', b.approved_by,
	  'approved_at', b.approved_at, 'created_at', b.created_at,
	  'payouts', COALESCE((SELECT jsonb_agg(` + payoutJSON + ` ORDER BY po.created_at, po.id) FROM payouts po WHERE po.batch_id = b.id), '[]'::jsonb))
	FROM payout_batches b`

func (s *Server) GetConsolePayoutBatches(w http.ResponseWriter, r *http.Request, params gen.GetConsolePayoutBatchesParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Status != nil {
		b.add("b.status = ?::payout_status", string(*params.Status))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT b.id, b.cycle_start, b.cycle_end, b.status, b.total_minor,
		(SELECT count(*) FROM payouts po WHERE po.batch_id = b.id) AS payout_count, b.prepared_by, b.approved_by, b.approved_at, b.created_at,
		b.created_at AS _t, b.id AS _id FROM payout_batches b WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) GetConsolePayoutBatchesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	raw, err := rawOne(r.Context(), s.DB, batchJSON+` WHERE b.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// PostConsolePayoutBatches gathers every payable earning in the cycle into one payout per provider.
func (s *Server) PostConsolePayoutBatches(w http.ResponseWriter, r *http.Request, params gen.PostConsolePayoutBatchesParams) {
	s.idempotent(w, r, (*string)(params.IdempotencyKey), func(_ []byte) (int, any, error) {
		in, err := httpx.Decode[gen.PayoutBatchCreate](r)
		if err != nil {
			return 0, nil, err
		}
		if in.CycleEnd.Time.Before(in.CycleStart.Time) {
			return 0, nil, httpx.Invalid("cycle_end is before cycle_start")
		}
		ctx := r.Context()
		var id uuid.UUID
		err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			// One preparation at a time: without this, two overlapping requests both pass the overlap
			// check and both insert payouts for the same payable earnings.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('payout_batches.prepare'))`); err != nil {
				return err
			}
			var overlap int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM payout_batches WHERE status IN ('draft','approved','processing') AND daterange(cycle_start, cycle_end, '[]') && daterange($1::date, $2::date, '[]')`,
				in.CycleStart.Time, in.CycleEnd.Time).Scan(&overlap); err != nil {
				return err
			}
			if overlap > 0 {
				return httpx.Conflict("An open payout batch already covers part of this cycle")
			}
			if err := tx.QueryRow(ctx, `INSERT INTO payout_batches (cycle_start, cycle_end, prepared_by) VALUES ($1,$2,$3) RETURNING id`,
				in.CycleStart.Time, in.CycleEnd.Time, *actorID(ctx)).Scan(&id); err != nil {
				return err
			}
			// A provider with a live safeguarding or loss case is skipped until finance releases it.
			if _, err := tx.Exec(ctx, `
				INSERT INTO payouts (batch_id, provider_id, amount_minor, msisdn)
				SELECT $1, e.provider_id, sum(e.net_minor), u.phone_e164
				FROM provider_earnings e JOIN users u ON u.id = e.provider_id
				WHERE e.status = 'payable' AND e.earned_at < ($3::date + 1) AND e.earned_at >= $2::date AND u.phone_e164 IS NOT NULL
				GROUP BY e.provider_id, u.phone_e164 HAVING sum(e.net_minor) > 0`, id, in.CycleStart.Time, in.CycleEnd.Time); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE provider_earnings e SET status = 'in_payout', payout_id = po.id
				FROM payouts po WHERE po.batch_id = $1 AND e.provider_id = po.provider_id AND e.status = 'payable'
				  AND e.earned_at < ($3::date + 1) AND e.earned_at >= $2::date`, id, in.CycleStart.Time, in.CycleEnd.Time); err != nil {
				return err
			}
			var n int
			if err := tx.QueryRow(ctx, `UPDATE payout_batches SET total_minor = COALESCE((SELECT sum(amount_minor) FROM payouts WHERE batch_id = $1),0) WHERE id = $1 RETURNING (SELECT count(*) FROM payouts WHERE batch_id = $1)`, id).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return httpx.RuleViolation("No payable earnings fall in this cycle")
			}
			return audit(ctx, tx, "payout_batch.prepare", "payout_batch", id, nil, map[string]any{"payouts": n}, "")
		})
		if err != nil {
			return 0, nil, mapErr(err)
		}
		raw, err := rawOne(ctx, s.DB, batchJSON+` WHERE b.id = $1`, id)
		return http.StatusCreated, raw, err
	})
}

func (s *Server) PostConsolePayoutBatchesApprove(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var payoutIDs []uuid.UUID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var preparer uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, prepared_by FROM payout_batches WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status, &preparer); err != nil {
			return err
		}
		if status != "draft" {
			return httpx.Conflict("Only a draft batch can be approved (status: " + status + ")")
		}
		if preparer == me {
			return httpx.RuleViolation("A payout batch must be approved by someone other than the person who prepared it")
		}
		if _, err := tx.Exec(ctx, `UPDATE payout_batches SET status = 'processing', approved_by = $2, approved_at = now() WHERE id = $1`, uuid.UUID(id), me); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `UPDATE payouts SET status = 'approved' WHERE batch_id = $1 AND status = 'draft' RETURNING id`, uuid.UUID(id))
		if err != nil {
			return err
		}
		for rows.Next() {
			var pid uuid.UUID
			if err := rows.Scan(&pid); err != nil {
				rows.Close()
				return err
			}
			payoutIDs = append(payoutIDs, pid)
		}
		rows.Close()
		return audit(ctx, tx, "payout_batch.approve", "payout_batch", uuid.UUID(id), nil, map[string]any{"payouts": len(payoutIDs)}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	// Sent one by one, outside the approval transaction, so a failing payout never undoes the others.
	// The relay to SQS is not built; this loop is what it would do.
	for _, pid := range payoutIDs {
		s.dispatchPayout(ctx, pid)
	}
	s.GetConsolePayoutBatchesById(w, r, id)
}

func (s *Server) dispatchPayout(ctx context.Context, payoutID uuid.UUID) {
	var msisdn string
	var amount int64
	var attempts int16
	if err := s.DB.QueryRow(ctx, `UPDATE payouts SET attempts = attempts + 1 WHERE id = $1 AND status = 'approved' RETURNING msisdn, amount_minor, attempts`, payoutID).
		Scan(&msisdn, &amount, &attempts); err != nil {
		return
	}
	orig := payoutID.String()
	if attempts > 1 {
		orig = fmt.Sprintf("%s.%d", orig, attempts)
	}
	res, err := s.Mpesa.B2C(ctx, mpesa.B2C{MSISDN: daraja(msisdn), AmountKES: shillings(amount), Remarks: "Homend Residence payout", OriginatorConv: orig,
		ResultURL: s.callbackURL("/webhooks/mpesa/b2c/result"), TimeoutURL: s.callbackURL("/webhooks/mpesa/b2c/timeout")})
	if err != nil {
		_, _ = s.DB.Exec(ctx, `UPDATE payouts SET status = 'failed', failure_reason = $2 WHERE id = $1`, payoutID, "Could not send to M-Pesa: "+err.Error())
		_ = s.rollUpBatch(ctx, s.DB, payoutID)
		return
	}
	_, _ = s.DB.Exec(ctx, `UPDATE payouts SET status = 'processing', mpesa_conversation_id = $2 WHERE id = $1 AND status = 'approved'`, payoutID, res.ConversationID)
}

// rollUpBatch sets a batch's status from its payouts once none is still in flight.
func (s *Server) rollUpBatch(ctx context.Context, q db, payoutID uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE payout_batches b SET status = CASE
		    WHEN EXISTS (SELECT 1 FROM payouts p WHERE p.batch_id = b.id AND p.status = 'failed') THEN 'failed'::payout_status
		    ELSE 'paid'::payout_status END
		WHERE b.id = (SELECT batch_id FROM payouts WHERE id = $1) AND b.status = 'processing'
		  AND NOT EXISTS (SELECT 1 FROM payouts p WHERE p.batch_id = b.id AND p.status IN ('draft','approved','processing'))`, payoutID)
	return err
}

func (s *Server) PostConsolePayoutsRetry(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM payouts WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status); err != nil {
			return err
		}
		if status != "failed" {
			return httpx.Conflict("Only a failed payout can be retried (status: " + status + ")")
		}
		if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'approved', failure_reason = NULL WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE payout_batches SET status = 'processing' WHERE id = (SELECT batch_id FROM payouts WHERE id = $1) AND status = 'failed'`, uuid.UUID(id)); err != nil {
			return err
		}
		return audit(ctx, tx, "payout.retry", "payout", uuid.UUID(id), nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	s.dispatchPayout(ctx, uuid.UUID(id))
	raw, err := rawOne(ctx, s.DB, `SELECT `+payoutJSON+` FROM payouts po WHERE po.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

// ---- ledger and reconciliation

// GetConsoleLedgerBalances reports each account's natural balance: credit accounts (client money held,
// payables, revenue, VAT) as positive amounts owed, and clearing and refunds as positive debits.
func (s *Server) GetConsoleLedgerBalances(w http.ResponseWriter, r *http.Request, params gen.GetConsoleLedgerBalancesParams) {
	asOf := s.Now()
	if params.AsOf != nil {
		asOf = *params.AsOf
	}
	out, err := rawAll(r.Context(), s.DB, `
		SELECT jsonb_build_object('account', a.account, 'as_of', $1::timestamptz,
		  'balance_minor', CASE WHEN a.account IN ('mpesa_clearing','refunds') THEN COALESCE(sum(l.amount_minor),0) ELSE -COALESCE(sum(l.amount_minor),0) END)
		FROM unnest(enum_range(NULL::ledger_account)) AS a(account)
		LEFT JOIN ledger_entries l ON l.account = a.account AND l.posted_at <= $1
		GROUP BY a.account ORDER BY a.account`, asOf)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GetConsoleReconciliation lines M-Pesa's own callbacks up against our payments for a period.
func (s *Server) GetConsoleReconciliation(w http.ResponseWriter, r *http.Request, params gen.GetConsoleReconciliationParams) {
	ctx := r.Context()
	to := time.Now().UTC().AddDate(0, 0, 1)
	from := to.AddDate(0, 0, -31)
	if params.From != nil {
		from = params.From.Time
	}
	if params.To != nil {
		to = params.To.Time.AddDate(0, 0, 1)
	}
	var matched int
	if err := s.DB.QueryRow(ctx, `
		SELECT count(*) FROM payments p WHERE p.status = 'succeeded' AND p.method IN ('mpesa_stk','mpesa_paybill')
		  AND p.completed_at >= $1 AND p.completed_at < $2
		  AND EXISTS (SELECT 1 FROM mpesa_callbacks c WHERE c.correlation_id IN (p.mpesa_receipt_number, p.mpesa_checkout_request_id) AND c.processed_at IS NOT NULL)`, from, to).Scan(&matched); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	unmatchedMpesa, err := rawAll(ctx, s.DB, `
		SELECT c.payload FROM mpesa_callbacks c
		WHERE c.callback_type IN ('stk_result','c2b_confirmation') AND c.received_at >= $1 AND c.received_at < $2
		  AND (c.processed_at IS NULL OR c.process_error IS NOT NULL
		       -- M-Pesa collected money but the payment was refused (an amount that did not match): reconcile by hand
		       OR EXISTS (SELECT 1 FROM payments p WHERE p.mpesa_checkout_request_id = c.correlation_id
		                  AND p.status = 'failed' AND p.mpesa_receipt_number IS NOT NULL))
		ORDER BY c.received_at`, from, to)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	unmatchedPayments, err := rawAll(ctx, s.DB, paymentJSON+`
		WHERE p.status = 'succeeded' AND p.method IN ('mpesa_stk','mpesa_paybill') AND p.completed_at >= $1 AND p.completed_at < $2
		  AND NOT EXISTS (SELECT 1 FROM mpesa_callbacks c WHERE c.correlation_id IN (p.mpesa_receipt_number, p.mpesa_checkout_request_id) AND c.processed_at IS NOT NULL)
		ORDER BY p.completed_at`, from, to)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var clientFunds, providerPayable int64
	if err := s.DB.QueryRow(ctx, `
		SELECT -COALESCE(sum(amount_minor) FILTER (WHERE account = 'client_funds'),0), -COALESCE(sum(amount_minor) FILTER (WHERE account = 'provider_payable'),0)
		FROM ledger_entries WHERE posted_at < $1`, to).Scan(&clientFunds, &providerPayable); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"matched": matched, "unmatched_mpesa": unmatchedMpesa, "unmatched_payments": unmatchedPayments,
		"client_funds_balance_minor": clientFunds, "provider_payable_minor": providerPayable})
}

var _ = time.Now

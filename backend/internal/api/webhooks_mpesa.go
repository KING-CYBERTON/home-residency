package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// webhookAllowed checks the shared secret and, when configured, the source network. Daraja cannot sign
// its callbacks, so the secret rides on the callback URL we register with it.
func (s *Server) webhookAllowed(r *http.Request) bool {
	if tok := s.Cfg.WebhookToken; tok != "" {
		got := r.URL.Query().Get("token")
		if got == "" {
			got = r.Header.Get("X-Webhook-Token")
		}
		if !crypt.Equal([]byte(got), []byte(tok)) {
			return false
		}
	}
	if len(s.Cfg.WebhookCIDRs) > 0 {
		ip := net.ParseIP(clientIP(r))
		ok := false
		for _, c := range s.Cfg.WebhookCIDRs {
			if _, n, err := net.ParseCIDR(strings.TrimSpace(c)); err == nil && ip != nil && n.Contains(ip) {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

type darajaAck struct {
	ResultCode int    `json:"ResultCode"`
	ResultDesc string `json:"ResultDesc"`
}

var accepted = darajaAck{0, "Accepted"}

// readWebhook authenticates the caller and returns the raw body. A rejected call gets a plain 403 so
// nothing about the endpoint leaks.
func (s *Server) readWebhook(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if !s.webhookAllowed(r) {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || !json.Valid(raw) {
		httpx.Fail(w, r, httpx.BadRequest("Body must be JSON"))
		return nil, false
	}
	return raw, true
}

// storeCallback keeps the raw callback before anything is done with it.
func (s *Server) storeCallback(ctx context.Context, typ, correlation string, raw []byte, r *http.Request) (uuid.UUID, error) {
	var id uuid.UUID
	var ip any
	if p := net.ParseIP(clientIP(r)); p != nil {
		ip = p.String()
	}
	err := s.DB.QueryRow(ctx, `INSERT INTO mpesa_callbacks (callback_type, correlation_id, payload, source_ip) VALUES ($1,NULLIF($2,''),$3,$4::inet) RETURNING id`,
		typ, correlation, raw, ip).Scan(&id)
	return id, err
}

// handleCallback stores, processes and acknowledges. Processing failures are recorded for finance and
// retried by the sweeper; Safaricom still gets its 200 so it does not hammer the endpoint.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request, typ string, correlate func([]byte) string) {
	raw, ok := s.readWebhook(w, r)
	if !ok {
		return
	}
	id, err := s.storeCallback(r.Context(), typ, correlate(raw), raw, r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := s.processCallback(context.WithoutCancel(r.Context()), id); err != nil {
		slog.Warn("mpesa callback not processed", "callback", id, "type", typ, "err", err)
	}
	httpx.JSON(w, http.StatusOK, accepted)
}

// ---- payload shapes

type stkCallback struct {
	Body struct {
		StkCallback struct {
			MerchantRequestID string `json:"MerchantRequestID"`
			CheckoutRequestID string `json:"CheckoutRequestID"`
			ResultCode        int    `json:"ResultCode"`
			ResultDesc        string `json:"ResultDesc"`
			CallbackMetadata  struct {
				Item []struct {
					Name  string          `json:"Name"`
					Value json.RawMessage `json:"Value"`
				} `json:"Item"`
			} `json:"CallbackMetadata"`
		} `json:"stkCallback"`
	} `json:"Body"`
}

type c2bPayload struct {
	TransID       string `json:"TransID"`
	TransAmount   any    `json:"TransAmount"`
	BillRefNumber string `json:"BillRefNumber"`
	MSISDN        string `json:"MSISDN"`
}

type resultPayload struct {
	Result struct {
		ResultCode               int    `json:"ResultCode"`
		ResultDesc               string `json:"ResultDesc"`
		OriginatorConversationID string `json:"OriginatorConversationID"`
		ConversationID           string `json:"ConversationID"`
		TransactionID            string `json:"TransactionID"`
		ResultParameters         struct {
			ResultParameter []struct {
				Key   string          `json:"Key"`
				Value json.RawMessage `json:"Value"`
			} `json:"ResultParameter"`
		} `json:"ResultParameters"`
	} `json:"Result"`
}

func jsonString(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return strings.Trim(string(v), `"`)
}

func jsonFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

// ---- endpoints

func (s *Server) PostWebhooksMpesaStk(w http.ResponseWriter, r *http.Request) {
	s.handleCallback(w, r, "stk_result", func(raw []byte) string {
		var p stkCallback
		_ = json.Unmarshal(raw, &p)
		return p.Body.StkCallback.CheckoutRequestID
	})
}

func (s *Server) PostWebhooksMpesaC2BConfirmation(w http.ResponseWriter, r *http.Request) {
	s.handleCallback(w, r, "c2b_confirmation", func(raw []byte) string {
		var p c2bPayload
		_ = json.Unmarshal(raw, &p)
		return p.TransID
	})
}

// PostWebhooksMpesaC2BValidation lets Safaricom know whether the account number is an invoice we can take money for.
func (s *Server) PostWebhooksMpesaC2BValidation(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.readWebhook(w, r)
	if !ok {
		return
	}
	var p c2bPayload
	_ = json.Unmarshal(raw, &p)
	var n int
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FROM invoices WHERE upper(reference) = upper($1) AND status IN ('issued','partially_paid')`, strings.TrimSpace(p.BillRefNumber)).Scan(&n)
	id, err := s.storeCallback(r.Context(), "c2b_validation", p.TransID, raw, r)
	if err == nil {
		_, _ = s.DB.Exec(r.Context(), `UPDATE mpesa_callbacks SET processed_at = now() WHERE id = $1`, id)
	}
	if n == 0 {
		httpx.JSON(w, http.StatusOK, darajaAck{1, "Rejected: unknown or settled invoice reference"})
		return
	}
	httpx.JSON(w, http.StatusOK, accepted)
}

func resultCorrelation(raw []byte) string {
	var p resultPayload
	_ = json.Unmarshal(raw, &p)
	if p.Result.ConversationID != "" {
		return p.Result.ConversationID
	}
	return p.Result.OriginatorConversationID
}

func (s *Server) PostWebhooksMpesaB2CResult(w http.ResponseWriter, r *http.Request) {
	s.handleCallback(w, r, "b2c_result", resultCorrelation)
}

func (s *Server) PostWebhooksMpesaB2CTimeout(w http.ResponseWriter, r *http.Request) {
	s.handleCallback(w, r, "b2c_timeout", resultCorrelation)
}

func (s *Server) PostWebhooksMpesaReversalResult(w http.ResponseWriter, r *http.Request) {
	s.handleCallback(w, r, "reversal_result", resultCorrelation)
}

// ---- processing

// processCallback applies one stored callback exactly once. It is idempotent: a callback already
// processed, or one whose effect is already booked, changes nothing.
func (s *Server) processCallback(ctx context.Context, id uuid.UUID) error {
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var typ string
		var payload []byte
		var processed *time.Time
		if err := tx.QueryRow(ctx, `SELECT callback_type, payload, processed_at FROM mpesa_callbacks WHERE id = $1 FOR UPDATE`, id).Scan(&typ, &payload, &processed); err != nil {
			return err
		}
		if processed != nil {
			return nil
		}
		var err error
		switch typ {
		case "stk_result":
			err = s.processSTK(ctx, tx, payload)
		case "c2b_confirmation":
			err = s.processC2B(ctx, tx, payload)
		case "b2c_result":
			err = s.processResult(ctx, tx, payload, false)
		case "b2c_timeout":
			err = s.processResult(ctx, tx, payload, true)
		case "reversal_result":
			err = s.processReversal(ctx, tx, payload)
		}
		if err != nil {
			return &callbackError{err}
		}
		_, err = tx.Exec(ctx, `UPDATE mpesa_callbacks SET processed_at = now(), process_error = NULL WHERE id = $1`, id)
		return err
	})
	var ce *callbackError
	if errors.As(err, &ce) {
		// A callback nothing matches will never match later, so it is closed and left for reconciliation.
		_, _ = s.DB.Exec(ctx, `UPDATE mpesa_callbacks SET process_error = $2, processed_at = CASE WHEN $3 THEN now() END WHERE id = $1`, id, ce.err.Error(), errors.Is(ce.err, errUnmatched))
		return ce.err
	}
	return err
}

type callbackError struct{ err error }

func (e *callbackError) Error() string { return e.err.Error() }

var errUnmatched = errors.New("no matching record")

func (s *Server) processSTK(ctx context.Context, tx pgx.Tx, payload []byte) error {
	var p stkCallback
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	cb := p.Body.StkCallback
	var paymentID, clientID uuid.UUID
	var status string
	if err := tx.QueryRow(ctx, `SELECT id, client_id, status::text FROM payments WHERE mpesa_checkout_request_id = $1 FOR UPDATE`, cb.CheckoutRequestID).Scan(&paymentID, &clientID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: checkout %s", errUnmatched, cb.CheckoutRequestID)
		}
		return err
	}
	if status == "succeeded" || status == "reversed" {
		return nil
	}
	if cb.ResultCode != 0 {
		next := "failed"
		if cb.ResultCode == 1032 { // customer cancelled the prompt
			next = "cancelled"
		}
		if _, err := tx.Exec(ctx, `UPDATE payments SET status = $2::payment_status, failure_reason = $3, completed_at = now() WHERE id = $1`, paymentID, next, cb.ResultDesc); err != nil {
			return err
		}
		return notify(ctx, tx, clientID, "payment.failed", "Payment not completed", cb.ResultDesc, map[string]any{"payment_id": paymentID})
	}
	var receipt, msisdn *string
	var amount *int64
	for _, it := range cb.CallbackMetadata.Item {
		switch it.Name {
		case "MpesaReceiptNumber":
			v := jsonString(it.Value)
			receipt = &v
		case "Amount":
			var f float64
			if json.Unmarshal(it.Value, &f) == nil {
				a := int64(math.Round(f * 100))
				amount = &a
			}
		case "PhoneNumber":
			v := "+" + jsonString(it.Value)
			msisdn = &v
		}
	}
	return s.succeedPayment(ctx, tx, paymentID, receipt, amount, msisdn)
}

// processC2B books a paybill payment against the invoice named in the account number.
func (s *Server) processC2B(ctx context.Context, tx pgx.Tx, payload []byte) error {
	var p c2bPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	if p.TransID == "" {
		return errors.New("callback has no TransID")
	}
	var dup int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM payments WHERE mpesa_receipt_number = $1`, p.TransID).Scan(&dup); err != nil {
		return err
	}
	if dup > 0 {
		return nil
	}
	var invoiceID, clientID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id, client_id FROM invoices WHERE upper(reference) = upper($1) AND status IN ('issued','partially_paid') FOR UPDATE`, strings.TrimSpace(p.BillRefNumber)).
		Scan(&invoiceID, &clientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: invoice %q is unknown or already settled; refund or record it manually", errUnmatched, p.BillRefNumber)
		}
		return err
	}
	amount := int64(math.Round(jsonFloat(p.TransAmount) * 100))
	if amount <= 0 {
		return errors.New("callback amount is not positive")
	}
	var msisdn *string
	if p.MSISDN != "" && !strings.Contains(p.MSISDN, "*") { // Safaricom now sends a hash; keep only a real number
		m := "+" + strings.TrimPrefix(p.MSISDN, "+")
		msisdn = &m
	}
	var pid uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO payments (invoice_id, client_id, method, status, amount_minor, payer_msisdn, mpesa_receipt_number, completed_at)
		VALUES ($1,$2,'mpesa_paybill','succeeded',$3,$4,$5, now()) RETURNING id`, invoiceID, clientID, amount, msisdn, p.TransID).Scan(&pid); err != nil {
		return err
	}
	return s.applyPayment(ctx, tx, pid)
}

// processResult handles a B2C result (payout or refund) or its queue timeout.
func (s *Server) processResult(ctx context.Context, tx pgx.Tx, payload []byte, timeout bool) error {
	var p resultPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	res := p.Result
	// Retries send "<payout id>.<attempt>"; the id is the part before the dot.
	origin, _, _ := strings.Cut(res.OriginatorConversationID, ".")
	originID, _ := uuid.Parse(origin)
	success := !timeout && res.ResultCode == 0
	reason := res.ResultDesc
	if timeout {
		reason = "M-Pesa timed out before answering. Check the statement before retrying."
	}

	var payoutID, providerID uuid.UUID
	var amount int64
	err := tx.QueryRow(ctx, `SELECT id, provider_id, amount_minor FROM payouts WHERE mpesa_conversation_id = $1 OR id = $2 FOR UPDATE`, res.ConversationID, originID).Scan(&payoutID, &providerID, &amount)
	if err == nil {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM payouts WHERE id = $1`, payoutID).Scan(&status); err != nil {
			return err
		}
		if status == "paid" {
			return nil
		}
		if !success {
			if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'failed', failure_reason = $2 WHERE id = $1`, payoutID, reason); err != nil {
				return err
			}
			return s.rollUpBatch(ctx, tx, payoutID)
		}
		var receipt *string
		if res.TransactionID != "" {
			receipt = &res.TransactionID
		}
		if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'paid', paid_at = now(), mpesa_receipt_number = $2, failure_reason = NULL WHERE id = $1`, payoutID, receipt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE provider_earnings SET status = 'paid' WHERE payout_id = $1 AND status = 'in_payout'`, payoutID); err != nil {
			return err
		}
		if err := postJournal(ctx, tx, "payout:"+payoutID.String(), ledgerRef{provider: &providerID, payout: &payoutID},
			ledgerLine{"provider_payable", amount}, ledgerLine{"mpesa_clearing", -amount}); err != nil {
			return err
		}
		if err := notify(ctx, tx, providerID, "payout.paid", "Payout sent", "We sent "+fmtMoney(amount)+" to your M-Pesa.", map[string]any{"payout_id": payoutID}); err != nil {
			return err
		}
		return s.rollUpBatch(ctx, tx, payoutID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	var refundID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM refunds WHERE mpesa_conversation_id = $1 OR id = $2 FOR UPDATE`, res.ConversationID, originID).Scan(&refundID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: conversation %s", errUnmatched, res.ConversationID)
		}
		return err
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status::text FROM refunds WHERE id = $1`, refundID).Scan(&status); err != nil {
		return err
	}
	if status == "completed" {
		return nil
	}
	if !success {
		_, err := tx.Exec(ctx, `UPDATE refunds SET status = 'failed' WHERE id = $1`, refundID)
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE refunds SET status = 'completed', completed_at = now() WHERE id = $1`, refundID); err != nil {
		return err
	}
	return s.bookRefund(ctx, tx, refundID)
}

func (s *Server) processReversal(ctx context.Context, tx pgx.Tx, payload []byte) error {
	var p resultPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	if p.Result.ResultCode != 0 {
		return nil
	}
	for _, rp := range p.Result.ResultParameters.ResultParameter {
		if rp.Key == "OriginalTransactionID" || rp.Key == "TransactionID" {
			tag, err := tx.Exec(ctx, `UPDATE payments SET status = 'reversed' WHERE mpesa_receipt_number = $1 AND status = 'succeeded'`, jsonString(rp.Value))
			if err != nil {
				return err
			}
			if tag.RowsAffected() > 0 {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: reversal does not match a payment", errUnmatched)
}

// ReprocessCallbacks retries callbacks that failed or were stored but not processed (a crash between
// storing and processing). It stands in for the SQS consumer until the queue is wired.
func (s *Server) ReprocessCallbacks(ctx context.Context) {
	rows, err := s.DB.Query(ctx, `SELECT id FROM mpesa_callbacks WHERE processed_at IS NULL AND callback_type <> 'c2b_validation' AND received_at < now() - interval '30 seconds' ORDER BY received_at LIMIT 50`)
	if err != nil {
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := s.processCallback(ctx, id); err != nil && !errors.Is(err, errUnmatched) {
			slog.Warn("reprocess callback", "callback", id, "err", err)
		}
	}
}

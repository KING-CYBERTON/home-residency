package api_test

import (
	"context"
	"testing"
	"time"
)

func stkCallback(checkout string, code int, amountKES int, receipt string) map[string]any {
	cb := map[string]any{"MerchantRequestID": "m1", "CheckoutRequestID": checkout, "ResultCode": code, "ResultDesc": "The service request is processed successfully."}
	if code != 0 {
		cb["ResultDesc"] = "Request cancelled by user"
	} else {
		cb["CallbackMetadata"] = map[string]any{"Item": []any{
			map[string]any{"Name": "Amount", "Value": amountKES},
			map[string]any{"Name": "MpesaReceiptNumber", "Value": receipt},
			map[string]any{"Name": "PhoneNumber", "Value": 254712345678},
		}}
	}
	return map[string]any{"Body": map[string]any{"stkCallback": cb}}
}

func TestPaymentsFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	w := h.newWorld()
	h.staff("triage", "triage@homend.test", "triage_officer")
	h.staff("fin1", "fin1@homend.test", "finance")
	h.staff("fin2", "fin2@homend.test", "finance")
	reqID, _ := h.runJob(w, "triage")
	invID := h.expect(200, "client", "GET", "/client/invoices", nil)["data"].([]any)[0].(map[string]any)["id"].(string)

	// STK push: bad phone, then a real prompt; a second prompt straight away is refused
	h.expect(422, "client", "POST", "/client/invoices/"+invID+"/pay", map[string]any{"phone": "0712345678"})
	pay := h.expect(202, "client", "POST", "/client/invoices/"+invID+"/pay", map[string]any{"phone": "+254712345678"}, "Idempotency-Key", "pay-1")
	payID := pay["id"].(string)
	if pay["status"] != "pending" || pay["amount_minor"] != 250000.0 {
		t.Fatalf("payment: %v", pay)
	}
	if again := h.expect(202, "client", "POST", "/client/invoices/"+invID+"/pay", map[string]any{"phone": "+254712345678"}, "Idempotency-Key", "pay-1"); again["id"] != payID {
		t.Fatal("idempotent replay created another payment")
	}
	h.expect(409, "client", "POST", "/client/invoices/"+invID+"/pay", map[string]any{"phone": "+254712345678"})
	if len(h.mpesa.STKs) != 1 || h.mpesa.STKs[0].AmountKES != 2500 || h.mpesa.STKs[0].MSISDN != "254712345678" {
		t.Fatalf("stk push: %+v", h.mpesa.STKs)
	}
	var checkout string
	if err := h.pool.QueryRow(ctx, `SELECT mpesa_checkout_request_id FROM payments WHERE id = $1`, payID).Scan(&checkout); err != nil {
		t.Fatal(err)
	}

	// the callback must carry the shared secret once one is configured
	h.app.Cfg.WebhookToken = "s3cret"
	h.expect(403, "", "POST", "/webhooks/mpesa/stk", stkCallback(checkout, 0, 2500, "QJK3ABC123"))
	h.app.Cfg.WebhookToken = ""

	// customer cancels the first prompt, then pays on the second
	h.expect(200, "", "POST", "/webhooks/mpesa/stk", stkCallback(checkout, 1032, 0, ""))
	if got := h.expect(200, "client", "GET", "/client/payments/"+payID, nil); got["status"] != "cancelled" {
		t.Fatalf("cancelled prompt: %v", got)
	}
	pay2 := h.expect(202, "client", "POST", "/client/invoices/"+invID+"/pay", map[string]any{"phone": "+254712345678"})
	var checkout2 string
	_ = h.pool.QueryRow(ctx, `SELECT mpesa_checkout_request_id FROM payments WHERE id = $1`, pay2["id"]).Scan(&checkout2)
	h.expect(200, "", "POST", "/webhooks/mpesa/stk", stkCallback(checkout2, 0, 2500, "QJK3ABC123"))
	h.expect(200, "", "POST", "/webhooks/mpesa/stk", stkCallback(checkout2, 0, 2500, "QJK3ABC123")) // Safaricom retries
	if got := h.expect(200, "client", "GET", "/client/payments/"+pay2["id"].(string), nil); got["status"] != "succeeded" || got["mpesa_receipt_number"] != "QJK3ABC123" {
		t.Fatalf("paid: %v", got)
	}
	if got := h.expect(200, "client", "GET", "/client/invoices/"+invID, nil); got["status"] != "paid" || got["paid_minor"] != 250000.0 {
		t.Fatalf("invoice after payment: %v", got)
	}
	h.expect(409, "client", "POST", "/client/invoices/"+invID+"/pay", map[string]any{"phone": "+254712345678"})

	// paid but not rated yet: nothing is payable. After a 5-star rating the earning is released.
	var n int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM provider_earnings`).Scan(&n)
	if n != 0 {
		t.Fatal("earnings must not exist before the rating")
	}
	h.expect(201, "client", "POST", "/client/requests/"+reqID+"/rating", map[string]any{"stars": 5})
	var status string
	var net int64
	_ = h.pool.QueryRow(ctx, `SELECT status::text, net_minor FROM provider_earnings`).Scan(&status, &net)
	if status != "payable" || net != 200000 {
		t.Fatalf("earning: %s %d", status, net)
	}
	h.expectLedgerBalanced(ctx)

	// payout: prepared by one finance officer, approved by another, paid when Safaricom confirms
	today := time.Now().UTC().Format("2006-01-02")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	h.expect(422, "fin1", "POST", "/console/payout-batches", map[string]any{"cycle_start": tomorrow, "cycle_end": today})
	batch := h.expect(201, "fin1", "POST", "/console/payout-batches", map[string]any{"cycle_start": today, "cycle_end": tomorrow})
	batchID := batch["id"].(string)
	if batch["total_minor"] != 200000.0 || batch["payout_count"] != 1.0 {
		t.Fatalf("batch: %v", batch)
	}
	h.expect(409, "fin2", "POST", "/console/payout-batches", map[string]any{"cycle_start": today, "cycle_end": tomorrow})
	h.expect(422, "fin1", "POST", "/console/payout-batches/"+batchID+"/approve", nil)
	done := h.expect(200, "fin2", "POST", "/console/payout-batches/"+batchID+"/approve", nil)
	payout := done["payouts"].([]any)[0].(map[string]any)
	if done["status"] != "processing" || payout["status"] != "processing" || len(h.mpesa.B2Cs) != 1 || h.mpesa.B2Cs[0].AmountKES != 2000 {
		t.Fatalf("approve: %v %+v", done, h.mpesa.B2Cs)
	}
	var conv string
	_ = h.pool.QueryRow(ctx, `SELECT mpesa_conversation_id FROM payouts WHERE id = $1`, payout["id"]).Scan(&conv)
	result := map[string]any{"Result": map[string]any{"ResultCode": 0, "ResultDesc": "ok", "ConversationID": conv, "OriginatorConversationID": h.mpesa.B2Cs[0].OriginatorConv, "TransactionID": "NLJ7RT61SV"}}
	h.expect(200, "", "POST", "/webhooks/mpesa/b2c/result", result)
	h.expect(200, "", "POST", "/webhooks/mpesa/b2c/result", result)
	if got := h.expect(200, "fin1", "GET", "/console/payout-batches/"+batchID, nil); got["status"] != "paid" || got["payouts"].([]any)[0].(map[string]any)["mpesa_receipt_number"] != "NLJ7RT61SV" {
		t.Fatalf("batch after result: %v", got)
	}
	_ = h.pool.QueryRow(ctx, `SELECT status::text FROM provider_earnings`).Scan(&status)
	if status != "paid" {
		t.Fatalf("earning after payout: %s", status)
	}

	// partial refund, requested and approved by different people, sent back by B2C
	pays := h.expect(200, "fin1", "GET", "/console/payments?status=succeeded", nil)["data"].([]any)
	if len(pays) != 1 {
		t.Fatalf("payments: %v", pays)
	}
	succeeded := pays[0].(map[string]any)["id"].(string)
	h.expect(422, "fin1", "POST", "/console/refunds", map[string]any{"payment_id": succeeded, "amount_minor": 999999, "reason": "too much"})
	ref := h.expect(201, "fin1", "POST", "/console/refunds", map[string]any{"payment_id": succeeded, "amount_minor": 50000, "reason": "Service complaint upheld"})
	h.expect(422, "fin1", "POST", "/console/refunds/"+ref["id"].(string)+"/approve", nil)
	ap := h.expect(200, "fin2", "POST", "/console/refunds/"+ref["id"].(string)+"/approve", nil)
	if ap["status"] != "processing" || len(h.mpesa.B2Cs) != 2 {
		t.Fatalf("refund approve: %v", ap)
	}
	var rconv string
	_ = h.pool.QueryRow(ctx, `SELECT mpesa_conversation_id FROM refunds WHERE id = $1`, ref["id"]).Scan(&rconv)
	h.expect(200, "", "POST", "/webhooks/mpesa/b2c/result", map[string]any{"Result": map[string]any{"ResultCode": 0, "ConversationID": rconv, "OriginatorConversationID": ref["id"], "TransactionID": "RFD1"}})
	if got := h.expect(200, "fin1", "GET", "/console/refunds?status=completed", nil); len(got["data"].([]any)) != 1 {
		t.Fatalf("completed refunds: %v", got)
	}

	// paybill: the client pays a subscription invoice by account number
	h.sql(`INSERT INTO subscription_plans (code, name, price_minor, billing_period, is_active) VALUES ('basic','Basic',100000,'monthly',true)`)
	var planID string
	_ = h.pool.QueryRow(ctx, `SELECT id FROM subscription_plans WHERE code = 'basic'`).Scan(&planID)
	sub := h.expect(201, "client", "POST", "/client/subscription", map[string]any{"accept_terms": true, "plan_id": planID})
	if sub["status"] != "past_due" {
		t.Fatalf("new subscription: %v", sub)
	}
	var subInv, subRef string
	_ = h.pool.QueryRow(ctx, `SELECT id, reference FROM invoices WHERE subscription_id IS NOT NULL`).Scan(&subInv, &subRef)
	h.expect(200, "", "POST", "/webhooks/mpesa/c2b/validation", map[string]any{"TransID": "X1", "BillRefNumber": subRef, "TransAmount": "1160.00"})
	if rej := h.expect(200, "", "POST", "/webhooks/mpesa/c2b/validation", map[string]any{"TransID": "X2", "BillRefNumber": "INV-NOPE"}); rej["ResultCode"] == 0.0 {
		t.Fatal("unknown account number must be rejected")
	}
	conf := map[string]any{"TransID": "SBP1", "BillRefNumber": subRef, "TransAmount": "1160.00", "MSISDN": "254712345678"}
	h.expect(200, "", "POST", "/webhooks/mpesa/c2b/confirmation", conf)
	h.expect(200, "", "POST", "/webhooks/mpesa/c2b/confirmation", conf)
	if got := h.expect(200, "client", "GET", "/client/subscription", nil); got["status"] != "active" {
		t.Fatalf("subscription after payment: %v", got)
	}
	// an unmatched deposit is kept for reconciliation rather than lost
	h.expect(200, "", "POST", "/webhooks/mpesa/c2b/confirmation", map[string]any{"TransID": "ODD1", "BillRefNumber": "INV-GONE", "TransAmount": "10"})
	rec := h.expect(200, "fin1", "GET", "/console/reconciliation?from="+today+"&to="+tomorrow, nil)
	if len(rec["unmatched_mpesa"].([]any)) != 1 || rec["matched"] != 2.0 {
		t.Fatalf("reconciliation: %v", rec)
	}
	h.expectLedgerBalanced(ctx)
	bal := h.expectList(200, "fin1", "GET", "/console/ledger/balances")
	if len(bal) != 7 {
		t.Fatalf("balances: %v", bal)
	}

	h.expect(200, "client", "POST", "/client/subscription/cancel", nil)
}

func (h *harness) expectLedgerBalanced(ctx context.Context) {
	h.t.Helper()
	var sum int64
	var unbalanced int
	_ = h.pool.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor),0) FROM ledger_entries`).Scan(&sum)
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT journal_id FROM ledger_entries GROUP BY journal_id HAVING sum(amount_minor) <> 0) x`).Scan(&unbalanced)
	if sum != 0 || unbalanced != 0 {
		h.t.Fatalf("ledger out of balance: total %d, %d unbalanced journals", sum, unbalanced)
	}
}

// The QA case: a KES 1,158.84 price is invoiced as KES 1,159 with a rounding line, VAT is the part
// of that total it already includes, and the M-Pesa prompt asks for exactly the invoice total.
func TestInvoiceRoundsToWholeShillings(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	w := h.newWorld()
	h.sql(`UPDATE rate_card_entries SET client_price_minor = 115884, provider_pay_minor = 80000 WHERE service_id = $1`, w.svcID)
	h.staff("triage", "triage@homend.test", "triage_officer")
	h.runJob(w, "triage")
	inv := h.expect(200, "client", "GET", "/client/invoices", nil)["data"].([]any)[0].(map[string]any)
	invID := inv["id"].(string)
	if inv["total_minor"] != 115900.0 || inv["vat_minor"] != 15986.0 || inv["subtotal_minor"] != 99914.0 {
		t.Fatalf("invoice: %v", inv)
	}
	full := h.expect(200, "client", "GET", "/client/invoices/"+invID, nil)
	var sum float64
	var rounding map[string]any
	for _, l := range full["lines"].([]any) {
		l := l.(map[string]any)
		sum += l["amount_minor"].(float64)
		if l["kind"] == "rounding" {
			rounding = l
		}
	}
	if rounding == nil || rounding["amount_minor"] != 16.0 || sum != 115900.0 {
		t.Fatalf("lines should add up to the total with a 16-cent rounding line: %v", full["lines"])
	}

	pay := h.expect(202, "client", "POST", "/client/invoices/"+invID+"/pay", map[string]any{"phone": "+254712345678"})
	if len(h.mpesa.STKs) != 1 || h.mpesa.STKs[0].AmountKES != 1159 {
		t.Fatalf("stk push: %+v", h.mpesa.STKs)
	}
	var checkout string
	if err := h.pool.QueryRow(ctx, `SELECT mpesa_checkout_request_id FROM payments WHERE id = $1`, pay["id"]).Scan(&checkout); err != nil {
		t.Fatal(err)
	}
	h.expect(200, "", "POST", "/webhooks/mpesa/stk", stkCallback(checkout, 0, 1159, "QJK3RND001"))
	if got := h.expect(200, "client", "GET", "/client/invoices/"+invID, nil); got["status"] != "paid" || got["paid_minor"] != 115900.0 {
		t.Fatalf("invoice after payment: %v", got)
	}
	var vat int64
	_ = h.pool.QueryRow(ctx, `SELECT COALESCE(-sum(amount_minor),0) FROM ledger_entries WHERE account = 'vat_payable'`).Scan(&vat)
	if vat != 15986 {
		t.Fatalf("VAT payable: %d", vat)
	}
	h.expectLedgerBalanced(ctx)
}

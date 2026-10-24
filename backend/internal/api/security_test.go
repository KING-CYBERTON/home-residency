package api_test

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
)

func TestStaffMFAReplayAndLockout(t *testing.T) {
	h := newHarness(t)
	pw, secret, _, err := h.app.CreateStaff(context.Background(), "ops", "ops@homend.test", []string{"triage_officer"})
	if err != nil {
		t.Fatal(err)
	}
	login := func() {
		t.Helper()
		h.tokens["ops"] = h.expect(200, "", "POST", "/auth/staff/login", map[string]any{"email": "ops@homend.test", "password": pw})["token"].(string)
	}
	login()
	code, _ := totp.GenerateCode(secret, time.Now())
	h.expect(200, "ops", "POST", "/auth/staff/mfa", map[string]any{"code": code})

	// the same code cannot open a second session
	login()
	h.expect(401, "ops", "POST", "/auth/staff/mfa", map[string]any{"code": code})
	for i := 0; i < 3; i++ {
		h.expect(401, "ops", "POST", "/auth/staff/mfa", map[string]any{"code": "000000"})
	}
	// the fifth wrong code locks the account and signs it out
	h.expect(429, "ops", "POST", "/auth/staff/mfa", map[string]any{"code": "000000"})
	h.expect(401, "ops", "POST", "/auth/staff/mfa", map[string]any{"code": "000000"})
	login()
	next, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	h.expect(429, "ops", "POST", "/auth/staff/mfa", map[string]any{"code": next})
}

func TestSTKCallbackAmountMismatchIsNotBooked(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	w := h.newWorld()
	h.staff("triage", "triage@homend.test", "triage_officer")
	h.runJob(w, "triage")
	invID := h.expect(200, "client", "GET", "/client/invoices", nil)["data"].([]any)[0].(map[string]any)["id"].(string)
	pay := h.expect(202, "client", "POST", "/client/invoices/"+invID+"/pay", map[string]any{"phone": "+254712345678"})
	var checkout string
	if err := h.pool.QueryRow(ctx, `SELECT mpesa_checkout_request_id FROM payments WHERE id = $1`, pay["id"]).Scan(&checkout); err != nil {
		t.Fatal(err)
	}
	h.expect(200, "", "POST", "/webhooks/mpesa/stk", stkCallback(checkout, 0, 1, "QJK3LOW001"))
	if got := h.expect(200, "client", "GET", "/client/payments/"+pay["id"].(string), nil); got["status"] != "failed" || got["amount_minor"] != 250000.0 {
		t.Fatalf("mismatched callback was booked: %v", got)
	}
	if got := h.expect(200, "client", "GET", "/client/invoices/"+invID, nil); got["paid_minor"] != 0.0 {
		t.Fatalf("invoice credited: %v", got)
	}
	// M-Pesa still took the money, so finance sees it on the reconciliation to settle by hand.
	h.staff("finance", "finance@homend.test", "finance")
	rec := h.expect(200, "finance", "GET", "/console/reconciliation", nil)
	if um, _ := rec["unmatched_mpesa"].([]any); len(um) != 1 {
		t.Fatalf("refused callback missing from reconciliation: %v", rec["unmatched_mpesa"])
	}
}

func TestCrossOriginStateChangeIsRefused(t *testing.T) {
	h := newHarness(t)
	post := func(headers map[string]string) int {
		t.Helper()
		req, _ := http.NewRequest("POST", h.srv.URL+api.BaseURL+"/auth/logout", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "text/plain")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "https://evil.homendresidence.co.ke"}); code != http.StatusForbidden {
		t.Fatalf("sibling-subdomain form post: got %d, want 403", code)
	}
	// server-to-server callers send neither header and reach the handler (401: no session)
	if code := post(nil); code != http.StatusUnauthorized {
		t.Fatalf("no browser headers: got %d, want 401", code)
	}
}

func TestDoorCodeParallelGuessesAreCapped(t *testing.T) {
	h := newHarness(t)
	w := h.newWorld()
	h.staff("triage", "triage@homend.test", "triage_officer")
	_, _, visitID := h.accepted(w, "triage")
	real := h.expect(200, "client", "GET", "/client/visits/"+visitID+"/checkin-code", nil)["code"].(string)
	wrong := "0000"
	if real == wrong {
		wrong = "1111"
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.call("provider", "POST", "/provider/visits/"+visitID+"/check-in", map[string]any{"client_event_id": uuid.NewString(), "method": "code",
				"code": wrong, "device_recorded_at": time.Now().UTC().Format(time.RFC3339)})
		}()
	}
	wg.Wait()
	var attempts int
	if err := h.pool.QueryRow(context.Background(), `SELECT checkin_attempts FROM visits WHERE id = $1`, visitID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 5 {
		t.Fatalf("attempts after 12 parallel guesses: %d, want 5 (the cap)", attempts)
	}
}

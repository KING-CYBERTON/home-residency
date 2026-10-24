package api_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
)

func (h *harness) postForm(path string, form url.Values) (int, string) {
	h.t.Helper()
	resp, err := http.PostForm(h.srv.URL+api.BaseURL+path, form)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// offered books a job and sends it to the provider without the provider answering.
func (h *harness) offered(w *world, triage string, later ...time.Duration) (reqID, assignID string) {
	start := 30 * time.Minute
	if len(later) > 0 {
		start = later[0]
	}
	h.t.Helper()
	r := h.expect(201, "client", "POST", "/client/requests", map[string]any{"accept_terms": true, "service_id": w.svcID, "address_id": w.addrID,
		"requested_start": time.Now().Add(start).UTC().Format(time.RFC3339)})
	reqID = r["id"].(string)
	h.expect(200, triage, "POST", "/console/requests/"+reqID+"/claim", nil)
	a := h.expect(201, triage, "POST", "/console/requests/"+reqID+"/assignments", map[string]any{"provider_id": w.providerID, "selection_reason": "Closest"})
	return reqID, a["id"].(string)
}

func TestUSSDAndSMSChannels(t *testing.T) {
	h := newHarness(t)
	w := h.newWorld()
	h.staff("triage", "tr@example.com", "triage_officer")
	phone := "+254700000002"

	code, body := h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s1"}, "phoneNumber": {"+254799999999"}, "text": {""}})
	if code != 200 || !strings.HasPrefix(body, "END") {
		t.Fatalf("unknown number: %d %q", code, body)
	}
	if code, _ := h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s1"}}); code != 400 {
		t.Fatalf("missing phone: %d", code)
	}
	if _, body := h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s2"}, "phoneNumber": {phone}, "text": {""}}); !strings.HasPrefix(body, "CON") {
		t.Fatalf("menu: %q", body)
	}
	if _, body := h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s2"}, "phoneNumber": {phone}, "text": {"1"}}); !strings.Contains(body, "no open job offers") {
		t.Fatalf("no offers yet: %q", body)
	}

	// an offer is accepted over USSD
	reqID, _ := h.offered(w, "triage")
	_, body = h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s3"}, "phoneNumber": {phone}, "text": {"1"}})
	if !strings.HasPrefix(body, "CON Job offers") {
		t.Fatalf("offers: %q", body)
	}
	if _, body = h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s3"}, "phoneNumber": {phone}, "text": {"1*1"}}); !strings.Contains(body, "Accept") {
		t.Fatalf("offer: %q", body)
	}
	if _, body = h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s3"}, "phoneNumber": {phone}, "text": {"1*1*1"}}); !strings.HasPrefix(body, "END") {
		t.Fatalf("accept: %q", body)
	}
	if st := h.expect(200, "triage", "GET", "/console/requests/"+reqID, nil)["status"]; st != "confirmed" {
		t.Fatalf("request after USSD accept: %v", st)
	}

	// check in over USSD with the client's door code
	visits := h.expectList(200, "provider", "GET", "/provider/visits")
	visit := visits[0].(map[string]any)["id"].(string)
	h.sql(`UPDATE visits SET scheduled_start = now() - interval '5 minutes' WHERE id = $1`, visit)
	if _, body = h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s4"}, "phoneNumber": {phone}, "text": {"2"}}); !strings.HasPrefix(body, "CON Choose") {
		t.Fatalf("check-in menu: %q", body)
	}
	doorCode := h.expect(200, "client", "GET", "/client/visits/"+visit+"/checkin-code", nil)["code"].(string)
	if _, body = h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s4"}, "phoneNumber": {phone}, "text": {"2*1*" + doorCode}}); !strings.HasPrefix(body, "END") {
		t.Fatalf("check-in: %q", body)
	}
	var method string
	if err := h.pool.QueryRow(t.Context(), `SELECT checkin_method::text FROM visits WHERE id = $1`, uuid.MustParse(visit)).Scan(&method); err != nil || method != "ussd" {
		t.Fatalf("check-in method %q err %v", method, err)
	}

	// SMS replies answer offers too
	h.offered(w, "triage", 72*time.Hour)
	if code, _ := h.postForm("/webhooks/sms/inbound", url.Values{"from": {phone}, "text": {"MAYBE"}}); code != 204 {
		t.Fatalf("sms inbound: %d", code)
	}
	if last := h.sms.Last(); last.To != phone || !strings.Contains(last.Body, "Reply YES") {
		t.Fatalf("help reply: %+v", last)
	}
	h.postForm("/webhooks/sms/inbound", url.Values{"from": {phone}, "text": {"NO"}})
	if code, _ := h.postForm("/webhooks/sms/inbound", url.Values{"text": {"YES"}}); code != 400 {
		t.Fatalf("sms without sender: %d", code)
	}
	if code, _ := h.postForm("/webhooks/sms/delivery", url.Values{"id": {"x1"}, "status": {"Success"}}); code != 204 {
		t.Fatalf("delivery report: %d", code)
	}

	// WhatsApp verification handshake
	h.app.Cfg.WebhookToken = "tok"
	defer func() { h.app.Cfg.WebhookToken = "" }()
	if code, _ := h.postForm("/webhooks/ussd", url.Values{"sessionId": {"s9"}, "phoneNumber": {phone}}); code != 403 {
		t.Fatalf("webhook without the secret: %d", code)
	}
}

func TestMessagingNotificationsAndPages(t *testing.T) {
	h := newHarness(t)
	w := h.newWorld()
	h.staff("triage", "tr@example.com", "triage_officer")
	h.staff("admin", "ad@example.com", "admin")
	reqID, _ := h.offered(w, "triage")

	h.sql(`INSERT INTO notifications (user_id, event_key, channel, status, title, body, sent_at) VALUES ($1,'offer.new','in_app','delivered','New job','Please respond', now())`, w.providerID)
	n := h.expect(200, "provider", "GET", "/notifications", nil)["data"].([]any)
	if len(n) == 0 {
		t.Fatal("no notification for the offer")
	}
	first := n[0].(map[string]any)["id"].(string)
	h.expect(204, "provider", "POST", "/notifications/"+first+"/read", nil)
	h.expect(204, "provider", "POST", "/notifications/read-all", nil)
	h.expect(404, "client", "POST", "/notifications/"+first+"/read", nil)

	h.expect(204, "client", "POST", "/push-subscriptions", map[string]any{"endpoint": "https://push.example/abc", "keys": map[string]any{"p256dh": "k", "auth": "a"}})
	h.expect(204, "client", "DELETE", "/push-subscriptions", nil)
	h.expect(204, "client", "POST", "/me/consents", []any{map[string]any{"purpose": "marketing", "notice_version": "2026-10", "granted": true}})

	// contact details are masked between the parties, staff see everything
	c := h.expect(201, "client", "POST", "/conversations", map[string]any{"request_id": reqID, "subject": "Arrival", "message": "Call me on 0712 345 678 or a@b.co"})
	cid := c["id"].(string)
	msgs := h.expect(200, "triage", "GET", "/conversations/"+cid+"/messages", nil)["data"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: %v", msgs)
	}
	body := msgs[0].(map[string]any)["body"].(string)
	if strings.Contains(body, "345") || strings.Contains(body, "a@b.co") {
		t.Fatalf("contact details leaked: %q", body)
	}
	h.expect(201, "triage", "POST", "/conversations/"+cid+"/messages", map[string]any{"body": "Noted, the provider is on the way"})
	if l := h.expect(200, "client", "GET", "/conversations/"+cid+"/messages", nil)["data"].([]any); len(l) != 2 {
		t.Fatalf("client sees %d messages", len(l))
	}
	h.expect(404, "provider", "GET", "/conversations/"+cid+"/messages", nil)
	if l := h.expect(200, "triage", "GET", "/conversations", nil)["data"].([]any); len(l) != 1 {
		t.Fatalf("staff conversations: %v", l)
	}

	// public pages: published ones only
	h.expect(404, "", "GET", "/public/pages/terms", nil)
	h.expect(201, "admin", "POST", "/console/content-pages", map[string]any{"slug": "terms", "title": "Terms", "body_markdown": "Be kind", "locale": "en", "is_published": true})
	p := h.expect(200, "", "GET", "/public/pages/terms", nil)
	if p["title"] != "Terms" {
		t.Fatalf("page: %v", p)
	}
}

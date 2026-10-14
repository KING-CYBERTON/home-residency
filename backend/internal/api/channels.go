package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// Providers without smartphones answer offers and check in over USSD, SMS or WhatsApp. Each channel
// resolves the sender's phone number to a provider and then calls the same handlers as the web app, so
// every rule (eligibility, expiry, door code) applies identically.

func e164(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone != "" && !strings.HasPrefix(phone, "+") {
		return "+" + phone
	}
	return phone
}

// providerContext returns a context that acts as the provider who owns the phone number.
func (s *Server) providerContext(ctx context.Context, phone, channel string) (context.Context, uuid.UUID, bool) {
	var id uuid.UUID
	if err := s.DB.QueryRow(ctx, `SELECT u.id FROM users u JOIN providers p ON p.user_id = u.id WHERE u.phone_e164 = $1 AND u.kind = 'provider' AND u.status = 'active'`, e164(phone)).Scan(&id); err != nil {
		return ctx, uuid.Nil, false
	}
	ctx = auth.WithPrincipal(ctx, &auth.Principal{UserID: id, Kind: auth.KindProvider})
	return withChannel(ctx, channel), id, true
}

type liveOffer struct {
	ID    uuid.UUID
	Ref   string
	Name  string
	Start time.Time
	Pay   int64
}

func (s *Server) liveOffers(ctx context.Context, provider uuid.UUID) ([]liveOffer, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT a.id, r.reference, sv.name, r.requested_start, r.provider_pay_minor
		FROM assignments a JOIN service_requests r ON r.id = a.request_id JOIN services sv ON sv.id = r.service_id
		WHERE a.provider_id = $1 AND a.status = 'offered' AND a.offer_expires_at > now() ORDER BY a.offered_at LIMIT 5`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []liveOffer
	for rows.Next() {
		var o liveOffer
		if err := rows.Scan(&o.ID, &o.Ref, &o.Name, &o.Start, &o.Pay); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// say turns the outcome of an in-process handler call into one line a phone can show.
func say(code int, body json.RawMessage, ok string) string {
	if code >= 200 && code < 300 {
		return ok
	}
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return "Sorry, that did not work. Please try again or call us."
}

func (s *Server) answerOffer(ctx context.Context, o liveOffer, accept bool) string {
	var code int
	var body json.RawMessage
	if accept {
		code, body = callHandler(ctx, map[string]any{}, func(w http.ResponseWriter, r *http.Request) {
			s.PostProviderOffersAccept(w, r, gen.Id(o.ID), gen.PostProviderOffersAcceptParams{})
		})
		return say(code, body, "Accepted "+o.Ref+". Check the app for the address.")
	}
	code, body = callHandler(ctx, map[string]any{}, func(w http.ResponseWriter, r *http.Request) {
		s.PostProviderOffersDecline(w, r, gen.Id(o.ID))
	})
	return say(code, body, "Declined "+o.Ref+".")
}

// ---- USSD

func (s *Server) PostWebhooksUssd(w http.ResponseWriter, r *http.Request) {
	if !s.webhookAllowed(r) {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Body must be form-encoded"))
		return
	}
	session, phone, text := r.PostForm.Get("sessionId"), r.PostForm.Get("phoneNumber"), r.PostForm.Get("text")
	if session == "" || phone == "" {
		httpx.Fail(w, r, httpx.BadRequest("sessionId and phoneNumber are required"))
		return
	}
	reply := s.ussdStep(r.Context(), session, phone, text)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, reply)
}

func (s *Server) ussdStep(ctx context.Context, session, phone, text string) string {
	pctx, provider, ok := s.providerContext(ctx, phone, "ussd")
	if !ok {
		return "END This number is not registered as an active Homend Residence provider."
	}
	var parts []string
	if text != "" {
		parts = strings.Split(text, "*")
	}
	reply := s.ussdMenu(pctx, provider, parts)
	_, _ = s.DB.Exec(ctx, `INSERT INTO ussd_sessions (session_id, phone_e164, user_id, state, ended_at) VALUES ($1,$2,$3,$4,CASE WHEN $5 THEN now() END)
		ON CONFLICT (session_id) DO UPDATE SET last_input_at = now(), state = EXCLUDED.state, ended_at = EXCLUDED.ended_at`,
		session, e164(phone), provider, mustJSON(map[string]any{"path": parts}), strings.HasPrefix(reply, "END"))
	return reply
}

func pick(parts []string, i, max int) (int, bool) {
	if i >= len(parts) {
		return 0, false
	}
	n, err := strconv.Atoi(parts[i])
	return n - 1, err == nil && n >= 1 && n <= max
}

func (s *Server) ussdMenu(ctx context.Context, provider uuid.UUID, p []string) string {
	if len(p) == 0 {
		return "CON Homend Residence\n1. Job offers\n2. Check in\n3. Check out"
	}
	switch p[0] {
	case "1":
		offers, err := s.liveOffers(ctx, provider)
		if err != nil {
			return "END Sorry, something went wrong. Please try again."
		}
		if len(offers) == 0 {
			return "END You have no open job offers."
		}
		if len(p) == 1 {
			var b strings.Builder
			b.WriteString("CON Job offers")
			for i, o := range offers {
				fmt.Fprintf(&b, "\n%d. %s %s %s", i+1, o.Name, o.Start.Format("Mon 2 Jan 15:04"), fmtMoney(o.Pay))
			}
			return b.String()
		}
		i, ok := pick(p, 1, len(offers))
		if !ok {
			return "END Invalid choice."
		}
		if len(p) == 2 {
			return fmt.Sprintf("CON %s %s\n1. Accept\n2. Decline", offers[i].Ref, offers[i].Name)
		}
		switch p[2] {
		case "1":
			return "END " + s.answerOffer(ctx, offers[i], true)
		case "2":
			return "END " + s.answerOffer(ctx, offers[i], false)
		}
		return "END Invalid choice."
	case "2", "3":
		checkIn := p[0] == "2"
		status, window := "checked_in", "TRUE"
		if checkIn {
			status, window = "scheduled", "v.scheduled_start < now() + interval '4 hours' AND v.scheduled_end > now() - interval '12 hours'"
		}
		rows, err := s.DB.Query(ctx, `SELECT v.id, r.reference, sv.name FROM visits v JOIN service_requests r ON r.id = v.request_id JOIN services sv ON sv.id = r.service_id
			WHERE v.provider_id = $1 AND v.status = $2::visit_status AND `+window+` ORDER BY v.scheduled_start LIMIT 5`, provider, status)
		if err != nil {
			return "END Sorry, something went wrong. Please try again."
		}
		type visit struct {
			id        uuid.UUID
			ref, name string
		}
		var vs []visit
		for rows.Next() {
			var v visit
			if err := rows.Scan(&v.id, &v.ref, &v.name); err != nil {
				rows.Close()
				return "END Sorry, something went wrong. Please try again."
			}
			vs = append(vs, v)
		}
		rows.Close()
		if len(vs) == 0 {
			if checkIn {
				return "END You have no visit to check in to right now."
			}
			return "END You have no visit in progress."
		}
		if len(p) == 1 {
			var b strings.Builder
			b.WriteString("CON Choose the visit")
			for i, v := range vs {
				fmt.Fprintf(&b, "\n%d. %s %s", i+1, v.ref, v.name)
			}
			return b.String()
		}
		i, ok := pick(p, 1, len(vs))
		if !ok {
			return "END Invalid choice."
		}
		now := s.Now()
		if !checkIn {
			code, body := callHandler(ctx, map[string]any{"client_event_id": uuid.New(), "device_recorded_at": now}, func(w http.ResponseWriter, r *http.Request) {
				s.PostProviderVisitsCheckOut(w, r, gen.Id(vs[i].id))
			})
			return "END " + say(code, body, "Checked out of "+vs[i].ref+". Thank you.")
		}
		if len(p) == 2 {
			return "CON Enter the 4-digit door code your client gives you"
		}
		code, body := callHandler(ctx, map[string]any{"client_event_id": uuid.New(), "device_recorded_at": now, "method": "code", "code": p[2]}, func(w http.ResponseWriter, r *http.Request) {
			s.PostProviderVisitsCheckIn(w, r, gen.Id(vs[i].id))
		})
		return "END " + say(code, body, "Checked in to "+vs[i].ref+".")
	}
	return "END Invalid choice."
}

// ---- SMS and WhatsApp replies

// offerReply handles "YES", "NO" or "YES <reference>" from a provider's phone and returns what to say back.
func (s *Server) offerReply(ctx context.Context, phone, text, channel string) (string, bool) {
	pctx, provider, ok := s.providerContext(ctx, phone, channel)
	if !ok {
		return "", false
	}
	fields := strings.Fields(strings.ToUpper(strings.TrimSpace(text)))
	if len(fields) == 0 {
		return "", false
	}
	var accept bool
	switch fields[0] {
	case "YES", "Y", "ACCEPT":
		accept = true
	case "NO", "N", "DECLINE":
	default:
		return "Reply YES to accept or NO to decline a job offer. Add the booking reference if you have more than one offer.", true
	}
	offers, err := s.liveOffers(pctx, provider)
	if err != nil {
		return "Sorry, something went wrong. Please try again.", true
	}
	if len(offers) == 0 {
		return "You have no open job offers.", true
	}
	var chosen *liveOffer
	if len(fields) > 1 {
		for i := range offers {
			if strings.EqualFold(offers[i].Ref, fields[1]) {
				chosen = &offers[i]
			}
		}
		if chosen == nil {
			return "We could not find an open offer with reference " + fields[1] + ".", true
		}
	} else if len(offers) == 1 {
		chosen = &offers[0]
	} else {
		return "You have " + strconv.Itoa(len(offers)) + " open offers. Reply " + fields[0] + " and the booking reference, for example " + fields[0] + " " + offers[0].Ref + ".", true
	}
	return s.answerOffer(pctx, *chosen, accept), true
}

func (s *Server) PostWebhooksSmsInbound(w http.ResponseWriter, r *http.Request) {
	if !s.webhookAllowed(r) {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Body must be form-encoded"))
		return
	}
	from, text := r.PostForm.Get("from"), r.PostForm.Get("text")
	if from == "" {
		httpx.Fail(w, r, httpx.BadRequest("from is required"))
		return
	}
	if reply, ok := s.offerReply(r.Context(), from, text, "sms"); ok && reply != "" {
		_ = s.SMS.Send(r.Context(), e164(from), "Homend Residence: "+reply)
	}
	httpx.NoContent(w)
}

// deliveryState maps a gateway's wording to ours.
func deliveryState(status string) string {
	switch strings.ToLower(status) {
	case "success", "delivered", "deliveredtoterminal", "read":
		return "delivered"
	case "sent", "submitted", "buffered", "queued":
		return "sent"
	}
	return "failed"
}

func (s *Server) PostWebhooksSmsDelivery(w http.ResponseWriter, r *http.Request) {
	if !s.webhookAllowed(r) {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Body must be form-encoded"))
		return
	}
	id, status := r.PostForm.Get("id"), r.PostForm.Get("status")
	if id == "" || status == "" {
		httpx.Fail(w, r, httpx.BadRequest("id and status are required"))
		return
	}
	state := deliveryState(status)
	reason := r.PostForm.Get("failureReason")
	if _, err := s.DB.Exec(r.Context(), `UPDATE notifications SET status = $2::notification_status, error = CASE WHEN $2 = 'failed' THEN NULLIF($3,'') END
		WHERE provider_message_id = $1 AND status <> 'read'`, id, state, reason); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// GetWebhooksWhatsapp answers Meta's one-off verification handshake.
func (s *Server) GetWebhooksWhatsapp(w http.ResponseWriter, r *http.Request, params gen.GetWebhooksWhatsappParams) {
	tok := s.Cfg.WebhookToken
	if tok == "" || params.HubMode == nil || *params.HubMode != "subscribe" || params.HubVerifyToken == nil ||
		!crypt.Equal([]byte(*params.HubVerifyToken), []byte(tok)) || params.HubChallenge == nil {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, *params.HubChallenge)
}

type waPayload struct {
	Entry []struct {
		Changes []struct {
			Value struct {
				Statuses []struct {
					ID     string `json:"id"`
					Status string `json:"status"`
					Errors []struct {
						Title string `json:"title"`
					} `json:"errors"`
				} `json:"statuses"`
				Messages []struct {
					From string `json:"from"`
					Text struct {
						Body string `json:"body"`
					} `json:"text"`
				} `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

func (s *Server) PostWebhooksWhatsapp(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.readWebhook(w, r)
	if !ok {
		return
	}
	var p waPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Unexpected payload"))
		return
	}
	ctx := context.WithoutCancel(r.Context())
	for _, e := range p.Entry {
		for _, c := range e.Changes {
			for _, st := range c.Value.Statuses {
				reason := ""
				if len(st.Errors) > 0 {
					reason = st.Errors[0].Title
				}
				_, _ = s.DB.Exec(ctx, `UPDATE notifications SET status = $2::notification_status, error = CASE WHEN $2 = 'failed' THEN NULLIF($3,'') END
					WHERE provider_message_id = $1 AND status <> 'read'`, st.ID, deliveryState(st.Status), reason)
			}
			for _, m := range c.Value.Messages {
				// There is no WhatsApp sender yet, so the reply goes back by SMS.
				if reply, ok := s.offerReply(ctx, m.From, m.Text.Body, "whatsapp"); ok && reply != "" {
					_ = s.SMS.Send(ctx, e164(m.From), "Homend Residence: "+reply)
				}
			}
		}
	}
	httpx.NoContent(w)
}

package api

import (
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// ---- notifications

func (s *Server) GetNotifications(w http.ResponseWriter, r *http.Request, params gen.GetNotificationsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	b.add("n.user_id = ?", auth.MustPrincipal(r.Context()).UserID)
	b.raw("n.channel = 'in_app'")
	if params.Unread != nil && *params.Unread {
		b.raw("n.read_at IS NULL")
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT n.id, n.event_key, n.title, n.body, n.data, n.read_at, n.created_at, n.created_at AS _t, n.id AS _id
		FROM notifications n WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) PostNotificationsRead(w http.ResponseWriter, r *http.Request, id gen.Id) {
	tag, err := s.DB.Exec(r.Context(), `UPDATE notifications SET read_at = COALESCE(read_at, now()), status = 'read' WHERE id = $1 AND user_id = $2 AND channel = 'in_app'`,
		uuid.UUID(id), auth.MustPrincipal(r.Context()).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	httpx.NoContent(w)
}

func (s *Server) PostNotificationsReadAll(w http.ResponseWriter, r *http.Request) {
	if _, err := s.DB.Exec(r.Context(), `UPDATE notifications SET read_at = now(), status = 'read' WHERE user_id = $1 AND channel = 'in_app' AND read_at IS NULL`,
		auth.MustPrincipal(r.Context()).UserID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (s *Server) PostPushSubscriptions(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.PushSubscriptionInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !strings.HasPrefix(in.Endpoint, "https://") || in.Keys.P256dh == "" || in.Keys.Auth == "" {
		httpx.Fail(w, r, httpx.Invalid("endpoint must be an https URL and keys are required"))
		return
	}
	// The same browser may re-subscribe after a different sign-in; the endpoint belongs to whoever registered last.
	if _, err := s.DB.Exec(r.Context(), `INSERT INTO web_push_subscriptions (user_id, endpoint, p256dh, auth) VALUES ($1,$2,$3,$4)
		ON CONFLICT (endpoint) DO UPDATE SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth`,
		auth.MustPrincipal(r.Context()).UserID, in.Endpoint, in.Keys.P256dh, in.Keys.Auth); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	httpx.NoContent(w)
}

func (s *Server) DeletePushSubscriptions(w http.ResponseWriter, r *http.Request, params gen.DeletePushSubscriptionsParams) {
	me := auth.MustPrincipal(r.Context()).UserID
	var err error
	if params.Endpoint != nil && *params.Endpoint != "" {
		_, err = s.DB.Exec(r.Context(), `DELETE FROM web_push_subscriptions WHERE user_id = $1 AND endpoint = $2`, me, *params.Endpoint)
	} else {
		_, err = s.DB.Exec(r.Context(), `DELETE FROM web_push_subscriptions WHERE user_id = $1`, me)
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// ---- consents

func (s *Server) PostMeConsents(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[[]gen.ConsentInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(in) == 0 || len(in) > 10 {
		httpx.Fail(w, r, httpx.Invalid("Send between 1 and 10 consent decisions"))
		return
	}
	for _, c := range in {
		if strings.TrimSpace(c.NoticeVersion) == "" {
			httpx.Fail(w, r, httpx.Invalid("notice_version is required"))
			return
		}
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var ip any
	if p := net.ParseIP(clientIP(r)); p != nil {
		ip = p.String()
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		// Consent is a history, not a flag: every decision is a new row so withdrawal leaves a trace.
		for _, c := range in {
			if _, err := tx.Exec(ctx, `INSERT INTO consents (user_id, purpose, notice_version, granted, ip_address) VALUES ($1,$2,$3,$4,$5::inet)`,
				me, string(c.Purpose), strings.TrimSpace(c.NoticeVersion), c.Granted, ip); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "consent.record", "user", me, nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	httpx.NoContent(w)
}

// ---- public content

func (s *Server) GetPublicPagesById(w http.ResponseWriter, r *http.Request, slug string, params gen.GetPublicPagesByIdParams) {
	lang := "en"
	if params.Lang != nil {
		lang = string(*params.Lang)
	}
	// Fall back to English when the page has not been translated.
	raw, err := rawOne(r.Context(), s.DB, pageRes.sel+` WHERE t.slug = $1 AND t.is_published ORDER BY (t.language = $2) DESC, (t.language = 'en') DESC LIMIT 1`, slug, lang)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.JSON(w, http.StatusOK, raw)
}

// ---- messaging with management

var (
	phoneInText = regexp.MustCompile(`\+?\d[\d\s\-().]{7,}\d`)
	emailInText = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

// maskContacts hides phone numbers and emails in what a client or provider writes, so the two sides
// cannot take a booking off the platform (report §7.2).
func maskContacts(body string) string {
	body = emailInText.ReplaceAllString(body, "[email hidden]")
	return phoneInText.ReplaceAllString(body, "[phone hidden]")
}

func isStaff(r *http.Request) bool { return auth.MustPrincipal(r.Context()).Kind == auth.KindStaff }

const conversationCols = `c.id, c.participant_id, c.request_id, c.complaint_id, c.subject, c.closed_at, c.created_at,
	(SELECT max(m.created_at) FROM messages m WHERE m.conversation_id = c.id) AS last_message_at,
	(SELECT count(*) FROM messages m WHERE m.conversation_id = c.id AND m.read_at IS NULL AND
	   CASE WHEN $1::boolean THEN m.sender_id = c.participant_id ELSE m.sender_id <> c.participant_id END) AS unread_count`

func (s *Server) GetConversations(w http.ResponseWriter, r *http.Request, params gen.GetConversationsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	staff := isStaff(r)
	where := "c.participant_id = $2"
	args := []any{staff, auth.MustPrincipal(r.Context()).UserID}
	if staff {
		where = "($2::uuid IS NOT NULL)" // staff see every conversation
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT `+conversationCols+`,
		COALESCE((SELECT max(m.created_at) FROM messages m WHERE m.conversation_id = c.id), c.created_at) AS _t, c.id AS _id
		FROM conversations c WHERE `+where, args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) PostConversations(w http.ResponseWriter, r *http.Request) {
	if isStaff(r) {
		httpx.Fail(w, r, httpx.Forbidden("participants_only", "Conversations are started by clients and providers"))
		return
	}
	in, err := httpx.Decode[gen.ConversationCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	body := strings.TrimSpace(in.Message)
	if body == "" || len(body) > 2000 {
		httpx.Fail(w, r, httpx.Invalid("message must be 1 to 2000 characters"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if in.RequestId != nil {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM service_requests r WHERE r.id = $1 AND (r.client_id = $2 OR EXISTS
				(SELECT 1 FROM assignments a WHERE a.request_id = r.id AND a.provider_id = $2))`, uuid.UUID(*in.RequestId), me).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return httpx.Invalid("request_id is not one of your bookings")
			}
		}
		if in.ComplaintId != nil {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM complaints WHERE id = $1 AND (client_id = $2 OR provider_id = $2)`, uuid.UUID(*in.ComplaintId), me).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return httpx.Invalid("complaint_id is not one of your cases")
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversations (participant_id, request_id, complaint_id, subject) VALUES ($1,$2,$3,$4) RETURNING id`,
			me, uuidPtr(in.RequestId), uuidPtr(in.ComplaintId), trimPtr(in.Subject)).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO messages (conversation_id, sender_id, body, channel) VALUES ($1,$2,$3,$4::channel)`, id, me, maskContacts(body), channelOf(ctx))
		return err
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, `SELECT to_jsonb(x) FROM (SELECT `+conversationCols+` FROM conversations c WHERE c.id = $2) x`, false, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

// conversationAccess returns the participant and whether it is closed, or 404 when the caller may not see it.
func (s *Server) conversationAccess(r *http.Request, id uuid.UUID) (participant uuid.UUID, closed bool, err error) {
	var closedAt *string
	if err = s.DB.QueryRow(r.Context(), `SELECT participant_id, closed_at::text FROM conversations WHERE id = $1`, id).Scan(&participant, &closedAt); err != nil {
		return participant, false, mapErr(err)
	}
	if !isStaff(r) && participant != auth.MustPrincipal(r.Context()).UserID {
		return participant, false, httpx.ErrNotFound
	}
	return participant, closedAt != nil, nil
}

func (s *Server) GetConversationsMessages(w http.ResponseWriter, r *http.Request, id gen.Id, params gen.GetConversationsMessagesParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	participant, _, err := s.conversationAccess(r, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	pg, err := pagedRaw(ctx, s.DB, `SELECT m.id, m.conversation_id, m.sender_id, (m.sender_id <> $2 AND EXISTS (SELECT 1 FROM users u WHERE u.id = m.sender_id AND u.kind = 'staff')) AS from_management,
		m.body, m.read_at, m.created_at, m.created_at AS _t, m.id AS _id FROM messages m WHERE m.conversation_id = $1`, []any{uuid.UUID(id), participant}, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Reading a thread marks what the other side wrote as read.
	if isStaff(r) {
		_, _ = s.DB.Exec(ctx, `UPDATE messages SET read_at = now() WHERE conversation_id = $1 AND sender_id = $2 AND read_at IS NULL`, uuid.UUID(id), participant)
	} else {
		_, _ = s.DB.Exec(ctx, `UPDATE messages SET read_at = now() WHERE conversation_id = $1 AND sender_id <> $2 AND read_at IS NULL`, uuid.UUID(id), participant)
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) PostConversationsMessages(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.MessageCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" || len(body) > 2000 {
		httpx.Fail(w, r, httpx.Invalid("body must be 1 to 2000 characters"))
		return
	}
	participant, closed, err := s.conversationAccess(r, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if closed {
		httpx.Fail(w, r, httpx.Conflict("This conversation is closed"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	staff := isStaff(r)
	if !staff {
		body = maskContacts(body)
	}
	var mid uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		ch := channelOf(ctx)
		if staff {
			ch = "console"
		}
		if err := tx.QueryRow(ctx, `INSERT INTO messages (conversation_id, sender_id, body, channel) VALUES ($1,$2,$3,$4::channel) RETURNING id`, uuid.UUID(id), me, body, ch).Scan(&mid); err != nil {
			return err
		}
		if staff {
			return notify(ctx, tx, participant, "message.received", "New message from Homend Residence", body, map[string]any{"conversation_id": id})
		}
		return nil
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, `SELECT jsonb_build_object('id', m.id, 'conversation_id', m.conversation_id, 'sender_id', m.sender_id, 'from_management', $2::boolean,
		'body', m.body, 'read_at', m.read_at, 'created_at', m.created_at) FROM messages m WHERE m.id = $1`, mid, staff)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, raw)
}

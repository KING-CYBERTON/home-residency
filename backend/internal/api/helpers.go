package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// db is satisfied by *pgxpool.Pool and pgx.Tx so helpers work inside or outside a transaction.
type db interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// mapErr turns common database failures into API errors.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "23505": // unique_violation
			return httpx.Conflict("Already exists or conflicts with an existing record").With("constraint", pe.ConstraintName)
		case "23514", "23503", "22P02", "22007", "22008": // check, fk, bad text/date representation
			return httpx.Invalid("Value rejected by a data rule").With("constraint", pe.ConstraintName)
		case "23P01": // exclusion_violation: overlapping windows
			return httpx.Conflict("Overlaps an existing entry").With("constraint", pe.ConstraintName)
		case "P0001": // raise_exception, e.g. append-only tables
			return httpx.Conflict(pe.Message)
		}
	}
	return err
}

func (s *Server) setting(ctx context.Context, q db, key string, def float64) float64 {
	var raw json.RawMessage
	if err := q.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&raw); err != nil {
		return def
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		return def
	}
	return f
}

// audit appends to the audit log inside the caller's transaction.
func audit(ctx context.Context, q db, action, entityType string, entityID uuid.UUID, before, after any, reason string) error {
	var actor *uuid.UUID
	var kind *string
	if p := auth.FromContext(ctx); p != nil {
		actor = &p.UserID
		k := string(p.Kind)
		kind = &k
	}
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	nz := func(x []byte) any {
		if string(x) == "null" {
			return nil
		}
		return x
	}
	c := httpx.ClientOf(ctx)
	_, err := q.Exec(ctx, `
		INSERT INTO audit_log (actor_id, actor_kind, action, entity_type, entity_id, before_state, after_state, reason, request_id, ip_address, user_agent)
		VALUES ($1, $2::user_kind, $3, $4, $5, $6, $7, NULLIF($8,''), NULLIF($9,''), NULLIF($10,'')::inet, NULLIF($11,''))`,
		actor, kind, action, entityType, entityID, nz(b), nz(a), reason, httpx.RequestID(ctx), c.IP, c.UserAgent)
	return err
}

// emit writes a domain event to the outbox in the caller's transaction; a relay publishes it to SQS.
func emit(ctx context.Context, q db, eventType, aggregateType string, aggregateID uuid.UUID, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO outbox_events (event_type, aggregate_type, aggregate_id, payload) VALUES ($1,$2,$3,$4)`,
		eventType, aggregateType, aggregateID, b)
	return err
}

// ---- pagination: keyset cursors over (time, id), newest first.

type cursor struct {
	T  time.Time
	ID uuid.UUID
}

func encodeCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(s *string) (*cursor, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*s)
	if err != nil {
		return nil, httpx.BadRequest("Invalid cursor")
	}
	t, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, httpx.BadRequest("Invalid cursor")
	}
	ts, err1 := time.Parse(time.RFC3339Nano, t)
	u, err2 := uuid.Parse(id)
	if err1 != nil || err2 != nil {
		return nil, httpx.BadRequest("Invalid cursor")
	}
	return &cursor{ts, u}, nil
}

func pageLimit(l *int) int {
	if l == nil || *l < 1 {
		return 25
	}
	if *l > 100 {
		return 100
	}
	return *l
}

type page[T any] struct {
	Data []T      `json:"data"`
	Meta pageMeta `json:"meta"`
}

type pageMeta struct {
	NextCursor *string `json:"next_cursor"`
	Limit      int     `json:"limit"`
}

// paginate trims the extra row fetched to detect another page and builds the response.
func paginate[T any](rows []T, limit int, key func(T) (time.Time, uuid.UUID)) page[T] {
	p := page[T]{Data: rows, Meta: pageMeta{Limit: limit}}
	if p.Data == nil {
		p.Data = []T{}
	}
	if len(rows) > limit {
		p.Data = rows[:limit]
		t, id := key(rows[limit-1])
		c := encodeCursor(t, id)
		p.Meta.NextCursor = &c
	}
	return p
}

// TermsVersion names the terms of service published at /terms (web/public/templates/pages.html shows
// it too). Change both whenever the terms change, so each acceptance records the wording agreed to.
const TermsVersion = "2026-10"

// acceptTerms checks that the client ticked the terms on a booking or plan and records it, once per
// version, as a consent with the address it came from.
func acceptTerms(ctx context.Context, q db, r *http.Request, userID uuid.UUID, accepted bool) error {
	if !accepted {
		return httpx.Invalid("Accept the terms of service to continue").With("accept_terms", "required")
	}
	_, err := q.Exec(ctx, `
		INSERT INTO consents (user_id, purpose, notice_version, granted, ip_address)
		SELECT $1, 'terms_of_service', $2, true, NULLIF($3,'')::inet
		WHERE NOT EXISTS (SELECT 1 FROM consents WHERE user_id = $1 AND purpose = 'terms_of_service' AND notice_version = $2 AND granted)`,
		userID, TermsVersion, clientIP(r))
	return err
}

func clientIP(r *http.Request) string { return httpx.ClientIP(r) }

func fmtMoney(minor int64) string {
	return "KES " + strconv.FormatFloat(float64(minor)/100, 'f', 2, 64)
}

var _ = fmt.Sprintf

type channelKey struct{}

// withChannel records which channel a request arrived on (web, ussd, sms, whatsapp, console), so a
// reply typed into a phone is attributed to it rather than to the web app.
func withChannel(ctx context.Context, ch string) context.Context {
	return context.WithValue(ctx, channelKey{}, ch)
}

func channelOf(ctx context.Context) string {
	if ch, ok := ctx.Value(channelKey{}).(string); ok {
		return ch
	}
	return "web"
}

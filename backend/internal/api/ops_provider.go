package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// ---- jobs

func (s *Server) GetProviderJobs(w http.ResponseWriter, r *http.Request, params gen.GetProviderJobsParams) {
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	limit := pageLimit(params.Limit)
	var status *string
	if params.Status != nil {
		st := string(*params.Status)
		status = &st
	}
	var ct *time.Time
	var cid *uuid.UUID
	if cur != nil {
		ct, cid = &cur.T, &cur.ID
	}
	rows, err := s.DB.Query(ctx, `
		SELECT a.id, a.created_at FROM assignments a JOIN service_requests r ON r.id = a.request_id
		WHERE a.provider_id = $1 AND a.status IN ('accepted','completed') AND ($2::text IS NULL OR r.status::text = $2)
		  AND ($3::timestamptz IS NULL OR (a.created_at, a.id) < ($3, $4::uuid))
		ORDER BY a.created_at DESC, a.id DESC LIMIT $5`, pid, status, ct, cid, limit+1)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	type key struct {
		id uuid.UUID
		t  time.Time
	}
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.t); err != nil {
			rows.Close()
			httpx.Fail(w, r, err)
			return
		}
		keys = append(keys, k)
	}
	rows.Close()
	pg := paginate(keys, limit, func(k key) (time.Time, uuid.UUID) { return k.t, k.id })
	out := page[jobDTO]{Data: make([]jobDTO, 0, len(pg.Data)), Meta: pg.Meta}
	for _, k := range pg.Data {
		j, err := s.loadJob(ctx, s.DB, k.id, pid)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out.Data = append(out.Data, j)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) GetProviderJobsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	var status string
	if err := s.DB.QueryRow(ctx, `SELECT status::text FROM assignments WHERE id = $1 AND provider_id = $2`, uuid.UUID(id), pid).Scan(&status); err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if status != "accepted" && status != "completed" {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	j, err := s.loadJob(ctx, s.DB, uuid.UUID(id), pid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, j)
}

// ---- offline sync

type memWriter struct {
	h    http.Header
	code int
	buf  bytes.Buffer
}

func (m *memWriter) Header() http.Header         { return m.h }
func (m *memWriter) WriteHeader(c int)           { m.code = c }
func (m *memWriter) Write(b []byte) (int, error) { return m.buf.Write(b) }

// callHandler runs a handler in-process with a JSON body as the signed-in user, so a synced event
// goes through exactly the checks of the online endpoint.
func callHandler(ctx context.Context, body any, h func(http.ResponseWriter, *http.Request)) (int, json.RawMessage) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	mw := &memWriter{h: http.Header{}, code: http.StatusOK}
	h(mw, req)
	return mw.code, mw.buf.Bytes()
}

func (s *Server) PostProviderSync(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.SyncBatch](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(in.Events) == 0 || len(in.Events) > 200 {
		httpx.Fail(w, r, httpx.Invalid("Send between 1 and 200 events"))
		return
	}
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	events := append([]gen.SyncEvent(nil), in.Events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].DeviceRecordedAt.Before(events[j].DeviceRecordedAt) })

	results := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		res := map[string]any{"client_event_id": ev.ClientEventId}
		var seen int
		if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM visit_events WHERE client_event_id = $1`, uuid.UUID(ev.ClientEventId)).Scan(&seen); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if seen > 0 {
			res["status"] = "duplicate"
			results = append(results, res)
			continue
		}
		payload := map[string]any{}
		if ev.Payload != nil {
			for k, v := range *ev.Payload {
				payload[k] = v
			}
		}
		payload["client_event_id"] = ev.ClientEventId
		payload["device_recorded_at"] = ev.DeviceRecordedAt
		var code int
		var raw json.RawMessage
		switch ev.EventType {
		case gen.SyncEventEventTypeCheckIn:
			code, raw = callHandler(ctx, payload, func(w http.ResponseWriter, rq *http.Request) {
				s.PostProviderVisitsCheckIn(w, rq, gen.Id(ev.VisitId))
			})
		case gen.SyncEventEventTypeCheckOut:
			code, raw = callHandler(ctx, payload, func(w http.ResponseWriter, rq *http.Request) {
				s.PostProviderVisitsCheckOut(w, rq, gen.Id(ev.VisitId))
			})
		case gen.SyncEventEventTypeNote:
			code, raw = s.syncNote(ctx, pid, ev)
		default:
			code, raw = http.StatusUnprocessableEntity, json.RawMessage(`{"code":"validation_failed","message":"Unknown event_type"}`)
		}
		if code >= 200 && code < 300 {
			res["status"] = "applied"
		} else {
			res["status"] = "rejected"
			var e map[string]any
			if json.Unmarshal(raw, &e) == nil {
				res["error"] = e
			}
		}
		results = append(results, res)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) syncNote(ctx context.Context, pid uuid.UUID, ev gen.SyncEvent) (int, json.RawMessage) {
	note := ""
	if ev.Payload != nil {
		if n, ok := (*ev.Payload)["notes"].(string); ok {
			note = n
		}
	}
	fail := func(code int, c, m string) (int, json.RawMessage) {
		b, _ := json.Marshal(map[string]string{"code": c, "message": m})
		return code, b
	}
	if len(note) == 0 || len(note) > 2000 {
		return fail(http.StatusUnprocessableEntity, "validation_failed", "A note needs 1 to 2000 characters")
	}
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE visits SET provider_notes = $3 WHERE id = $1 AND provider_id = $2`, uuid.UUID(ev.VisitId), pid, note)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		_, err = tx.Exec(ctx, `INSERT INTO visit_events (visit_id, client_event_id, event_type, payload, device_recorded_at, channel) VALUES ($1,$2,'note',$3,$4,'web')`,
			uuid.UUID(ev.VisitId), uuid.UUID(ev.ClientEventId), mustJSON(map[string]string{"notes": note}), ev.DeviceRecordedAt)
		return err
	})
	if err != nil {
		if he, ok := mapErr(err).(*httpx.Error); ok {
			return fail(he.Status, he.Code, he.Message)
		}
		return fail(http.StatusInternalServerError, "internal_error", "Could not apply the event")
	}
	return http.StatusOK, nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ---- earnings and payouts

func (s *Server) GetProviderEarnings(w http.ResponseWriter, r *http.Request, params gen.GetProviderEarningsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	b.add("e.provider_id = ?", auth.MustPrincipal(r.Context()).UserID)
	if params.Status != nil {
		b.add("e.status = ?::earning_status", string(*params.Status))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT e.id, r.reference AS request_reference, e.visit_id, e.gross_minor, e.commission_minor, e.withholding_minor, e.net_minor,
		e.status, e.hold_reason, e.payout_id, e.earned_at, e.earned_at AS _t, e.id AS _id
		FROM provider_earnings e LEFT JOIN service_requests r ON r.id = e.request_id WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) GetProviderEarningsSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	now := s.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := map[string]any{}
	var pending, payable, held, paid int64
	if err := s.DB.QueryRow(ctx, `
		SELECT COALESCE(sum(net_minor) FILTER (WHERE status = 'pending'), 0),
		       COALESCE(sum(net_minor) FILTER (WHERE status IN ('payable','in_payout')), 0),
		       COALESCE(sum(net_minor) FILTER (WHERE status = 'held'), 0)
		FROM provider_earnings WHERE provider_id = $1`, pid).Scan(&pending, &payable, &held); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := s.DB.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor), 0) FROM payouts WHERE provider_id = $1 AND status = 'paid' AND paid_at >= $2`, pid, monthStart).Scan(&paid); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out["pending_minor"], out["payable_minor"], out["held_minor"], out["paid_this_month_minor"] = pending, payable, held, paid
	var cycle struct {
		Frequency string `json:"frequency"`
		Weekday   int    `json:"weekday"`
	}
	var raw json.RawMessage
	if s.DB.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = 'payout.cycle'`).Scan(&raw) == nil && json.Unmarshal(raw, &cycle) == nil && cycle.Frequency == "weekly" {
		d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		for int(d.Weekday()) != cycle.Weekday {
			d = d.AddDate(0, 0, 1)
		}
		out["next_payout_date"] = d.Format("2006-01-02")
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) GetProviderPayouts(w http.ResponseWriter, r *http.Request, params gen.GetProviderPayoutsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT p.id, p.batch_id, p.provider_id, p.amount_minor, p.msisdn, p.status, p.mpesa_receipt_number, p.failure_reason, p.paid_at,
		p.created_at AS _t, p.id AS _id FROM payouts p WHERE p.provider_id = $1 AND p.status <> 'draft'`, []any{auth.MustPrincipal(r.Context()).UserID}, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

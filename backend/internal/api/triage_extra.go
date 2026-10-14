package api

import (
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
)

// PostConsoleRequestsShareShortlist shows chosen shortlist entries to a client who asked to choose.
func (s *Server) PostConsoleRequestsShareShortlist(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[struct {
		EntryIDs []uuid.UUID `json:"entry_ids"`
	}](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(in.EntryIDs) < 2 || len(in.EntryIDs) > 5 {
		httpx.Fail(w, r, httpx.Invalid("Share between 2 and 5 candidates"))
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status, ref string
		var client uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, client_id, reference FROM service_requests WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status, &client, &ref); err != nil {
			return err
		}
		if status != "triaging" && status != "awaiting_client_choice" {
			return httpx.Conflict("Claim the request before sharing a shortlist (it is " + status + ")")
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM shortlist_entries WHERE request_id = $1 AND id = ANY($2)`, uuid.UUID(id), in.EntryIDs).Scan(&n); err != nil {
			return err
		}
		if n != len(in.EntryIDs) {
			return httpx.Invalid("Every entry must belong to this request's shortlist, without repeats")
		}
		if _, err := tx.Exec(ctx, `UPDATE shortlist_entries SET shown_to_client = (id = ANY($2)), client_selected = false WHERE request_id = $1`, uuid.UUID(id), in.EntryIDs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'awaiting_client_choice', offer_client_choice = true WHERE id = $1`, uuid.UUID(id)); err != nil {
			return err
		}
		if err := notify(ctx, tx, client, "shortlist.shared", "Choose your provider for "+ref, "We have a shortlist ready for you to choose from.", map[string]any{"request_id": id}); err != nil {
			return err
		}
		return audit(ctx, tx, "request.share_shortlist", "service_request", uuid.UUID(id), nil, in, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	d, err := s.loadRequestStaff(ctx, s.DB, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Server) PostConsoleRequestsCancel(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[struct {
		Reason      string `json:"reason"`
		CancelledBy string `json:"cancelled_by"`
		WaiveFee    bool   `json:"waive_fee"`
	}](r)
	if err != nil || len(strings.TrimSpace(in.Reason)) < 3 {
		httpx.Fail(w, r, httpx.Invalid("reason is required"))
		return
	}
	if in.CancelledBy != "client" && in.CancelledBy != "provider" && in.CancelledBy != "management" {
		httpx.Fail(w, r, httpx.Invalid("cancelled_by must be client, provider or management"))
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var client uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, client_id FROM service_requests WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status, &client); err != nil {
			return err
		}
		switch status {
		case "awaiting_rating", "closed", "cancelled":
			return httpx.Conflict("A request that is " + status + " cannot be cancelled")
		}
		if err := cancelRequest(ctx, tx, uuid.UUID(id), in.CancelledBy, strings.TrimSpace(in.Reason)); err != nil {
			return err
		}
		if err := notify(ctx, tx, client, "request.cancelled", "Your booking was cancelled", in.Reason, map[string]any{"request_id": id}); err != nil {
			return err
		}
		if err := audit(ctx, tx, "request.cancel", "service_request", uuid.UUID(id), map[string]any{"status": status},
			map[string]any{"status": "cancelled", "by": in.CancelledBy, "waive_fee": in.WaiveFee}, in.Reason); err != nil {
			return err
		}
		return emit(ctx, tx, "request.cancelled", "service_request", uuid.UUID(id), map[string]any{"by": in.CancelledBy})
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	d, err := s.loadRequestStaff(ctx, s.DB, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// ---- visits

// PostConsoleRequestsVisits adds visits to a confirmed job, for recurring and multi-day work.
func (s *Server) PostConsoleRequestsVisits(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.VisitSchedule](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(in.Visits) == 0 || len(in.Visits) > 60 {
		httpx.Fail(w, r, httpx.Invalid("Schedule between 1 and 60 visits"))
		return
	}
	for _, v := range in.Visits {
		if !v.ScheduledEnd.After(v.ScheduledStart) {
			httpx.Fail(w, r, httpx.Invalid("Each visit must end after it starts"))
			return
		}
	}
	ctx := r.Context()
	var ids []uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var assignment, provider uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT r.status::text, a.id, a.provider_id FROM service_requests r
			JOIN assignments a ON a.request_id = r.id AND a.status = 'accepted' WHERE r.id = $1 FOR UPDATE OF r`, uuid.UUID(id)).Scan(&status, &assignment, &provider); err != nil {
			if err == pgx.ErrNoRows {
				return httpx.Conflict("Only a job with an accepted provider can be scheduled")
			}
			return err
		}
		if status != "confirmed" && status != "in_progress" {
			return httpx.Conflict("A request that is " + status + " cannot take new visits")
		}
		for _, v := range in.Visits {
			var clash int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM visits WHERE provider_id = $1 AND status IN ('scheduled','checked_in')
				AND tstzrange(scheduled_start, scheduled_end) && tstzrange($2, $3)`, provider, v.ScheduledStart, v.ScheduledEnd).Scan(&clash); err != nil {
				return err
			}
			if clash > 0 {
				return httpx.Conflict(fmt.Sprintf("The provider already has a visit overlapping %s", v.ScheduledStart.Format(time.RFC3339)))
			}
			var vid uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO visits (request_id, assignment_id, provider_id, scheduled_start, scheduled_end) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
				uuid.UUID(id), assignment, provider, v.ScheduledStart, v.ScheduledEnd).Scan(&vid); err != nil {
				return err
			}
			ids = append(ids, vid)
		}
		return audit(ctx, tx, "visit.schedule", "service_request", uuid.UUID(id), nil, map[string]any{"visits": len(ids)}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	out := make([]visitDTO, 0, len(ids))
	for _, vid := range ids {
		v, err := s.loadVisit(ctx, s.DB, vid)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (s *Server) PatchConsoleVisitsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.VisitUpdate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.Status == nil && in.ScheduledStart == nil && in.ScheduledEnd == nil {
		httpx.Fail(w, r, httpx.Invalid("Nothing to change"))
		return
	}
	reason := ""
	if in.Reason != nil {
		reason = strings.TrimSpace(*in.Reason)
	}
	if in.Status != nil && len(reason) < 3 {
		httpx.Fail(w, r, httpx.Invalid("reason is required to mark a visit missed or cancelled"))
		return
	}
	if in.Status != nil && *in.Status != "missed" && *in.Status != "cancelled" {
		httpx.Fail(w, r, httpx.Invalid("status can only be missed or cancelled"))
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var reqID, provider uuid.UUID
		var start, end time.Time
		if err := tx.QueryRow(ctx, `SELECT status::text, request_id, provider_id, scheduled_start, scheduled_end FROM visits WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).
			Scan(&status, &reqID, &provider, &start, &end); err != nil {
			return err
		}
		if status != "scheduled" && !(in.Status != nil && status == "checked_in") {
			return httpx.Conflict("A visit that is " + status + " cannot be changed")
		}
		if in.ScheduledStart != nil {
			start = *in.ScheduledStart
		}
		if in.ScheduledEnd != nil {
			end = *in.ScheduledEnd
		}
		if !end.After(start) {
			return httpx.Invalid("The visit must end after it starts")
		}
		if in.ScheduledStart != nil || in.ScheduledEnd != nil {
			var clash int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM visits WHERE provider_id = $1 AND id <> $2 AND status IN ('scheduled','checked_in')
				AND tstzrange(scheduled_start, scheduled_end) && tstzrange($3, $4)`, provider, uuid.UUID(id), start, end).Scan(&clash); err != nil {
				return err
			}
			if clash > 0 {
				return httpx.Conflict("The provider already has a visit at that time")
			}
			// A new time needs a new door code: the old one was issued for the old slot.
			if _, err := tx.Exec(ctx, `UPDATE visits SET scheduled_start = $2, scheduled_end = $3, checkin_code_hash = NULL, checkin_attempts = 0 WHERE id = $1`, uuid.UUID(id), start, end); err != nil {
				return err
			}
		}
		if in.Status != nil {
			st := string(*in.Status)
			if _, err := tx.Exec(ctx, `UPDATE visits SET status = $2::visit_status WHERE id = $1`, uuid.UUID(id), st); err != nil {
				return err
			}
			if st == "missed" {
				if _, err := tx.Exec(ctx, `INSERT INTO job_exceptions (kind, request_id, visit_id, provider_id, details) VALUES ('no_show',$1,$2,$3,$4)`,
					reqID, uuid.UUID(id), provider, mustJSON(map[string]string{"reason": reason})); err != nil {
					return err
				}
			}
			var done int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM visits WHERE request_id = $1 AND status IN ('completed','client_confirmed')`, reqID).Scan(&done); err != nil {
				return err
			}
			if done > 0 {
				if err := s.completeIfDone(ctx, tx, reqID); err != nil {
					return err
				}
			}
		}
		return audit(ctx, tx, "visit.update", "visit", uuid.UUID(id), map[string]any{"status": status}, in, reason)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	v, err := s.loadVisit(ctx, s.DB, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

// PostConsoleVisitsManualCheckIn records an arrival for a provider who cannot use the app.
func (s *Server) PostConsoleVisitsManualCheckIn(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var reqID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, request_id FROM visits WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).Scan(&status, &reqID); err != nil {
			return err
		}
		if status != "scheduled" {
			return httpx.Conflict("This visit is " + status)
		}
		now := s.Now()
		if _, err := tx.Exec(ctx, `UPDATE visits SET status = 'checked_in', checked_in_at = $2, checkin_method = 'console', device_recorded_at = $2 WHERE id = $1`, uuid.UUID(id), now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'in_progress' WHERE id = $1 AND status = 'confirmed'`, reqID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO visit_events (visit_id, client_event_id, event_type, payload, device_recorded_at, channel) VALUES ($1,$2,'check_in','{}',$3,'console')`,
			uuid.UUID(id), uuid.New(), now); err != nil {
			return err
		}
		return audit(ctx, tx, "visit.manual_check_in", "visit", uuid.UUID(id), nil, map[string]any{"by": auth.MustPrincipal(ctx).UserID}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	v, err := s.loadVisit(ctx, s.DB, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, v)
}

// ---- exceptions

func (s *Server) GetConsoleExceptions(w http.ResponseWriter, r *http.Request, params gen.GetConsoleExceptionsParams) {
	cur, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	b := &qb{}
	if params.Kind != nil {
		b.add("x.kind = ?::exception_kind", string(*params.Kind))
	}
	if params.Open != nil {
		if *params.Open {
			b.raw("x.reviewed_at IS NULL")
		} else {
			b.raw("x.reviewed_at IS NOT NULL")
		}
	}
	if params.From != nil {
		b.add("x.detected_at >= ?", params.From.Time)
	}
	if params.To != nil {
		b.add("x.detected_at < ?", params.To.Time.AddDate(0, 0, 1))
	}
	pg, err := pagedRaw(r.Context(), s.DB, `SELECT x.id, x.kind, x.request_id, x.visit_id, x.provider_id, x.details, x.detected_at, x.reviewed_by, x.reviewed_at, x.resolution,
		x.detected_at AS _t, x.id AS _id FROM job_exceptions x WHERE `+b.where(), b.args, cur, pageLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pg)
}

func (s *Server) PostConsoleExceptionsResolve(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ExceptionResolve](r)
	if err != nil || len(strings.TrimSpace(in.Resolution)) < 3 {
		httpx.Fail(w, r, httpx.Invalid("resolution is required"))
		return
	}
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var kind string
		var reqID, visitID, providerID *uuid.UUID
		var reviewed *time.Time
		if err := tx.QueryRow(ctx, `SELECT kind::text, request_id, visit_id, provider_id, reviewed_at FROM job_exceptions WHERE id = $1 FOR UPDATE`, uuid.UUID(id)).
			Scan(&kind, &reqID, &visitID, &providerID, &reviewed); err != nil {
			return err
		}
		if reviewed != nil {
			return httpx.Conflict("This exception is already resolved")
		}
		if _, err := tx.Exec(ctx, `UPDATE job_exceptions SET reviewed_by = $2, reviewed_at = now(), resolution = $3 WHERE id = $1`, uuid.UUID(id), me, strings.TrimSpace(in.Resolution)); err != nil {
			return err
		}
		if in.OpenComplaint != nil && *in.OpenComplaint {
			if reqID == nil || providerID == nil {
				return httpx.Invalid("A complaint needs an exception that is tied to a job and a provider")
			}
			var client uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT client_id FROM service_requests WHERE id = $1`, *reqID).Scan(&client); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO complaints (source, severity, client_id, provider_id, request_id, visit_id, summary, raised_by)
				VALUES ('management','quality',$1,$2,$3,$4,$5,$6)`, client, *providerID, *reqID, visitID, "Opened from a "+strings.ReplaceAll(kind, "_", " ")+" exception: "+strings.TrimSpace(in.Resolution), me); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "exception.resolve", "job_exception", uuid.UUID(id), nil, in, in.Resolution)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	raw, err := rawOne(ctx, s.DB, exceptionJSON+` WHERE x.id = $1`, uuid.UUID(id))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, raw)
}

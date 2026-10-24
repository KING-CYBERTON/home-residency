package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/domain"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

func (s *Server) loadRequestStaff(ctx context.Context, q db, id uuid.UUID) (requestStaffDTO, error) {
	base, rr, err := s.loadRequest(ctx, q, id, nil)
	if err != nil {
		return requestStaffDTO{}, err
	}
	out := requestStaffDTO{requestDTO: base, ProviderPayMinor: rr.providerPay, Priority: rr.priority, TriageOfficerID: rr.triageOfficerID,
		Variations: []json.RawMessage{}, Exceptions: []json.RawMessage{}}
	if out.Variations, err = rawAll(ctx, q, variationJSON+` WHERE v.request_id = $1 ORDER BY v.created_at`, id); err != nil {
		return out, err
	}
	if out.Exceptions, err = rawAll(ctx, q, exceptionJSON+` WHERE x.request_id = $1 ORDER BY x.detected_at`, id); err != nil {
		return out, err
	}
	err = q.QueryRow(ctx, `SELECT u.id, u.full_name, u.phone_e164, u.status::text, c.id_verification::text, u.created_at
		FROM users u JOIN clients c ON c.user_id = u.id WHERE u.id = $1`, base.ClientID).
		Scan(&out.Client.ID, &out.Client.FullName, &out.Client.Phone, &out.Client.Status, &out.Client.IDVerification, &out.Client.CreatedAt)
	if err != nil {
		return out, err
	}
	_ = q.QueryRow(ctx, `SELECT triage_started_at FROM service_requests WHERE id = $1`, id).Scan(&out.TriageStartedAt)
	if out.Assignments, err = s.loadAssignments(ctx, q, id); err != nil {
		return out, err
	}
	return out, nil
}

const providerSummaryCols = `p.user_id, u.full_name, p.status::text, u.phone_e164, p.rating_avg::float8, p.appraisal_score::float8, p.completed_jobs, p.home_service_area_id`

func (s *Server) loadAssignments(ctx context.Context, q db, requestID uuid.UUID) ([]assignmentDTO, error) {
	rows, err := q.Query(ctx, `
		SELECT a.id, a.request_id, `+providerSummaryCols+`, a.status::text, a.selected_by, a.selection_reason, a.offered_at, a.offer_expires_at,
		       a.responded_at, a.response_channel::text, a.decline_reason
		FROM assignments a JOIN providers p ON p.user_id = a.provider_id JOIN users u ON u.id = p.user_id
		WHERE a.request_id = $1 ORDER BY a.offered_at`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []assignmentDTO{}
	for rows.Next() {
		var a assignmentDTO
		if err := rows.Scan(&a.ID, &a.RequestID, &a.Provider.ID, &a.Provider.FullName, &a.Provider.Status, &a.Provider.Phone, &a.Provider.RatingAvg,
			&a.Provider.AppraisalScore, &a.Provider.CompletedJobs, &a.Provider.HomeServiceAreaID, &a.Status, &a.SelectedBy, &a.SelectionReason,
			&a.OfferedAt, &a.OfferExpiresAt, &a.RespondedAt, &a.ResponseChannel, &a.DeclineReason); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Server) GetConsoleRequests(w http.ResponseWriter, r *http.Request, p gen.GetConsoleRequestsParams) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	cur, err := decodeCursor(p.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	limit := pageLimit(p.Limit)
	var status *string
	if p.Status != nil {
		st := string(*p.Status)
		status = &st
	}
	var officer *uuid.UUID
	if p.TriageOfficerId != nil {
		o := uuid.UUID(*p.TriageOfficerId)
		officer = &o
	}
	if p.Mine != nil && *p.Mine {
		officer = &me
	}
	var curT *time.Time
	var curID *uuid.UUID
	if cur != nil {
		curT, curID = &cur.T, &cur.ID
	}
	var from, to *time.Time
	if p.From != nil {
		from = &p.From.Time
	}
	if p.To != nil {
		to = &p.To.Time
	}
	var qtext *string
	if p.Q != nil && strings.TrimSpace(*p.Q) != "" {
		t := "%" + strings.TrimSpace(*p.Q) + "%"
		qtext = &t
	}
	// The queue is oldest-first: the request that has waited longest is served first.
	rows, err := s.DB.Query(ctx, `
		SELECT r.id, r.submitted_at FROM service_requests r
		WHERE ($1::text IS NULL OR r.status::text = $1) AND ($2::uuid IS NULL OR r.category_id = $2)
		  AND ($3::uuid IS NULL OR r.service_area_id = $3) AND ($4::uuid IS NULL OR r.triage_officer_id = $4)
		  AND ($5::date IS NULL OR r.submitted_at >= $5) AND ($6::date IS NULL OR r.submitted_at < $6::date + 1)
		  AND ($7::text IS NULL OR r.reference ILIKE $7)
		  AND ($8::timestamptz IS NULL OR (r.submitted_at, r.id) > ($8, $9::uuid))
		  AND ($11::uuid IS NULL OR r.client_id = $11)
		ORDER BY r.submitted_at, r.id LIMIT $10`,
		status, p.CategoryId, p.ServiceAreaId, officer, from, to, qtext, curT, curID, limit+1, p.ClientId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var t time.Time
		if err := rows.Scan(&id, &t); err != nil {
			rows.Close()
			httpx.Fail(w, r, err)
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	var dtos []requestStaffDTO
	for _, id := range ids {
		d, err := s.loadRequestStaff(ctx, s.DB, id)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		dtos = append(dtos, d)
	}
	httpx.JSON(w, http.StatusOK, paginate(dtos, limit, func(d requestStaffDTO) (time.Time, uuid.UUID) { return d.SubmittedAt, d.ID }))
}

func (s *Server) GetConsoleRequestsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	d, err := s.loadRequestStaff(r.Context(), s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Server) PostConsoleRequestsClaim(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	me := auth.MustPrincipal(ctx).UserID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var officer *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, triage_officer_id FROM service_requests WHERE id = $1 FOR UPDATE`, id).Scan(&status, &officer); err != nil {
			return err
		}
		if status != "submitted" && status != "triaging" {
			return httpx.Conflict("A request that is " + status + " cannot be claimed")
		}
		if officer != nil && *officer != me {
			return httpx.Conflict("Another triage officer has already taken this request")
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'triaging', triage_officer_id = $2, triage_started_at = COALESCE(triage_started_at, now()) WHERE id = $1`, id, me); err != nil {
			return err
		}
		return audit(ctx, tx, "request.claim", "service_request", id, map[string]any{"status": status}, map[string]any{"status": "triaging"}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	d, err := s.loadRequestStaff(ctx, s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// ---- shortlist: the engine proposes, a human assigns (report §5.2 step 4)

func (s *Server) loadShortlist(ctx context.Context, q db, requestID uuid.UUID) ([]shortlistEntryDTO, error) {
	rows, err := q.Query(ctx, `
		SELECT e.id, e.rank, e.score::float8, e.score_breakdown, `+providerSummaryCols+`, e.shown_to_client, e.client_selected, e.generated_at
		FROM shortlist_entries e JOIN providers p ON p.user_id = e.provider_id JOIN users u ON u.id = p.user_id
		WHERE e.request_id = $1 ORDER BY e.rank`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []shortlistEntryDTO{}
	for rows.Next() {
		var e shortlistEntryDTO
		if err := rows.Scan(&e.ID, &e.Rank, &e.Score, &e.ScoreBreakdown, &e.Provider.ID, &e.Provider.FullName, &e.Provider.Status, &e.Provider.Phone,
			&e.Provider.RatingAvg, &e.Provider.AppraisalScore, &e.Provider.CompletedJobs, &e.Provider.HomeServiceAreaID,
			&e.ShownToClient, &e.ClientSelected, &e.GeneratedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Server) GetConsoleRequestsShortlist(w http.ResponseWriter, r *http.Request, id gen.Id) {
	out, err := s.loadShortlist(r.Context(), s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) PostConsoleRequestsShortlist(w http.ResponseWriter, r *http.Request, id gen.Id, params gen.PostConsoleRequestsShortlistParams) {
	ctx := r.Context()
	size := 5
	if params.Size != nil && *params.Size > 0 && *params.Size <= 20 {
		size = *params.Size
	}
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var categoryID uuid.UUID
		var areaID *uuid.UUID
		var start time.Time
		var end *time.Time
		var defMin *int
		var lat, lng *float64
		if err := tx.QueryRow(ctx, `
			SELECT r.status::text, r.category_id, r.service_area_id, r.requested_start, r.requested_end, sv.default_duration_minutes,
			       COALESCE(a.lat, sa.centre_lat)::float8, COALESCE(a.lng, sa.centre_lng)::float8
			FROM service_requests r JOIN services sv ON sv.id = r.service_id JOIN addresses a ON a.id = r.address_id
			LEFT JOIN service_areas sa ON sa.id = a.service_area_id
			WHERE r.id = $1 FOR UPDATE OF r`, id).Scan(&status, &categoryID, &areaID, &start, &end, &defMin, &lat, &lng); err != nil {
			return err
		}
		if !domain.CanTriage(status) {
			return httpx.Conflict("A request that is " + status + " cannot be triaged")
		}
		ws, we := requestWindow(start, end, defMin)
		rows, err := tx.Query(ctx, `
			SELECT p.user_id, p.rating_avg::float8, p.rating_count, p.completed_jobs,
			       -- providers set a home area in the portal, not coordinates: use the area's centre
			       COALESCE(p.home_lat, ha.centre_lat)::float8, COALESCE(p.home_lng, ha.centre_lng)::float8
			FROM providers p JOIN provider_categories pc ON pc.provider_id = p.user_id AND pc.category_id = $1 AND pc.status = 'active'
			LEFT JOIN service_areas ha ON ha.id = p.home_service_area_id
			WHERE p.status = 'active'
			  AND ($2::uuid IS NULL OR NOT EXISTS (SELECT 1 FROM provider_service_areas x WHERE x.provider_id = p.user_id)
			       OR EXISTS (SELECT 1 FROM provider_service_areas x WHERE x.provider_id = p.user_id AND x.service_area_id = $2))`, categoryID, areaID)
		if err != nil {
			return err
		}
		type cand struct {
			id          uuid.UUID
			rating      *float64
			ratingCount int
			completed   int
			lat, lng    *float64
		}
		var cands []cand
		for rows.Next() {
			var c cand
			if err := rows.Scan(&c.id, &c.rating, &c.ratingCount, &c.completed, &c.lat, &c.lng); err != nil {
				rows.Close()
				return err
			}
			cands = append(cands, c)
		}
		rows.Close()

		type scored struct {
			id uuid.UUID
			m  domain.MatchScore
		}
		var ranked []scored
		for _, c := range cands {
			f, err := s.providerFacts(ctx, tx, c.id, categoryID, ws, we, nil)
			if err != nil {
				return err
			}
			if domain.CheckAssignable(f) != nil {
				continue
			}
			dist := 7.5 // neutral when either side has no coordinates
			if lat != nil && lng != nil && c.lat != nil && c.lng != nil {
				dist = domain.DistanceMetres(*lat, *lng, *c.lat, *c.lng) / 1000
			}
			in := domain.MatchInput{DistanceKm: dist, RatingCount: c.ratingCount, LiveJobs: f.LiveJobs, MaxJobs: f.MaxConcurrentJobs, CompletedJob: c.completed}
			if c.rating != nil {
				in.RatingAvg = *c.rating
			}
			ranked = append(ranked, scored{c.id, domain.ScoreMatch(in)})
		}
		// Highest score first; ties broken by id so regenerating gives a stable order.
		for i := 1; i < len(ranked); i++ {
			for j := i; j > 0 && (ranked[j].m.Score > ranked[j-1].m.Score ||
				(ranked[j].m.Score == ranked[j-1].m.Score && ranked[j].id.String() < ranked[j-1].id.String())); j-- {
				ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
			}
		}
		if len(ranked) > size {
			ranked = ranked[:size]
		}
		if _, err := tx.Exec(ctx, `DELETE FROM shortlist_entries WHERE request_id = $1`, id); err != nil {
			return err
		}
		for i, c := range ranked {
			breakdown := map[string]float64{"skill": c.m.Skill, "distance_km": c.m.DistanceKm, "rating": c.m.Rating, "availability": c.m.Availability, "workload": c.m.Workload}
			if _, err := tx.Exec(ctx, `INSERT INTO shortlist_entries (request_id, provider_id, rank, score, score_breakdown) VALUES ($1,$2,$3,$4,$5)`,
				id, c.id, i+1, c.m.Score, breakdown); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "request.shortlist", "service_request", id, nil, map[string]any{"candidates": len(ranked)}, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	out, err := s.loadShortlist(ctx, s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ---- assignment

func (s *Server) PostConsoleRequestsAssignments(w http.ResponseWriter, r *http.Request, id gen.Id, params gen.PostConsoleRequestsAssignmentsParams) {
	s.idempotent(w, r, params.IdempotencyKey, func(_ []byte) (int, any, error) {
		in, err := httpx.Decode[gen.AssignmentCreate](r)
		if err != nil {
			return 0, nil, err
		}
		reason := strings.TrimSpace(in.SelectionReason)
		if len(reason) < 3 {
			return 0, nil, httpx.Invalid("selection_reason is required")
		}
		ctx := r.Context()
		me := auth.MustPrincipal(ctx).UserID
		expiry := time.Duration(s.setting(ctx, s.DB, "offer.expiry_minutes", 120)) * time.Minute
		if in.OfferExpiresInMinutes != nil {
			if *in.OfferExpiresInMinutes < 5 || *in.OfferExpiresInMinutes > 1440 {
				return 0, nil, httpx.Invalid("offer_expires_in_minutes must be between 5 and 1440")
			}
			expiry = time.Duration(*in.OfferExpiresInMinutes) * time.Minute
		}
		var assignmentID uuid.UUID
		var ref, providerPhone string
		err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			var status string
			var categoryID uuid.UUID
			var start time.Time
			var end *time.Time
			var defMin *int
			if err := tx.QueryRow(ctx, `
				SELECT r.status::text, r.category_id, r.requested_start, r.requested_end, sv.default_duration_minutes, r.reference
				FROM service_requests r JOIN services sv ON sv.id = r.service_id WHERE r.id = $1 FOR UPDATE OF r`, id).
				Scan(&status, &categoryID, &start, &end, &defMin, &ref); err != nil {
				return err
			}
			if !domain.CanTriage(status) {
				return httpx.Conflict("A request that is " + status + " cannot be assigned")
			}
			var live int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM assignments WHERE request_id = $1 AND status IN ('offered','accepted')`, id).Scan(&live); err != nil {
				return err
			}
			if live > 0 {
				return httpx.Conflict("This request already has a live offer. Withdraw it first.")
			}
			ws, we := requestWindow(start, end, defMin)
			facts, err := s.providerFacts(ctx, tx, in.ProviderId, categoryID, ws, we, nil)
			if err != nil {
				return httpx.Invalid("Unknown provider")
			}
			if he := domain.CheckAssignable(facts); he != nil {
				return he
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO assignments (request_id, provider_id, selected_by, selection_reason, offer_expires_at)
				VALUES ($1,$2,$3,$4,$5) RETURNING id`, id, in.ProviderId, me, reason, s.Now().Add(expiry)).Scan(&assignmentID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'awaiting_provider', triage_officer_id = COALESCE(triage_officer_id, $2),
				triage_started_at = COALESCE(triage_started_at, now()) WHERE id = $1`, id, me); err != nil {
				return err
			}
			_ = tx.QueryRow(ctx, `SELECT phone_e164 FROM users WHERE id = $1`, in.ProviderId).Scan(&providerPhone)
			if err := audit(ctx, tx, "assignment.create", "assignment", assignmentID, nil, map[string]any{"request_id": id, "provider_id": in.ProviderId}, reason); err != nil {
				return err
			}
			return emit(ctx, tx, "assignment.offered", "assignment", assignmentID, map[string]any{"request_id": id, "provider_id": in.ProviderId})
		})
		if err != nil {
			return 0, nil, mapErr(err)
		}
		if providerPhone != "" {
			// Best effort until the outbox relay owns notifications.
			_ = s.SMS.Send(ctx, providerPhone, "Homend Residence: new job offer "+ref+". Open your portal to accept or decline.")
		}
		as, err := s.loadAssignments(ctx, s.DB, id)
		if err != nil {
			return 0, nil, err
		}
		for _, a := range as {
			if a.ID == assignmentID {
				return http.StatusCreated, a, nil
			}
		}
		return 0, nil, httpx.ErrNotFound
	})
}

func (s *Server) PostConsoleAssignmentsWithdraw(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ReasonInput](r)
	if err != nil || len(strings.TrimSpace(in.Reason)) < 3 {
		httpx.Fail(w, r, httpx.Invalid("reason is required"))
		return
	}
	ctx := r.Context()
	var reqID uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT request_id, status::text FROM assignments WHERE id = $1 FOR UPDATE`, id).Scan(&reqID, &status); err != nil {
			return err
		}
		if status != "offered" && status != "accepted" {
			return httpx.Conflict("An assignment that is " + status + " cannot be withdrawn")
		}
		if _, err := tx.Exec(ctx, `UPDATE assignments SET status = 'withdrawn', ended_at = now(), ended_reason = $2 WHERE id = $1`, id, in.Reason); err != nil {
			return err
		}
		// Back to triage so management can pick a replacement; scheduled visits go with the provider.
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'triaging', confirmed_at = NULL WHERE id = $1 AND status IN ('awaiting_provider','confirmed')`, reqID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE visits SET status = 'cancelled' WHERE assignment_id = $1 AND status = 'scheduled'`, id); err != nil {
			return err
		}
		if err := audit(ctx, tx, "assignment.withdraw", "assignment", id, map[string]any{"status": status}, map[string]any{"status": "withdrawn"}, in.Reason); err != nil {
			return err
		}
		return emit(ctx, tx, "assignment.withdrawn", "assignment", id, map[string]any{"request_id": reqID})
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	as, err := s.loadAssignments(ctx, s.DB, reqID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, a := range as {
		if a.ID == uuid.UUID(id) {
			httpx.JSON(w, http.StatusOK, a)
			return
		}
	}
	httpx.Fail(w, r, httpx.ErrNotFound)
}

func (s *Server) PostConsoleRequestsMarkUnfilled(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.ReasonInput](r)
	if err != nil || len(strings.TrimSpace(in.Reason)) < 3 {
		httpx.Fail(w, r, httpx.Invalid("reason is required"))
		return
	}
	ctx := r.Context()
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM service_requests WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
			return err
		}
		if !domain.CanTriage(status) {
			return httpx.Conflict("A request that is " + status + " cannot be marked unfilled")
		}
		if _, err := tx.Exec(ctx, `UPDATE assignments SET status = 'withdrawn', ended_at = now(), ended_reason = 'request unfilled' WHERE request_id = $1 AND status = 'offered'`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'unfilled', cancel_reason = $2 WHERE id = $1`, id, in.Reason); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO job_exceptions (kind, request_id, details) VALUES ('unfilled', $1, $2)`, id, []byte(`{"reason":"marked unfilled by triage"}`)); err != nil {
			return err
		}
		if err := audit(ctx, tx, "request.unfilled", "service_request", id, map[string]any{"status": status}, map[string]any{"status": "unfilled"}, in.Reason); err != nil {
			return err
		}
		return emit(ctx, tx, "request.unfilled", "service_request", id, map[string]any{})
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	d, err := s.loadRequestStaff(ctx, s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Server) GetConsoleDashboard(w http.ResponseWriter, r *http.Request) {
	var d struct {
		TriageQueue                  int                  `json:"triage_queue"`
		AwaitingProvider             int                  `json:"awaiting_provider"`
		ApplicationsAwaitingVetting  int                  `json:"applications_awaiting_vetting"`
		ApplicationsAwaitingApproval int                  `json:"applications_awaiting_approval"`
		OpenComplaints               int                  `json:"open_complaints"`
		OpenSafeguardingComplaints   int                  `json:"open_safeguarding_complaints"`
		OpenExceptions               int                  `json:"open_exceptions"`
		DocumentsExpiring30d         int                  `json:"documents_expiring_30d"`
		PayoutsAwaitingApproval      int                  `json:"payouts_awaiting_approval"`
		TodaysVisits                 int                  `json:"todays_visits"`
		NewLeads                     int                  `json:"new_leads"`
		SegregationWarnings          []segregationWarning `json:"segregation_warnings"`
	}
	err := s.DB.QueryRow(r.Context(), `
		SELECT
		 (SELECT count(*) FROM service_requests WHERE status IN ('submitted','triaging')),
		 (SELECT count(*) FROM service_requests WHERE status = 'awaiting_provider'),
		 (SELECT count(*) FROM applications WHERE status IN ('submitted','in_vetting')),
		 (SELECT count(*) FROM applications WHERE status = 'awaiting_approval'),
		 (SELECT count(*) FROM complaints WHERE status NOT IN ('closed','withdrawn')),
		 (SELECT count(*) FROM complaints WHERE status NOT IN ('closed','withdrawn') AND severity IN ('safeguarding','loss')),
		 (SELECT count(*) FROM job_exceptions WHERE reviewed_at IS NULL),
		 (SELECT count(*) FROM documents WHERE status = 'verified' AND expires_on BETWEEN current_date AND current_date + 30),
		 (SELECT count(*) FROM payout_batches WHERE status = 'draft'),
		 (SELECT count(*) FROM visits WHERE scheduled_start::date = current_date AND status IN ('scheduled','checked_in')),
		 (SELECT count(*) FROM leads WHERE status = 'new')`).
		Scan(&d.TriageQueue, &d.AwaitingProvider, &d.ApplicationsAwaitingVetting, &d.ApplicationsAwaitingApproval, &d.OpenComplaints,
			&d.OpenSafeguardingComplaints, &d.OpenExceptions, &d.DocumentsExpiring30d, &d.PayoutsAwaitingApproval, &d.TodaysVisits, &d.NewLeads)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if d.SegregationWarnings, err = s.segregationWarnings(r.Context()); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

type segregationWarning struct {
	Duty    string   `json:"duty"`
	Label   string   `json:"label"`
	Message string   `json:"message"`
	People  int      `json:"people"`
	Roles   []string `json:"roles"`
}

// twoPersonDuties are the steps the database refuses to let one person do alone: who prepares or
// investigates (makers) and who signs off (checkers). Admins hold every role.
var twoPersonDuties = []struct {
	duty, label, why string
	makers, checkers []string
}{
	{"provider_approval", "Provider approvals", "an application's vetting officer cannot also approve it",
		[]string{"vetting_officer"}, []string{"approver"}},
	{"payouts_and_refunds", "Payouts and refunds", "the finance officer who prepares a payout batch or refund cannot also approve it",
		[]string{"finance"}, []string{"finance"}},
	{"complaint_decisions", "Complaint decisions", "a complaint's investigator cannot also decide it",
		[]string{"supervisor", "complaints_manager"}, []string{"complaints_manager"}},
}

// segregationWarnings lists the two-person duties no pair of active staff can complete: there must
// be a maker and a different checker.
func (s *Server) segregationWarnings(ctx context.Context) ([]segregationWarning, error) {
	out := []segregationWarning{}
	for _, d := range twoPersonDuties {
		var makers, checkers, people int
		if err := s.DB.QueryRow(ctx, `
			WITH staff AS (
			  SELECT DISTINCT u.id, (r.role::text = ANY($1) OR r.role = 'admin') AS maker, (r.role::text = ANY($2) OR r.role = 'admin') AS checker
			  FROM users u JOIN staff_roles r ON r.user_id = u.id
			  WHERE u.kind = 'staff' AND u.status = 'active')
			SELECT count(DISTINCT id) FILTER (WHERE maker), count(DISTINCT id) FILTER (WHERE checker), count(DISTINCT id) FILTER (WHERE maker OR checker)
			FROM staff`, d.makers, d.checkers).Scan(&makers, &checkers, &people); err != nil {
			return nil, err
		}
		if makers > 0 && checkers > 0 && people >= 2 {
			continue
		}
		roles := append([]string{}, d.checkers...)
		if d.makers[0] != d.checkers[0] {
			roles = append(d.makers[:1:1], roles...)
		}
		who := "Nobody active holds the roles"
		if people == 1 {
			who = "Only one active staff member holds the roles"
		}
		out = append(out, segregationWarning{Duty: d.duty, Label: d.label, People: people, Roles: roles,
			Message: fmt.Sprintf("%s for this, and %s, so it cannot be completed. Give a second person the %s role.", who, d.why, strings.ReplaceAll(strings.Join(roles, " or "), "_", " "))})
	}
	return out, nil
}

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	dbpkg "github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/domain"
	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// ---- offers

func (s *Server) GetProviderOffers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	rows, err := s.DB.Query(ctx, `
		SELECT a.id, a.status::text, a.offer_expires_at, r.reference, r.service_id, ad.estate, r.service_area_id,
		       r.requested_start, r.requested_end, r.recurrence_rule, r.provider_pay_minor, r.client_notes
		FROM assignments a JOIN service_requests r ON r.id = a.request_id JOIN addresses ad ON ad.id = r.address_id
		WHERE a.provider_id = $1 AND a.status = 'offered' AND a.offer_expires_at > now() ORDER BY a.offer_expires_at`, pid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	type row struct {
		o         offerDTO
		serviceID uuid.UUID
		areaID    *uuid.UUID
	}
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.o.ID, &x.o.Status, &x.o.OfferExpiresAt, &x.o.RequestReference, &x.serviceID, &x.o.Estate, &x.areaID,
			&x.o.RequestedStart, &x.o.RequestedEnd, &x.o.RecurrenceRule, &x.o.ProviderPayMinor, &x.o.ClientNotes); err != nil {
			rows.Close()
			httpx.Fail(w, r, err)
			return
		}
		list = append(list, x)
	}
	rows.Close()
	out := []offerDTO{}
	for _, x := range list {
		if x.o.Service, err = scanService(s.DB.QueryRow(ctx, `SELECT `+serviceCols+` FROM services s WHERE s.id = $1`, x.serviceID)); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if x.areaID != nil {
			var a serviceAreaDTO
			err := s.DB.QueryRow(ctx, `SELECT id, slug, name, county, is_active, centre_lat::float8, centre_lng::float8, radius_km::float8 FROM service_areas WHERE id = $1`, *x.areaID).
				Scan(&a.ID, &a.Slug, &a.Name, &a.County, &a.IsActive, &a.CentreLat, &a.CentreLng, &a.RadiusKm)
			if err == nil {
				x.o.ServiceArea = &a
			}
		}
		out = append(out, x.o)
	}
	httpx.JSON(w, http.StatusOK, out)
}

type jobDTO struct {
	AssignmentID     uuid.UUID  `json:"assignment_id"`
	RequestReference string     `json:"request_reference"`
	Status           string     `json:"status"`
	Service          serviceDTO `json:"service"`
	ClientName       string     `json:"client_display_name"`
	Address          addressDTO `json:"address"`
	Visits           []visitDTO `json:"visits"`
	ProviderPayMinor int64      `json:"provider_pay_minor"`
}

var errOfferExpired = httpx.ErrGone.With("reason", "offer_expired")

func (s *Server) PostProviderOffersAccept(w http.ResponseWriter, r *http.Request, id gen.Id, params gen.PostProviderOffersAcceptParams) {
	s.idempotent(w, r, params.IdempotencyKey, func(_ []byte) (int, any, error) {
		ctx := r.Context()
		pid := auth.MustPrincipal(ctx).UserID
		var clientPhone, ref string
		var reqID uuid.UUID
		err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			var status string
			var expires time.Time
			var categoryID uuid.UUID
			var start time.Time
			var end *time.Time
			var defMin *int
			if err := tx.QueryRow(ctx, `
				SELECT a.request_id, a.status::text, a.offer_expires_at, r.category_id, r.requested_start, r.requested_end, sv.default_duration_minutes, r.reference
				FROM assignments a JOIN service_requests r ON r.id = a.request_id JOIN services sv ON sv.id = r.service_id
				WHERE a.id = $1 AND a.provider_id = $2 FOR UPDATE OF a, r`, id, pid).
				Scan(&reqID, &status, &expires, &categoryID, &start, &end, &defMin, &ref); err != nil {
				return err
			}
			if status != "offered" {
				return httpx.Conflict("This offer is " + status)
			}
			if !expires.After(s.Now()) {
				return errOfferExpired
			}
			ws, we := requestWindow(start, end, defMin)
			facts, err := s.providerFacts(ctx, tx, pid, categoryID, ws, we, (*uuid.UUID)(&id))
			if err != nil {
				return err
			}
			if he := domain.CheckAssignable(facts); he != nil {
				return he
			}
			if _, err := tx.Exec(ctx, `UPDATE assignments SET status = 'accepted', responded_at = now(), response_channel = $2::channel WHERE id = $1`, id, channelOf(ctx)); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'confirmed', confirmed_at = now() WHERE id = $1`, reqID); err != nil {
				return err
			}
			// One visit now; recurring schedules are expanded by the console's schedule-visits step.
			if _, err := tx.Exec(ctx, `INSERT INTO visits (request_id, assignment_id, provider_id, scheduled_start, scheduled_end) VALUES ($1,$2,$3,$4,$5)`,
				reqID, id, pid, ws, we); err != nil {
				return err
			}
			_ = tx.QueryRow(ctx, `SELECT u.phone_e164 FROM service_requests r JOIN users u ON u.id = r.client_id WHERE r.id = $1`, reqID).Scan(&clientPhone)
			if err := audit(ctx, tx, "assignment.accept", "assignment", id, map[string]any{"status": "offered"}, map[string]any{"status": "accepted"}, ""); err != nil {
				return err
			}
			return emit(ctx, tx, "request.confirmed", "service_request", reqID, map[string]any{"provider_id": pid})
		})
		if errors.Is(err, errOfferExpired) {
			// The failed transaction rolled back, so record the expiry on its own.
			s.expireOffer(ctx, uuid.UUID(id))
			return 0, nil, err
		}
		if err != nil {
			return 0, nil, mapErr(err)
		}
		if clientPhone != "" {
			_ = s.SMS.Send(ctx, clientPhone, "Homend Residence: your booking "+ref+" is confirmed.")
		}
		job, err := s.loadJob(ctx, s.DB, uuid.UUID(id), pid)
		return http.StatusOK, job, err
	})
}

func (s *Server) expireOffer(ctx context.Context, assignmentID uuid.UUID) {
	_ = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var reqID uuid.UUID
		if err := tx.QueryRow(ctx, `UPDATE assignments SET status = 'expired', ended_at = now(), ended_reason = 'offer expired'
			WHERE id = $1 AND status = 'offered' RETURNING request_id`, assignmentID).Scan(&reqID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'triaging' WHERE id = $1 AND status = 'awaiting_provider'`, reqID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO job_exceptions (kind, request_id, details) VALUES ('offer_expired', $1, '{}')`, reqID)
		return err
	})
}

func (s *Server) PostProviderOffersDecline(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, _ := httpx.Decode[gen.DeclineInput](r) // body is optional
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var reqID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT request_id, status::text FROM assignments WHERE id = $1 AND provider_id = $2 FOR UPDATE`, id, pid).Scan(&reqID, &status); err != nil {
			return err
		}
		if status != "offered" {
			return httpx.Conflict("This offer is " + status)
		}
		if _, err := tx.Exec(ctx, `UPDATE assignments SET status = 'declined', responded_at = now(), response_channel = $3::channel, decline_reason = $2, ended_at = now() WHERE id = $1`, id, in.Reason, channelOf(ctx)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'triaging' WHERE id = $1 AND status = 'awaiting_provider'`, reqID); err != nil {
			return err
		}
		if err := audit(ctx, tx, "assignment.decline", "assignment", id, nil, nil, derefStr(in.Reason)); err != nil {
			return err
		}
		return emit(ctx, tx, "assignment.declined", "assignment", id, map[string]any{"request_id": reqID})
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	httpx.NoContent(w)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Server) loadJob(ctx context.Context, q db, assignmentID, providerID uuid.UUID) (jobDTO, error) {
	var j jobDTO
	var reqID, serviceID, addrID uuid.UUID
	var clientName string
	var start time.Time
	err := q.QueryRow(ctx, `
		SELECT a.id, r.id, r.reference, r.status::text, r.service_id, r.address_id, u.full_name, r.provider_pay_minor, r.requested_start
		FROM assignments a JOIN service_requests r ON r.id = a.request_id JOIN users u ON u.id = r.client_id
		WHERE a.id = $1 AND a.provider_id = $2`, assignmentID, providerID).
		Scan(&j.AssignmentID, &reqID, &j.RequestReference, &j.Status, &serviceID, &addrID, &clientName, &j.ProviderPayMinor, &start)
	if err != nil {
		return j, mapErr(err)
	}
	j.ClientName = displayName(clientName)
	if j.Service, err = scanService(q.QueryRow(ctx, `SELECT `+serviceCols+` FROM services s WHERE s.id = $1`, serviceID)); err != nil {
		return j, err
	}
	if j.Address, err = s.loadAddress(ctx, q, addrID, nil); err != nil {
		return j, err
	}
	// Gate codes and guard names are released on the day, not at acceptance.
	if s.Now().Before(start.Add(-12 * time.Hour)) {
		j.Address.AccessDetails = nil
	}
	j.Visits, err = s.loadVisitsFor(ctx, q, reqID, providerID)
	return j, err
}

func (s *Server) loadVisitsFor(ctx context.Context, q db, reqID, providerID uuid.UUID) ([]visitDTO, error) {
	all, err := s.loadVisits(ctx, q, reqID)
	if err != nil {
		return nil, err
	}
	out := []visitDTO{}
	for _, v := range all {
		var n int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM visits WHERE id = $1 AND provider_id = $2`, v.ID, providerID).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			out = append(out, v)
		}
	}
	return out, nil
}

// ---- visits

func (s *Server) GetProviderVisits(w http.ResponseWriter, r *http.Request, params gen.GetProviderVisitsParams) {
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	var from, to *time.Time
	if params.From != nil {
		from = &params.From.Time
	}
	if params.To != nil {
		t := params.To.Time.Add(24 * time.Hour)
		to = &t
	}
	rows, err := s.DB.Query(ctx, `
		SELECT id, request_id, status::text, scheduled_start, scheduled_end, checked_in_at, checkin_method::text, checked_out_at, client_confirmed_at, provider_notes
		FROM visits WHERE provider_id = $1 AND ($2::timestamptz IS NULL OR scheduled_start >= $2) AND ($3::timestamptz IS NULL OR scheduled_start < $3)
		ORDER BY scheduled_start`, pid, from, to)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer rows.Close()
	out := []visitDTO{}
	for rows.Next() {
		var v visitDTO
		if err := rows.Scan(&v.ID, &v.RequestID, &v.Status, &v.ScheduledStart, &v.ScheduledEnd, &v.CheckedInAt, &v.CheckinMethod, &v.CheckedOutAt, &v.ClientConfirmedAt, &v.ProviderNotes); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) loadVisit(ctx context.Context, q db, id uuid.UUID) (visitDTO, error) {
	var v visitDTO
	err := q.QueryRow(ctx, `
		SELECT id, request_id, status::text, scheduled_start, scheduled_end, checked_in_at, checkin_method::text, checked_out_at, client_confirmed_at, provider_notes
		FROM visits WHERE id = $1`, id).Scan(&v.ID, &v.RequestID, &v.Status, &v.ScheduledStart, &v.ScheduledEnd, &v.CheckedInAt, &v.CheckinMethod, &v.CheckedOutAt, &v.ClientConfirmedAt, &v.ProviderNotes)
	return v, mapErr(err)
}

const maxDoorCodeAttempts = 5

func (s *Server) PostProviderVisitsCheckIn(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.CheckInInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	now := s.Now()
	if in.DeviceRecordedAt.After(now.Add(5 * time.Minute)) {
		httpx.Fail(w, r, httpx.Invalid("device_recorded_at is in the future"))
		return
	}
	method := string(in.Method)
	storedMethod := method
	if channelOf(r.Context()) == "ussd" {
		storedMethod = "ussd"
	}
	if method == "geo" && (in.Lat == nil || in.Lng == nil) {
		httpx.Fail(w, r, httpx.Invalid("lat and lng are required for a geo check-in"))
		return
	}
	if method == "code" && (in.Code == nil || len(*in.Code) != 4) {
		httpx.Fail(w, r, httpx.Invalid("The 4-digit door code is required"))
		return
	}

	var badCode bool
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		// A retried event (offline sync, double tap) is a no-op that returns the current visit.
		// Serialise concurrent copies of one event so the loser sees it and returns the original result.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, in.ClientEventId); err != nil {
			return err
		}
		var seen int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM visit_events WHERE client_event_id = $1`, in.ClientEventId).Scan(&seen); err != nil {
			return err
		}
		if seen > 0 {
			return nil
		}
		var status string
		var start, end time.Time
		var codeHash []byte
		var attempts int
		var reqID uuid.UUID
		var alat, alng *float64
		if err := tx.QueryRow(ctx, `
			SELECT v.status::text, v.scheduled_start, v.scheduled_end, v.checkin_code_hash, v.checkin_attempts, v.request_id, ad.lat::float8, ad.lng::float8
			FROM visits v JOIN service_requests r ON r.id = v.request_id JOIN addresses ad ON ad.id = r.address_id
			WHERE v.id = $1 AND v.provider_id = $2 FOR UPDATE OF v`, id, pid).Scan(&status, &start, &end, &codeHash, &attempts, &reqID, &alat, &alng); err != nil {
			return err
		}
		if status != "scheduled" {
			return httpx.Conflict("This visit is " + status)
		}
		if in.DeviceRecordedAt.Before(start.Add(-time.Hour)) || in.DeviceRecordedAt.After(end.Add(6*time.Hour)) {
			return httpx.RuleViolation("Check-in is only possible from one hour before the visit")
		}
		var distance *int
		if method == "code" {
			if attempts >= maxDoorCodeAttempts {
				return httpx.ErrRateLimited.With("reason", "door code locked until the client generates a new one")
			}
			if codeHash == nil || !hmacEqual(codeHash, s.doorCodeHash(uuid.UUID(id), *in.Code)) {
				badCode = true
				return nil // commit nothing here; the attempt is counted below, outside this transaction
			}
		} else {
			if alat != nil && alng != nil {
				d := int(domain.DistanceMetres(*in.Lat, *in.Lng, *alat, *alng))
				distance = &d
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE visits SET status = 'checked_in', checked_in_at = $2, checkin_method = $3::checkin_method, checkin_lat = $4, checkin_lng = $5,
			  checkin_distance_m = $6, device_recorded_at = $2 WHERE id = $1`, id, in.DeviceRecordedAt, storedMethod, in.Lat, in.Lng, distance); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'in_progress' WHERE id = $1 AND status = 'confirmed'`, reqID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO visit_events (visit_id, client_event_id, event_type, payload, device_recorded_at, channel) VALUES ($1,$2,'check_in','{}',$3,$4::channel)`,
			id, in.ClientEventId, in.DeviceRecordedAt, channelOf(ctx)); err != nil {
			return err
		}
		if distance != nil && float64(*distance) > s.setting(ctx, tx, "checkin.max_distance_m", 300) {
			if _, err := tx.Exec(ctx, `INSERT INTO job_exceptions (kind, request_id, visit_id, provider_id, details) VALUES ('geo_mismatch',$1,$2,$3,$4)`,
				reqID, id, pid, []byte(`{"distance_m":`+strconv.Itoa(*distance)+`}`)); err != nil {
				return err
			}
		}
		if in.DeviceRecordedAt.After(start.Add(15 * time.Minute)) {
			if _, err := tx.Exec(ctx, `INSERT INTO job_exceptions (kind, request_id, visit_id, provider_id, details) VALUES ('late_arrival',$1,$2,$3,'{}')`, reqID, id, pid); err != nil {
				return err
			}
		}
		return audit(ctx, tx, "visit.check_in", "visit", id, nil, map[string]any{"method": method}, "")
	})
	if badCode {
		_, _ = s.DB.Exec(ctx, `UPDATE visits SET checkin_attempts = checkin_attempts + 1 WHERE id = $1 AND provider_id = $2`, id, pid)
		httpx.Fail(w, r, httpx.NewError(http.StatusUnprocessableEntity, "invalid_code", "That door code is wrong"))
		return
	}
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	v, err := s.loadVisit(ctx, s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) PostProviderVisitsCheckOut(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.CheckOutInput](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	pid := auth.MustPrincipal(ctx).UserID
	if in.DeviceRecordedAt.After(s.Now().Add(5 * time.Minute)) {
		httpx.Fail(w, r, httpx.Invalid("device_recorded_at is in the future"))
		return
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		// Serialise concurrent copies of one event so the loser sees it and returns the original result.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, in.ClientEventId); err != nil {
			return err
		}
		var seen int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM visit_events WHERE client_event_id = $1`, in.ClientEventId).Scan(&seen); err != nil {
			return err
		}
		if seen > 0 {
			return nil
		}
		var status string
		var reqID uuid.UUID
		var checkedIn *time.Time
		if err := tx.QueryRow(ctx, `SELECT status::text, request_id, checked_in_at FROM visits WHERE id = $1 AND provider_id = $2 FOR UPDATE`, id, pid).Scan(&status, &reqID, &checkedIn); err != nil {
			return err
		}
		if status != "checked_in" {
			return httpx.Conflict("Check in before checking out (visit is " + status + ")")
		}
		if checkedIn != nil && in.DeviceRecordedAt.Before(*checkedIn) {
			return httpx.Invalid("Check-out cannot be before check-in")
		}
		if _, err := tx.Exec(ctx, `UPDATE visits SET status = 'completed', checked_out_at = $2, provider_notes = COALESCE($3, provider_notes) WHERE id = $1`,
			id, in.DeviceRecordedAt, trimPtr(in.Notes)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO visit_events (visit_id, client_event_id, event_type, payload, device_recorded_at, channel) VALUES ($1,$2,'check_out','{}',$3,$4::channel)`,
			id, in.ClientEventId, in.DeviceRecordedAt, channelOf(ctx)); err != nil {
			return err
		}
		if err := s.completeIfDone(ctx, tx, reqID); err != nil {
			return err
		}
		return audit(ctx, tx, "visit.check_out", "visit", id, nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	v, err := s.loadVisit(ctx, s.DB, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	t := strings.TrimSpace(*p)
	if t == "" {
		return nil
	}
	return &t
}

func hmacEqual(a, b []byte) bool { return crypt.Equal(a, b) }

// completeIfDone moves a job to awaiting_rating once no visit is left to do, and raises its invoice.
// Withdrawn variations and the invoice both follow, because the price is fixed from here.
func (s *Server) completeIfDone(ctx context.Context, tx pgx.Tx, reqID uuid.UUID) error {
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM visits WHERE request_id = $1 AND status IN ('scheduled','checked_in')`, reqID).Scan(&open); err != nil {
		return err
	}
	if open > 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'awaiting_rating', completed_at = now() WHERE id = $1 AND status = 'in_progress'`, reqID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		// The price is fixed from here, so a change nobody answered falls away.
		if _, err := tx.Exec(ctx, `UPDATE variations SET status = 'withdrawn', decided_at = now() WHERE request_id = $1 AND status = 'pending'`, reqID); err != nil {
			return err
		}
		if _, err := s.createRequestInvoice(ctx, tx, reqID); err != nil {
			return err
		}
	}
	return emit(ctx, tx, "request.completed", "service_request", reqID, map[string]any{})
}

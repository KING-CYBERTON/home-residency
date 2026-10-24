package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
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

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// ---- addresses

func (s *Server) GetClientAddresses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	rows, err := s.DB.Query(ctx, `SELECT id FROM addresses WHERE client_id = $1 AND archived_at IS NULL ORDER BY is_default DESC, created_at`, cid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			httpx.Fail(w, r, err)
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []addressDTO{}
	for _, id := range ids {
		a, err := s.loadAddress(ctx, s.DB, id, &cid)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out = append(out, a)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func validateAddress(in gen.AddressInput, creating bool) error {
	if creating && strings.TrimSpace(in.Label) == "" {
		return httpx.Invalid("label is required")
	}
	if (in.Lat == nil) != (in.Lng == nil) {
		return httpx.Invalid("Give both lat and lng, or neither")
	}
	if in.Lat != nil && (*in.Lat < -90 || *in.Lat > 90 || *in.Lng < -180 || *in.Lng > 180) {
		return httpx.Invalid("lat/lng out of range")
	}
	return nil
}

func (s *Server) encryptOpt(v *string) ([]byte, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	return s.Cipher.Encrypt([]byte(*v))
}

func (s *Server) PostClientAddresses(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.AddressInput](r)
	if err == nil {
		err = validateAddress(in, true)
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	enc, err := s.encryptOpt(in.AccessDetails)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var id uuid.UUID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM addresses WHERE client_id = $1 AND archived_at IS NULL`, cid).Scan(&n); err != nil {
			return err
		}
		isDefault := n == 0 || (in.IsDefault != nil && *in.IsDefault)
		if isDefault {
			if _, err := tx.Exec(ctx, `UPDATE addresses SET is_default = false WHERE client_id = $1 AND is_default`, cid); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `
			INSERT INTO addresses (client_id, label, service_area_id, estate, street, house_number, landmark, lat, lng, access_details_enc, is_default)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
			cid, strings.TrimSpace(in.Label), in.ServiceAreaId, in.Estate, in.Street, in.HouseNumber, in.Landmark, in.Lat, in.Lng, enc, isDefault).Scan(&id)
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	a, err := s.loadAddress(ctx, s.DB, id, &cid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, a)
}

func (s *Server) PatchClientAddressesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.AddressInput](r)
	if err == nil {
		err = validateAddress(in, false)
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	enc, err := s.encryptOpt(in.AccessDetails)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		if in.IsDefault != nil && *in.IsDefault {
			if _, err := tx.Exec(ctx, `UPDATE addresses SET is_default = false WHERE client_id = $1 AND is_default AND id <> $2`, cid, id); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `
			UPDATE addresses SET label = COALESCE(NULLIF($3,''), label), service_area_id = COALESCE($4, service_area_id),
			  estate = COALESCE($5, estate), street = COALESCE($6, street), house_number = COALESCE($7, house_number),
			  landmark = COALESCE($8, landmark), lat = COALESCE($9, lat), lng = COALESCE($10, lng),
			  access_details_enc = COALESCE($11, access_details_enc), is_default = COALESCE($12, is_default)
			WHERE id = $1 AND client_id = $2 AND archived_at IS NULL`,
			id, cid, strings.TrimSpace(in.Label), in.ServiceAreaId, in.Estate, in.Street, in.HouseNumber, in.Landmark, in.Lat, in.Lng, enc, in.IsDefault)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return nil
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	a, err := s.loadAddress(ctx, s.DB, id, &cid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

func (s *Server) DeleteClientAddressesById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	// Archive rather than delete: past requests keep pointing at the address they were booked for.
	tag, err := s.DB.Exec(ctx, `UPDATE addresses SET archived_at = now(), is_default = false WHERE id = $1 AND client_id = $2 AND archived_at IS NULL`, id, cid)
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

// ---- pricing / quotes

type priced struct {
	service  serviceDTO
	category categoryDTO
	address  addressDTO
	rate     *rateCardEntryDTO
	qty      float64
	price    domain.Price
}

func (s *Server) priceFor(ctx context.Context, q db, clientID, serviceID, addressID uuid.UUID, qtyIn *float32) (*priced, error) {
	p := &priced{qty: 1}
	if qtyIn != nil {
		p.qty = round2(float64(*qtyIn))
	}
	var err error
	if p.service, err = scanService(q.QueryRow(ctx, `SELECT `+serviceCols+` FROM services s WHERE s.id = $1`, serviceID)); err != nil {
		return nil, httpx.Invalid("Unknown service")
	}
	if p.category, err = scanCategory(q.QueryRow(ctx, `SELECT `+categoryCols+` FROM service_categories c WHERE c.id = $1`, p.service.CategoryID)); err != nil {
		return nil, err
	}
	if !p.service.IsActive || !p.category.IsActive {
		return nil, httpx.RuleViolation("This service is not available yet")
	}
	if p.qty < p.service.MinQuantity || (p.service.MaxQuantity != nil && p.qty > *p.service.MaxQuantity) {
		return nil, httpx.Invalid(fmt.Sprintf("quantity must be at least %v", p.service.MinQuantity))
	}
	if p.address, err = s.loadAddress(ctx, q, addressID, &clientID); err != nil {
		return nil, httpx.Invalid("Unknown address")
	}
	if p.rate, err = currentRate(ctx, q, serviceID, p.address.ServiceAreaID); err != nil {
		return nil, err
	}
	if p.rate == nil {
		return nil, httpx.RuleViolation("No price is published for this service in your area")
	}
	vat := s.setting(ctx, q, "vat.rate_pct", 16)
	p.price = domain.ComputePrice(p.rate.ClientPriceMinor, p.qty, p.rate.PlacementFeeMinor, vat)
	return p, nil
}

// categoryWindow is when the category's visits may start.
func categoryWindow(c categoryDTO) domain.BookingWindow {
	w := domain.BookingWindow{Category: c.Name, MinLead: time.Duration(c.MinLeadMinutes) * time.Minute}
	if c.OpensAt != nil && c.ClosesAt != nil {
		w.OpensAt, w.ClosesAt = *c.OpensAt, *c.ClosesAt
	}
	return w
}

func (s *Server) areaSupported(ctx context.Context, q db, areaID *uuid.UUID) bool {
	if areaID == nil {
		return false
	}
	var ok bool
	_ = q.QueryRow(ctx, `SELECT is_active FROM service_areas WHERE id = $1`, *areaID).Scan(&ok)
	return ok
}

func (s *Server) PostClientQuotes(w http.ResponseWriter, r *http.Request) {
	in, err := httpx.Decode[gen.QuoteRequest](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	p, err := s.priceFor(ctx, s.DB, auth.MustPrincipal(ctx).UserID, in.ServiceId, in.AddressId, in.Quantity)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, quoteDTO{
		ServiceID: p.service.ID, RateCardEntryID: p.rate.ID, Quantity: p.qty, UnitPriceMinor: p.rate.ClientPriceMinor,
		PlacementFeeMinor: p.rate.PlacementFeeMinor, ServiceMinor: p.price.ServiceMinor, RoundingMinor: p.price.RoundingMinor, SubtotalMinor: p.price.SubtotalMinor, VATMinor: p.price.VATMinor,
		TotalMinor: p.price.TotalMinor, Currency: p.rate.Currency, ServiceAreaSupported: s.areaSupported(ctx, s.DB, p.address.ServiceAreaID),
	})
}

// ---- requests

func (s *Server) PostClientRequests(w http.ResponseWriter, r *http.Request, params gen.PostClientRequestsParams) {
	s.idempotent(w, r, params.IdempotencyKey, func(raw []byte) (int, any, error) {
		in, err := httpx.Decode[gen.ServiceRequestCreate](r)
		if err != nil {
			return 0, nil, err
		}
		ctx := r.Context()
		cid := auth.MustPrincipal(ctx).UserID
		now := s.Now()
		if in.RequestedStart.Before(now.Add(-5 * time.Minute)) {
			return 0, nil, httpx.Invalid("requested_start is in the past")
		}
		if in.RequestedEnd != nil && !in.RequestedEnd.After(in.RequestedStart) {
			return 0, nil, httpx.Invalid("requested_end must be after requested_start")
		}
		var id uuid.UUID
		err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
			if err := acceptTerms(ctx, tx, r, cid, in.AcceptTerms); err != nil {
				return err
			}
			p, err := s.priceFor(ctx, tx, cid, in.ServiceId, in.AddressId, in.Quantity)
			if err != nil {
				return err
			}
			if err := categoryWindow(p.category).CheckStart(in.RequestedStart, now); err != nil {
				return err
			}
			if in.RecurrenceRule != nil && p.category.Engagement != "day_based" {
				return httpx.Invalid("recurrence_rule is only for day-based services")
			}
			if p.category.RequiresClientIDVerification {
				var st string
				if err := tx.QueryRow(ctx, `SELECT id_verification::text FROM clients WHERE user_id = $1`, cid).Scan(&st); err != nil {
					return err
				}
				if st != "verified" {
					return httpx.RuleViolation("Identity verification is required before booking this service").With("id_verification", st)
				}
			}
			if in.ReplacesRequestId != nil {
				var n int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM service_requests WHERE id = $1 AND client_id = $2`, *in.ReplacesRequestId, cid).Scan(&n); err != nil || n == 0 {
					return httpx.Invalid("replaces_request_id is not one of your requests")
				}
			}
			providerPay := int64(math.Round(float64(p.rate.ProviderPayMinor) * p.qty))
			if err := tx.QueryRow(ctx, `
				INSERT INTO service_requests (client_id, address_id, service_area_id, category_id, service_id, rate_card_entry_id, quantity,
				  client_price_minor, provider_pay_minor, placement_fee_minor, total_minor, currency, requested_start, requested_end,
				  recurrence_rule, client_notes, replaces_request_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
				cid, in.AddressId, p.address.ServiceAreaID, p.category.ID, p.service.ID, p.rate.ID, p.qty,
				p.rate.ClientPriceMinor, providerPay, p.rate.PlacementFeeMinor, p.price.TotalMinor, p.rate.Currency,
				in.RequestedStart, in.RequestedEnd, in.RecurrenceRule, in.ClientNotes, in.ReplacesRequestId).Scan(&id); err != nil {
				return err
			}
			if err := audit(ctx, tx, "request.submit", "service_request", id, nil, map[string]any{"service_id": p.service.ID, "total_minor": p.price.TotalMinor}, ""); err != nil {
				return err
			}
			return emit(ctx, tx, "request.submitted", "service_request", id, map[string]any{"client_id": cid})
		})
		if err != nil {
			return 0, nil, mapErr(err)
		}
		dto, _, err := s.loadRequest(ctx, s.DB, id, &cid)
		return http.StatusCreated, dto, err
	})
}

func (s *Server) GetClientRequests(w http.ResponseWriter, r *http.Request, params gen.GetClientRequestsParams) {
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
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
	var curT *time.Time
	var curID *uuid.UUID
	if cur != nil {
		curT, curID = &cur.T, &cur.ID
	}
	rows, err := s.DB.Query(ctx, `
		SELECT id, submitted_at FROM service_requests
		WHERE client_id = $1 AND ($2::text IS NULL OR status::text = $2)
		  AND ($3::timestamptz IS NULL OR (submitted_at, id) < ($3, $4::uuid))
		ORDER BY submitted_at DESC, id DESC LIMIT $5`, cid, status, curT, curID, limit+1)
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
	var dtos []requestDTO
	for _, k := range keys {
		d, _, err := s.loadRequest(ctx, s.DB, k.id, &cid)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		dtos = append(dtos, d)
	}
	httpx.JSON(w, http.StatusOK, paginate(dtos, limit, func(d requestDTO) (time.Time, uuid.UUID) { return d.SubmittedAt, d.ID }))
}

func (s *Server) GetClientRequestsById(w http.ResponseWriter, r *http.Request, id gen.Id) {
	cid := auth.MustPrincipal(r.Context()).UserID
	d, _, err := s.loadRequest(r.Context(), s.DB, id, &cid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Server) PostClientRequestsCancel(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.CancelInput](r)
	if err != nil || strings.TrimSpace(in.Reason) == "" {
		httpx.Fail(w, r, httpx.Invalid("reason is required"))
		return
	}
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status::text FROM service_requests WHERE id = $1 AND client_id = $2 FOR UPDATE`, id, cid).Scan(&status); err != nil {
			return err
		}
		if !domain.CanClientCancel(status) {
			return httpx.Conflict("A request that is " + status + " cannot be cancelled")
		}
		if err := cancelRequest(ctx, tx, uuid.UUID(id), "client", in.Reason); err != nil {
			return err
		}
		if err := audit(ctx, tx, "request.cancel", "service_request", id, map[string]any{"status": status}, map[string]any{"status": "cancelled"}, in.Reason); err != nil {
			return err
		}
		return emit(ctx, tx, "request.cancelled", "service_request", id, map[string]any{"by": "client"})
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	d, _, err := s.loadRequest(ctx, s.DB, id, &cid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// cancelRequest cancels the request and everything hanging off it. The caller holds the row lock.
func cancelRequest(ctx context.Context, q db, id uuid.UUID, by, reason string) error {
	if _, err := q.Exec(ctx, `UPDATE service_requests SET status = 'cancelled', cancelled_at = now(), cancelled_by = $2::party, cancel_reason = $3 WHERE id = $1`, id, by, reason); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `UPDATE assignments SET status = 'withdrawn', ended_at = now(), ended_reason = 'request cancelled' WHERE request_id = $1 AND status IN ('offered','accepted')`, id); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE visits SET status = 'cancelled' WHERE request_id = $1 AND status = 'scheduled'`, id)
	return err
}

// ---- visits: door code and confirmation

func (s *Server) doorCodeHash(visitID uuid.UUID, code string) []byte {
	return crypt.HMAC(s.Auth.Pepper, "door", visitID.String(), code)
}

func (s *Server) GetClientVisitsCheckinCode(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	var status string
	var start, end time.Time
	err := s.DB.QueryRow(ctx, `
		SELECT v.status::text, v.scheduled_start, v.scheduled_end FROM visits v
		JOIN service_requests r ON r.id = v.request_id WHERE v.id = $1 AND r.client_id = $2`, id, cid).Scan(&status, &start, &end)
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if status != "scheduled" {
		httpx.Fail(w, r, httpx.Conflict("A door code is only available before the provider has checked in"))
		return
	}
	if s.Now().Before(start.Add(-2 * time.Hour)) {
		httpx.Fail(w, r, httpx.RuleViolation("The door code is available from two hours before the visit"))
		return
	}
	code, err := s.Auth.NewCode()
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	code = code[:4]
	if _, err := s.DB.Exec(ctx, `UPDATE visits SET checkin_code_hash = $2, checkin_attempts = 0 WHERE id = $1`, id, s.doorCodeHash(id, code)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"visit_id": id, "code": code, "valid_until": end})
}

func (s *Server) PostClientVisitsConfirm(w http.ResponseWriter, r *http.Request, id gen.Id) {
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	var v visitDTO
	err := dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `
			SELECT v.status::text FROM visits v JOIN service_requests r ON r.id = v.request_id
			WHERE v.id = $1 AND r.client_id = $2 FOR UPDATE OF v`, id, cid).Scan(&status); err != nil {
			return err
		}
		if status != "completed" {
			return httpx.Conflict("Only a completed visit can be confirmed (status: " + status + ")")
		}
		if _, err := tx.Exec(ctx, `UPDATE visits SET status = 'client_confirmed', client_confirmed_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(ctx, tx, "visit.confirm", "visit", id, nil, nil, "")
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	err = s.DB.QueryRow(ctx, `
		SELECT id, request_id, status::text, scheduled_start, scheduled_end, checked_in_at, checkin_method::text, checked_out_at, client_confirmed_at, provider_notes
		FROM visits WHERE id = $1`, id).Scan(&v.ID, &v.RequestID, &v.Status, &v.ScheduledStart, &v.ScheduledEnd, &v.CheckedInAt, &v.CheckinMethod, &v.CheckedOutAt, &v.ClientConfirmedAt, &v.ProviderNotes)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

// ---- rating closes the job (report §5.2 step 10) and feeds appraisal

func (s *Server) PostClientRequestsRating(w http.ResponseWriter, r *http.Request, id gen.Id) {
	in, err := httpx.Decode[gen.RatingCreate](r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.Stars < 1 || in.Stars > 5 {
		httpx.Fail(w, r, httpx.Invalid("stars must be between 1 and 5"))
		return
	}
	sub := map[string]int{}
	if in.SubScores != nil {
		for k, v := range map[string]*int{"punctuality": in.SubScores.Punctuality, "quality": in.SubScores.Quality, "conduct": in.SubScores.Conduct} {
			if v != nil {
				if *v < 1 || *v > 5 {
					httpx.Fail(w, r, httpx.Invalid("sub_scores must be between 1 and 5"))
					return
				}
				sub[k] = *v
			}
		}
	}
	ctx := r.Context()
	cid := auth.MustPrincipal(ctx).UserID
	var out ratingDTO
	err = dbpkg.Tx(ctx, s.DB, func(tx pgx.Tx) error {
		var status string
		var providerPay, clientPrice int64
		var qty float64
		var providerID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT status::text, provider_pay_minor, client_price_minor, quantity::float8 FROM service_requests WHERE id = $1 AND client_id = $2 FOR UPDATE`, id, cid).
			Scan(&status, &providerPay, &clientPrice, &qty); err != nil {
			return err
		}
		if status != "awaiting_rating" {
			return httpx.Conflict("This job is not waiting for a rating (status: " + status + ")")
		}
		if err := tx.QueryRow(ctx, `SELECT provider_id FROM assignments WHERE request_id = $1 AND status = 'accepted'`, id).Scan(&providerID); err != nil {
			return err
		}
		if in.VisitId != nil {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM visits WHERE id = $1 AND request_id = $2`, *in.VisitId, id).Scan(&n); err != nil || n == 0 {
				return httpx.Invalid("visit_id does not belong to this request")
			}
		}
		subJSON, _ := json.Marshal(sub)
		out = ratingDTO{RequestID: id, VisitID: (*uuid.UUID)(in.VisitId), ProviderID: providerID, Stars: in.Stars, SubScores: sub, Comment: in.Comment}
		if err := tx.QueryRow(ctx, `
			INSERT INTO ratings (request_id, visit_id, client_id, provider_id, stars, sub_scores, comment)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at`, id, in.VisitId, cid, providerID, in.Stars, subJSON, in.Comment).Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		// Close the job and update the provider's standing in the same transaction.
		if _, err := tx.Exec(ctx, `UPDATE service_requests SET status = 'closed', closed_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE assignments SET status = 'completed', ended_at = now() WHERE request_id = $1 AND status = 'accepted'`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE providers SET completed_jobs = completed_jobs + 1,
			  rating_avg = round((SELECT avg(stars) FROM ratings WHERE provider_id = $1), 2),
			  rating_count = (SELECT count(*) FROM ratings WHERE provider_id = $1)
			WHERE user_id = $1`, providerID); err != nil {
			return err
		}
		gross := int64(math.Round(float64(clientPrice) * qty))
		// Approved variations are part of the job: the client paid for them and the provider earns their share.
		var varClient, varProvider int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(client_delta_minor),0), COALESCE(sum(provider_delta_minor),0) FROM variations WHERE request_id = $1 AND status = 'approved'`, id).
			Scan(&varClient, &varProvider); err != nil {
			return err
		}
		gross += varClient
		providerPay += varProvider
		// Client prices include VAT, which is not the provider's or the platform's to keep: the earning
		// splits the work without its VAT. The invoice recorded that figure when it was issued, so the
		// ledger split made when it is paid matches; before an invoice exists, use today's rate.
		var workNet *int64
		if err := tx.QueryRow(ctx, `SELECT work_net_minor FROM invoices WHERE request_id = $1 AND status <> 'void' ORDER BY created_at LIMIT 1`, id).Scan(&workNet); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if workNet != nil {
			gross = *workNet
		} else {
			gross = domain.NetOfVAT(gross, s.setting(ctx, tx, "vat.rate_pct", 16))
		}
		held := domain.OpensComplaint(in.Stars, s.setting(ctx, tx, "rating.complaint_threshold", 3))
		earnStatus, holdReason := "pending", (*string)(nil)
		if held {
			earnStatus = "held"
			hr := "Low rating under review"
			holdReason = &hr
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO provider_earnings (provider_id, request_id, gross_minor, commission_minor, withholding_minor, net_minor, status, hold_reason)
			VALUES ($1,$2,$3,$4,0,$5,$6::earning_status,$7)`, providerID, id, gross, gross-providerPay, providerPay, earnStatus, holdReason); err != nil {
			return err
		}
		if err := s.releaseEarnings(ctx, tx, id); err != nil {
			return err
		}
		if held {
			var cmp uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO complaints (source, severity, client_id, provider_id, request_id, rating_id, summary, raised_by)
				VALUES ('low_rating','quality',$1,$2,$3,$4,$5,$1) RETURNING id`,
				cid, providerID, id, out.ID, fmt.Sprintf("Automatic case: %d-star rating", in.Stars)).Scan(&cmp); err != nil {
				return err
			}
			out.ComplaintID = &cmp
			if _, err := tx.Exec(ctx, `INSERT INTO job_exceptions (kind, request_id, provider_id, details) VALUES ('low_rating',$1,$2,$3)`,
				id, providerID, []byte(fmt.Sprintf(`{"stars":%d,"complaint_id":"%s"}`, in.Stars, cmp))); err != nil {
				return err
			}
		}
		if err := audit(ctx, tx, "request.rate", "service_request", id, nil, map[string]any{"stars": in.Stars}, ""); err != nil {
			return err
		}
		return emit(ctx, tx, "request.closed", "service_request", id, map[string]any{"stars": in.Stars, "provider_id": providerID})
	})
	if err != nil {
		httpx.Fail(w, r, mapErr(err))
		return
	}
	if out.SubScores == nil {
		out.SubScores = map[string]int{}
	}
	httpx.JSON(w, http.StatusCreated, out)
}

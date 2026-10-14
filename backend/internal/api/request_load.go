package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

type reqRow struct {
	requestDTO
	addressID       uuid.UUID
	categoryID      uuid.UUID
	serviceID       uuid.UUID
	providerPay     int64
	priority        int
	triageOfficerID *uuid.UUID
}

const requestCols = `r.id, r.reference, r.status::text, r.client_id, r.address_id, r.category_id, r.service_id, r.quantity::float8,
	r.client_price_minor, r.placement_fee_minor, r.total_minor, r.currency, r.requested_start, r.requested_end, r.recurrence_rule,
	r.client_notes, r.offer_client_choice, r.submitted_at, r.confirmed_at, r.completed_at, r.closed_at, r.cancelled_at,
	r.cancelled_by::text, r.cancel_reason, r.provider_pay_minor, r.priority, r.triage_officer_id`

// loadRequest assembles the client-facing view of a request. ownerID, when set, restricts it to that client.
func (s *Server) loadRequest(ctx context.Context, q db, id uuid.UUID, ownerID *uuid.UUID) (requestDTO, reqRow, error) {
	var rr reqRow
	d := &rr.requestDTO
	err := q.QueryRow(ctx, `SELECT `+requestCols+` FROM service_requests r WHERE r.id = $1 AND ($2::uuid IS NULL OR r.client_id = $2)`, id, ownerID).
		Scan(&d.ID, &d.Reference, &d.Status, &d.ClientID, &rr.addressID, &rr.categoryID, &rr.serviceID, &d.Quantity,
			&d.ClientPriceMinor, &d.PlacementFeeMinor, &d.TotalMinor, &d.Currency, &d.RequestedStart, &d.RequestedEnd, &d.RecurrenceRule,
			&d.ClientNotes, &d.OfferClientChoice, &d.SubmittedAt, &d.ConfirmedAt, &d.CompletedAt, &d.ClosedAt, &d.CancelledAt,
			&d.CancelledBy, &d.CancelReason, &rr.providerPay, &rr.priority, &rr.triageOfficerID)
	if err != nil {
		return requestDTO{}, rr, mapErr(err)
	}
	if d.Address, err = s.loadAddress(ctx, q, rr.addressID, nil); err != nil {
		return requestDTO{}, rr, err
	}
	// Providers never see access details before they accept; clients see their own.
	if d.Category, err = scanCategory(q.QueryRow(ctx, `SELECT `+categoryCols+` FROM service_categories c WHERE c.id = $1`, rr.categoryID)); err != nil {
		return requestDTO{}, rr, err
	}
	if d.Service, err = scanService(q.QueryRow(ctx, `SELECT `+serviceCols+` FROM services s WHERE s.id = $1`, rr.serviceID)); err != nil {
		return requestDTO{}, rr, err
	}
	d.RatingRequired = d.Status == "awaiting_rating"
	d.Visits, err = s.loadVisits(ctx, q, id)
	if err != nil {
		return requestDTO{}, rr, err
	}
	_ = q.QueryRow(ctx, `SELECT id FROM invoices WHERE request_id = $1 AND status <> 'void' ORDER BY created_at LIMIT 1`, id).Scan(&d.InvoiceID)
	var pid uuid.UUID
	if err := q.QueryRow(ctx, `SELECT provider_id FROM assignments WHERE request_id = $1 AND status IN ('accepted','completed') ORDER BY offered_at DESC LIMIT 1`, id).Scan(&pid); err == nil {
		pp, err := s.loadProviderPublic(ctx, q, pid)
		if err != nil {
			return requestDTO{}, rr, err
		}
		d.Provider = &pp
	}
	return *d, rr, nil
}

func (s *Server) loadVisits(ctx context.Context, q db, requestID uuid.UUID) ([]visitDTO, error) {
	rows, err := q.Query(ctx, `
		SELECT id, request_id, status::text, scheduled_start, scheduled_end, checked_in_at, checkin_method::text, checked_out_at, client_confirmed_at, provider_notes
		FROM visits WHERE request_id = $1 ORDER BY scheduled_start`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []visitDTO{}
	for rows.Next() {
		var v visitDTO
		if err := rows.Scan(&v.ID, &v.RequestID, &v.Status, &v.ScheduledStart, &v.ScheduledEnd, &v.CheckedInAt, &v.CheckinMethod, &v.CheckedOutAt, &v.ClientConfirmedAt, &v.ProviderNotes); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// loadProviderPublic is what a client may learn about a provider: first name and initial, never contacts.
func (s *Server) loadProviderPublic(ctx context.Context, q db, id uuid.UUID) (providerPublicDTO, error) {
	var d providerPublicDTO
	var full string
	err := q.QueryRow(ctx, `
		SELECT p.user_id, u.full_name, p.bio, p.years_experience, p.languages, p.rating_avg::float8, p.rating_count, p.completed_jobs,
		       COALESCE((SELECT array_agg(c.slug ORDER BY c.slug) FROM provider_categories pc JOIN service_categories c ON c.id = pc.category_id
		                 WHERE pc.provider_id = p.user_id AND pc.status = 'active'), '{}')
		FROM providers p JOIN users u ON u.id = p.user_id WHERE p.user_id = $1`, id).
		Scan(&d.ID, &full, &d.Bio, &d.YearsExperience, &d.Languages, &d.RatingAvg, &d.RatingCount, &d.CompletedJobs, &d.Categories)
	d.DisplayName = displayName(full)
	return d, mapErr(err)
}

func displayName(full string) string {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return parts[0] + " " + strings.ToUpper(parts[len(parts)-1][:1]) + "."
}

func (s *Server) loadAddress(ctx context.Context, q db, id uuid.UUID, ownerID *uuid.UUID) (addressDTO, error) {
	var a addressDTO
	var enc []byte
	err := q.QueryRow(ctx, `
		SELECT id, label, service_area_id, estate, street, house_number, landmark, lat::float8, lng::float8, access_details_enc, is_default
		FROM addresses WHERE id = $1 AND ($2::uuid IS NULL OR client_id = $2)`, id, ownerID).
		Scan(&a.ID, &a.Label, &a.ServiceAreaID, &a.Estate, &a.Street, &a.HouseNumber, &a.Landmark, &a.Lat, &a.Lng, &enc, &a.IsDefault)
	if err != nil {
		return a, mapErr(err)
	}
	if enc != nil {
		plain, err := s.Cipher.Decrypt(enc)
		if err != nil {
			return a, err
		}
		str := string(plain)
		a.AccessDetails = &str
	}
	return a, nil
}

var _ = httpx.ErrNotFound
